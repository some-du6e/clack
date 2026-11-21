#!/usr/bin/env python3
"""Every Go test timeout in a job ends before the job's budget.

  budget-check.py WORKFLOW.yml...

A job's timeout-minutes is its budget (docs/CI-SPEED.md): GitHub stops the
job there, without the step's output. A `go test -timeout` (or a test
binary's -test.timeout) at or past it means a hung test is stopped by
GitHub first, and Go's own `panic: test timed out` and goroutine dump never
print (ci-speed round 2: the idle jobs' fanout timeout was their budget).
Each one must end at least MARGIN minutes before the budget; a job without
a budget is GitHub's 360 minutes.
"""
import re
import sys

MARGIN = 2
JOB = re.compile(r'^  ([A-Za-z0-9_-]+):\s*$')
BUDGET = re.compile(r'^    timeout-minutes:\s*(\d+)')
TIMEOUT = re.compile(r'-(?:test\.)?timeout[ =](\d+)(m|s|h)\b')


def minutes(n, unit):
    return {'s': n / 60, 'm': n, 'h': n * 60}[unit]


def check(path):
    errs = []
    job, budget, uses = None, 360, []
    lines = open(path).read().split('\n')

    def flush():
        for ln, t, text in uses:
            if t > budget - MARGIN:
                errs.append(f'{path}:{ln}: job {job}: a {t:g} min Go timeout against its {budget} min budget '
                            f'(at most {budget - MARGIN}): {text}')

    in_jobs = False
    for i, line in enumerate(lines, 1):
        if line.startswith('jobs:'):
            in_jobs = True
            continue
        m = JOB.match(line) if in_jobs else None
        if m:
            flush()
            job, budget, uses = m.group(1), 360, []
            continue
        m = BUDGET.match(line)
        if m and job:
            budget = int(m.group(1))
            continue
        if job and 'test' in line:
            for n, unit in TIMEOUT.findall(line):
                uses.append((i, minutes(int(n), unit), line.strip()[:120]))
    flush()
    return errs


def main():
    errs = []
    for p in sys.argv[1:]:
        errs += check(p)
    for e in errs:
        print(f'::error::{e}')
    print(f'budget-check: {len(sys.argv) - 1} workflows, {len(errs)} timeouts past their budgets')
    return 1 if errs else 0


if __name__ == '__main__':
    sys.exit(main())
