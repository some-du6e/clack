#!/usr/bin/env python3
"""Every Go test timeout in a job runs out early enough that a hang's dump
prints inside the job's budget.

  budget-check.py [--starts FILE] WORKFLOW.yml...
  budget-check.py record [--starts FILE] WORKFLOW.yml JOBS.json... > NEW

A job's timeout-minutes is its budget (docs/CI-SPEED.md): GitHub stops the
job there, without the step's output. Go prints `panic: test timed out` and
every goroutine when a test binary's -timeout (-test.timeout) runs out,
counted from that binary's start, so the dump prints at the binary's start
plus its timeout, and that must come MARGIN minutes before the budget.

A binary starts, at the latest:
- a step's first go test, by SLOW times the step's recorded start, plus
  BUILD; a later one in the step, by SLOW times the step's recorded end.
  FILE (.github/step-starts.json) records each test step's start and end in
  minutes into its job, the most over recent green runs: `record` reads
  `gh api repos/OWNER/REPO/actions/runs/ID/jobs` output and prints FILE
  with those runs merged in. A record holds only while every test step
  before it in the job has one (a step put in front moves them later);
- a step without one: by FIRST if it is the job's first test step, else by
  the job's p95 (its budget's comment), or half its budget without one.

A step that starts dump-before-budget.sh MINUTES is checked on that
instead, at SLOW times its start plus MINUTES: `go test ./...` starts each
package whenever a slot frees up, so a late one's -timeout can end anywhere
past the budget, and the script makes whatever still runs dump in time.

SLOW: the steps before ran a quarter slower than their slowest recorded
run. ci-speed round 2 found the idle jobs' fanout timeout equal to their
budget, and full-test's 30 min against its 40 losing any package that
started after minute 8 (train-130's full-test, attempt 2: killed at 40 min,
nothing printed).
"""
import json
import os
import re
import sys

MARGIN = 2
FIRST = 4
BUILD = 1.5
SLOW = 1.25
STARTS = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'step-starts.json')

JOB = re.compile(r'^  ([A-Za-z0-9_-]+):\s*$')
JOB_NAME = re.compile(r'^    name:\s*(.+?)\s*$')
BUDGET = re.compile(r'^    timeout-minutes:\s*(\d+)(.*)$')
STEP = re.compile(r'^      - (.*)$')
STEP_KEY = re.compile(r'^(?:      - |        )(name|run|uses):\s*(.*?)\s*$')
P95 = re.compile(r'p95\D{0,3}?(\d+(?:\.\d+)?)')
TIMEOUT = re.compile(r'-(?:test\.)?timeout[ =](\d+)(m|s|h)\b')
WATCHDOG = re.compile(r'dump-before-budget\.sh"?\s+(\d+)')


def minutes(n, unit):
    return {'s': n / 60, 'm': n, 'h': n * 60}[unit]


def unquote(s):
    return s[1:-1] if len(s) > 1 and s[0] == s[-1] and s[0] in '"\'' else s


def parse(path):
    """[{id, name, budget, p95, steps: [{display, timeouts: [(line, minutes, text)], watchdog}]}]"""
    jobs, job, step = [], None, None
    lines = open(path).read().split('\n')
    in_jobs = False
    for i, line in enumerate(lines, 1):
        if line.startswith('jobs:'):
            in_jobs = True
            continue
        if not in_jobs:
            continue
        m = JOB.match(line)
        if m:
            job = {'id': m.group(1), 'name': None, 'budget': 360, 'p95': None, 'steps': []}
            jobs.append(job)
            step = None
            continue
        if job is None:
            continue
        if step is None:
            m = JOB_NAME.match(line)
            if m:
                job['name'] = unquote(m.group(1))
                continue
            m = BUDGET.match(line)
            if m:
                job['budget'] = int(m.group(1))
                p = P95.search(m.group(2))
                job['p95'] = float(p.group(1)) if p else None
                continue
        if STEP.match(line):
            step = {'name': None, 'run': None, 'uses': None, 'line': i, 'timeouts': [], 'watchdog': None}
            job['steps'].append(step)
        if step is None:
            continue
        m = STEP_KEY.match(line)
        if m and step[m.group(1)] is None:
            step[m.group(1)] = unquote(m.group(2))
        m = WATCHDOG.search(line)
        if m:
            step['watchdog'] = (i, int(m.group(1)))
        if 'test' in line:
            for n, unit in TIMEOUT.findall(line):
                step['timeouts'].append((i, minutes(int(n), unit), line.strip()[:100]))
    for job in jobs:
        for s in job['steps']:
            s['display'] = display(s, lines)
    return jobs


def display(s, lines):
    """the step's name as GitHub's API reports it"""
    if s['name']:
        return s['name']
    if s['uses']:
        return 'Run ' + s['uses']
    run = s['run'] or ''
    if run in ('', '|', '>', '|-', '>-'):
        for line in lines[s['line']:]:  # the block's first line
            if line.strip():
                return 'Run ' + line.strip()
    return 'Run ' + run


def load(path):
    try:
        with open(path) as f:
            return json.load(f)
    except FileNotFoundError:
        return {}


def check(path, starts):
    errs = []
    rec_wf = starts.get(os.path.basename(path), {})
    for job in parse(path):
        budget, rec = job['budget'], rec_wf.get(job['id'], {})
        later = job['p95'] if job['p95'] is not None else budget / 2
        recorded, first = True, True
        for s in job['steps']:
            if not s['timeouts'] and not s['watchdog']:
                continue
            r = rec.get(s['display']) if recorded else None
            recorded = r is not None
            if s['watchdog']:
                ln, w = s['watchdog']
                at = (SLOW * r[0] if r else (1 if first else later)) + w
                if at + MARGIN > budget:
                    errs.append(f'{path}:{ln}: job {job["id"]}: dump-before-budget.sh {w} dumps by minute {at:.1f}, '
                                f'past its {budget} min budget less {MARGIN}')
                first = False
                continue
            for k, (ln, t, text) in enumerate(s['timeouts']):
                if r and k == 0:
                    start, why = SLOW * r[0] + BUILD, f'its step starts by {r[0]:g} (recorded), x{SLOW:g}, + {BUILD:g} to build'
                elif r:
                    start, why = SLOW * r[1], f'after its step\'s first go test: by its recorded end {r[1]:g}, x{SLOW:g}'
                elif first and k == 0:
                    start, why = FIRST, 'the job\'s first test binary, no record'
                else:
                    start, why = max(FIRST, later), (f'no record: by the job\'s p95, {later:g}' if job['p95'] is not None
                                                     else f'no record or p95: by half the budget, {later:g}')
                if start + t + MARGIN > budget:
                    errs.append(f'{path}:{ln}: job {job["id"]}: a {t:g} min Go timeout from minute {start:.1f} ({why}) '
                                f'dumps at {start + t:.1f}, past its {budget} min budget less {MARGIN}: {text}')
            first = False
    return errs


def record(args):
    """merges one workflow's runs (JOBS.json files) into the starts file"""
    import datetime as dt
    starts_path = STARTS
    if args[:1] == ['--starts']:
        starts_path, args = args[1], args[2:]
    wf_path, runs = args[0], args[1:]
    jobs = parse(wf_path)

    def t(s):
        return dt.datetime.fromisoformat(s.replace('Z', '+00:00')).timestamp()

    def lit(j):  # a job's display name up to its first expression
        return (j['name'] or j['id']).split('${{')[0].strip()

    def job_of(api_name):
        base = api_name.split(' (')[0]
        best = None
        for j in jobs:
            if base == j['id'] or (j['name'] and lit(j) and api_name.startswith(lit(j))):
                if best is None or len(lit(j)) > len(lit(best)):
                    best = j
        return best

    starts = load(starts_path)
    out = starts.setdefault(os.path.basename(wf_path), {})
    n = 0
    for p in runs:
        with open(p) as f:
            data = json.load(f)
        for aj in data.get('jobs', []):
            if aj.get('conclusion') != 'success' or not aj.get('steps') or not aj.get('started_at'):
                continue
            j = job_of(aj['name'])
            if j is None:
                continue
            names = {s['display'] for s in j['steps'] if s['timeouts'] or s['watchdog']}
            t0 = t(aj['started_at'])
            for s in aj['steps']:
                if s['name'] not in names or s.get('conclusion') != 'success':
                    continue
                if not s.get('started_at') or not s.get('completed_at'):
                    continue
                a, b = (t(s['started_at']) - t0) / 60, (t(s['completed_at']) - t0) / 60
                o = out.setdefault(j['id'], {}).get(s['name'], [0, 0])
                out[j['id']][s['name']] = [round(max(o[0], a), 1), round(max(o[1], b), 1)]
                n += 1
    json.dump(starts, sys.stdout, indent=1, sort_keys=True, ensure_ascii=False)
    sys.stdout.write('\n')
    print(f'record: {os.path.basename(wf_path)}: {n} step timings from {len(runs)} runs', file=sys.stderr)
    return 0


def main():
    args = sys.argv[1:]
    if args[:1] == ['record']:
        return record(args[1:])
    starts_path = STARTS
    if args[:1] == ['--starts']:
        starts_path, args = args[1], args[2:]
    starts = load(starts_path)
    errs = []
    for p in args:
        errs += check(p, starts)
    for e in errs:
        print(f'::error::{e}')
    print(f'budget-check: {len(args)} workflows, {len(errs)} Go timeouts whose dump would print past the budget')
    return 1 if errs else 0


if __name__ == '__main__':
    sys.exit(main())
