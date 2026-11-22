#!/usr/bin/env python3
"""expect_fail_wire.py [--check] WORKFLOW.yml...

Wires expect_fail into workflows (docs/CI-SPEED.md, "Proof runs"), as
tools/expectfail's TestWorkflowsWireExpectFail wants it:

  - the expect_fail input under workflow_dispatch;
  - in every job with a setup-go step: the expect-fail action right after
    setup-go, and the expect-fail-report action as its last step;
  - the expect-fail job, needing every job.

Idempotent: a wired file comes out unchanged, so run it on a merged file
(a train merging branches made before quiet-ci, whose new jobs lack the
steps) and commit what it changes. --check changes nothing and exits 1 if
a file needs wiring, naming what. Needs only Python 3 (no Go, no checkout
beyond the files).

The repo's workflows are laid out alike (jobs at 2 spaces, steps at 6);
a file that isn't fails with a message rather than being guessed at.
"""
import re
import sys

INPUT = [
    '      expect_fail:',
    '        description: "proof runs: the tests that must fail, comma-separated (TestA,TestB/sub); the run passes only if each fails and nothing else does (docs/CI-SPEED.md, Proof runs)"',
    '        required: false',
    '        default: ""',
]
SETUP = [
    '      - uses: ./.github/actions/expect-fail',
    '        with:',
    '          tests: ${{ inputs.expect_fail }}',
    '          leg: ${{ github.job }}-${{ strategy.job-index }}',
]
REPORT = [
    '      - if: ${{ always() && env.XF_ID }}',
    '        uses: ./.github/actions/expect-fail-report',
]
CHECK_HEAD = [
    '',
    "  # expect_fail's verdict across the jobs (docs/CI-SPEED.md, \"Proof runs\"):",
    '  # every listed test failed in some job and passed in none, nothing else',
    '  # failed, every job that ran succeeded. Proof runs only. A new job goes',
    '  # in its needs (.github/scripts/expect_fail_wire.py adds it).',
    '  expect-fail:',
    '    needs:',
]
CHECK_TAIL = [
    "    if: ${{ always() && inputs.expect_fail != '' }}",
    '    runs-on: ubuntu-latest',
    '    timeout-minutes: 10 # budget, min (a checkout, setup-go and the check)',
    '    steps:',
    '      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0',
    '        with:',
    '          persist-credentials: false',
    '          sparse-checkout: tools/expectfail',
    '      - uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0',
    '        with:',
    '          go-version-file: go.mod',
    '          cache: false',
    '      - uses: actions/download-artifact@d3f86a106a0bac45b974a628896c90dbdf5c8093 # v4.3.0',
    '        with:',
    '          pattern: expect-fail-*',
    '          path: ${{ runner.temp }}/xf',
    '      - name: every listed test failed, nothing else did',
    '        env:',
    '          EXPECT_FAIL: ${{ inputs.expect_fail }}',
    '          NEEDS: ${{ toJSON(needs) }}',
    '        run: go run ./tools/expectfail -check "$RUNNER_TEMP/xf"',
]
JOB = re.compile(r'^  ([A-Za-z0-9_-]+):\s*$')
STEP = re.compile(r'^      - ')


class Bad(Exception):
    pass


def wire_input(L, notes):
    if any(re.match(r'^      expect_fail:\s*$', l) for l in L):
        return L
    i = next((k for k, l in enumerate(L) if re.match(r'^  workflow_dispatch:\s*$', l)), None)
    if i is None:
        raise Bad('no workflow_dispatch: trigger to add the input to')
    notes.append('the expect_fail input')
    if i + 1 < len(L) and re.match(r'^    inputs:\s*$', L[i + 1]):
        k = i + 2
        while k < len(L) and (L[k].startswith('      ') or (not L[k].strip() and k + 1 < len(L) and L[k + 1].startswith('      '))):
            k += 1
        return L[:k] + INPUT + L[k:]
    return L[:i + 1] + ['    inputs:'] + INPUT + L[i + 1:]


def split_jobs(L):
    """head (through 'jobs:'), then [(id, lines)] with each job's trailing
    blank and 2-space comment lines (the next job's preamble) split off
    into a separate 'gap' entry."""
    j0 = L.index('jobs:')
    head, body = L[:j0 + 1], L[j0 + 1:]
    starts = [k for k, l in enumerate(body) if JOB.match(l)]
    parts = [(None, body[:starts[0]] if starts else body)]
    for n, k in enumerate(starts):
        end = starts[n + 1] if n + 1 < len(starts) else len(body)
        job = body[k:end]
        gap = []
        while job and (not job[-1].strip() or job[-1].startswith('  #')):
            gap.insert(0, job.pop())
        parts.append((JOB.match(job[0]).group(1), job))
        parts.append((None, gap))
    return head, parts


def blocks(job):
    """the job's lines before its first step, then each step as (own, gap):
    its own lines (the step line and what's indented under it) and the
    comment and blank lines after them, before the next step."""
    idx = [k for k, l in enumerate(job) if STEP.match(l)]
    if not idx:
        return job, []
    res = []
    for n, k in enumerate(idx):
        end = idx[n + 1] if n + 1 < len(idx) else len(job)
        own = k + 1
        while own < end:
            l = job[own]
            if l.startswith('        '):
                own += 1
            elif not l.strip():
                # a blank line inside the step (a run: block) if an indented
                # line follows before anything at 6 spaces or less
                nxt = next((x for x in job[own + 1:end] if x.strip()), '')
                if nxt.startswith('        '):
                    own += 1
                else:
                    break
            else:
                break
        res.append((job[k:own], job[own:end]))
    return job[:idx[0]], res


def is_uses(lines, action):
    return any(l.strip().removeprefix('- ').strip() == 'uses: ' + action for l in lines)


def wire_job(jid, job, notes):
    if 'actions/setup-go@' not in '\n'.join(job):
        return job
    if '    steps:' not in job:
        raise Bad(f'job {jid}: setup-go without a steps: list at 4 spaces')
    for l in job[job.index('    steps:') + 1:]:
        if re.match(r'^ {6}\S', l) and not (STEP.match(l) or l.startswith('      #')):
            raise Bad(f'job {jid}: a line at 6 spaces that starts no step: {l.strip()[:60]}')
    pre, bl = blocks(job)
    if not bl:
        raise Bad(f'job {jid}: setup-go outside a steps list')
    sg = [n for n, (own, _) in enumerate(bl) if 'actions/setup-go@' in '\n'.join(own)]
    if len(sg) != 1:
        raise Bad(f'job {jid}: {len(sg)} setup-go steps')
    out = list(pre)
    for n, (own, gap) in enumerate(bl):
        if is_uses(own, './.github/actions/expect-fail') or is_uses(own, './.github/actions/expect-fail-report'):
            out.extend(gap)  # the step goes; what followed it stays
            continue
        out.extend(own)
        if n == sg[0]:
            out.extend(SETUP)
        out.extend(gap)
    tail = []
    while out and not out[-1].strip():
        tail.insert(0, out.pop())
    out.extend(REPORT)
    out.extend(tail)
    if out != job:
        notes.append(f'job {jid}: its expect-fail steps')
    return out


def wire_check(parts, notes):
    ids = [jid for jid, _ in parts if jid and jid != 'expect-fail']
    for n, (jid, job) in enumerate(parts):
        if jid != 'expect-fail':
            continue
        try:
            a = job.index('    needs:')
        except ValueError:
            raise Bad('the expect-fail job has no block-list needs:')
        b = a + 1
        have = []
        while b < len(job) and re.match(r'^      - [A-Za-z0-9_-]+\s*$', job[b]):
            have.append(job[b].strip()[2:].strip())
            b += 1
        missing = [x for x in ids if x not in have]
        gone = [x for x in have if x not in ids]
        if missing or gone:
            notes.append('the expect-fail job\'s needs: ' + ', '.join(['+' + x for x in missing] + ['-' + x for x in gone]))
            new = [x for x in have if x in ids] + missing
            parts[n] = (jid, job[:a + 1] + [f'      - {x}' for x in new] + job[b:])
        return parts
    notes.append('the expect-fail job')
    parts.append(('expect-fail', CHECK_HEAD[1:] + [f'      - {x}' for x in ids] + CHECK_TAIL))
    # a blank line before it
    parts.insert(len(parts) - 1, (None, ['']))
    return parts


def wire(text):
    notes = []
    L = text.split('\n')
    trailing_nl = text.endswith('\n')
    if trailing_nl:
        L = L[:-1]
    L = wire_input(L, notes)
    head, parts = split_jobs(L)
    parts = [(jid, wire_job(jid, job, notes) if jid and jid != 'expect-fail' else job) for jid, job in parts]
    # trailing gap of the last job: keep it at the end
    parts = wire_check(parts, notes)
    out = list(head)
    for _, lines in parts:
        out.extend(lines)
    while out and not out[-1].strip():
        out.pop()
    return '\n'.join(out) + '\n', notes


def main(argv):
    check = '--check' in argv
    files = [a for a in argv if a != '--check']
    if not files:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    rc = 0
    for f in files:
        try:
            text = open(f).read()
            new, notes = wire(text)
        except (Bad, ValueError) as e:
            print(f'{f}: {e}', file=sys.stderr)
            rc = 2
            continue
        if new == text:
            print(f'{f}: wired')
            continue
        if check:
            print(f'{f}: needs ' + '; '.join(notes))
            rc = max(rc, 1)
            continue
        open(f, 'w').write(new)
        print(f'{f}: added ' + '; '.join(notes))
    return rc


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
