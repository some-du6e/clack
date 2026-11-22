#!/usr/bin/env python3
"""perf.yml's first job: the base commit, and whether the diff from it can
change a number the perf jobs measure.

usage: affects.py   (in the checkout, fetch-depth 0; env BASE, FORCE, EVENT,
                     REF_NAME; writes GITHUB_OUTPUT and GITHUB_STEP_SUMMARY)

The base: BASE when given; for a dispatch on a branch other than ui and
main, where that branch left ui (its merge-base with origin/ui), so the
numbers say what the branch changed; otherwise (pushes, ui, main) the
newest v* release tag before HEAD.

A dispatch whose diff from the base touches only docs, tests or CI files
can't move a number (both sides run HEAD's harness and fakeslackd), so its
jobs skip, and say so; FORCE=true runs them anyway (a harness change, or
base-vs-base runs that measure the noise). Pushes always run: each one
adds its row to perf-history.
"""
import fnmatch
import os
import subprocess
import sys

# what can't change the client's own numbers
NO_PERF = [
    '*.md', 'docs/*', 'LICENSE*', 'THIRD_PARTY_NOTICES*', '.gitignore', '.gitattributes',
    '*_test.go', '*/testdata/*', 'testdata/*',
    '.github/*',
]


def git(*args):
    return subprocess.run(['git', *args], check=True, capture_output=True, text=True).stdout.strip()


def resolve(ref):
    for r in (ref, 'origin/' + ref):
        try:
            return git('rev-parse', '-q', '--verify', r + '^{commit}')
        except subprocess.CalledProcessError:
            pass
    return None


def no_perf(path):
    return any(fnmatch.fnmatch(path, p) for p in NO_PERF)


def main():
    base_in = os.environ.get('BASE', '').strip()
    force = os.environ.get('FORCE', '') == 'true'
    event = os.environ.get('EVENT', '')
    ref = os.environ.get('REF_NAME', '')
    if base_in:
        base, how = resolve(base_in), base_in
    elif event == 'workflow_dispatch' and ref not in ('ui', 'main') and resolve('origin/ui'):
        base, how = git('merge-base', 'HEAD', resolve('origin/ui')), 'where this branch left ui'
    else:
        tag = subprocess.run(['git', 'describe', '--tags', '--abbrev=0', '--match', 'v*', 'HEAD^'],
                             capture_output=True, text=True).stdout.strip()
        base, how = resolve(tag or 'HEAD^'), tag or 'HEAD^'
    if not base:
        print(f'::error::base {base_in or how} not found')
        return 1
    files = [f for f in git('diff', '--name-only', base, 'HEAD').splitlines() if f]
    perf = [f for f in files if not no_perf(f)]
    run = event != 'workflow_dispatch' or force or bool(perf)
    label = f'{how} ({base[:7]})'
    if not files:
        why = f'no change from the base {label}'
    elif perf:
        why = f'{len(perf)} of the {len(files)} files changed since {label} can change its numbers, e.g. ' + ', '.join(perf[:5])
    else:
        why = f'the {len(files)} files changed since {label} are docs, tests and CI files only: ' + ', '.join(files[:8])
    if run and not perf and force:
        why += ' (force: runs anyway)'
    elif run and event != 'workflow_dispatch':
        why += f' ({event}: runs every time)'
    msg = ('perf runs: ' if run else 'perf skipped: ') + why
    print(msg)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as f:
        f.write(f'perf={"true" if run else "false"}\nbase={base}\nlabel={label}\n')
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as f:
        f.write(('### ' if run else '### ⏭️ ') + msg + '\n')
    return 0


if __name__ == '__main__':
    sys.exit(main())
