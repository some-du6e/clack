#!/usr/bin/env bash
# dump-before-budget.sh MINUTES: started in the background of a go test
# step; MINUTES after that, every Go test binary still running gets SIGQUIT,
# so each prints all its goroutines and exits, and go test prints that as the
# package's output. GitHub stops a job at its budget (timeout-minutes)
# without a word, and go test's -timeout runs from each package's own start:
# in a `go test ./...` a package can start late enough that its -timeout ends
# past the budget (full-test, ci-speed round 2). budget-check.py checks
# MINUTES against the job's budget. Linux and macOS: Windows has no SIGQUIT.
set -u
sleep $(($1 * 60))
# the program itself named *.test (go test's binaries, fanout.sh's), not
# a command line that only mentions one
pids=$(pgrep -f '^[^ ]*[.]test( |$)') || exit 0
echo "::error::still running $1 min into the step: SIGQUIT to each test binary, so it prints its goroutines before the job's budget"
# shellcheck disable=SC2086 # one pid a word
ps -o pid=,etime=,args= -p "$(echo $pids | tr ' ' ',')" | cut -c1-200
# shellcheck disable=SC2086
kill -QUIT $pids
