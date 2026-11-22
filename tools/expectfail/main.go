// Command expectfail is CI's expected-failure mode for proof runs
// (docs/CI-SPEED.md, "Proof runs"). A red or mutant branch proves that
// named tests fail on the old code; with expect_fail set, the run passes
// only if each of them fails and nothing else does, so a proof doesn't
// end red (each red run pings Karim's phone).
//
// The workflows' expect-fail action builds it and sets
// GOFLAGS=-exec=<it>, so every go test in the job runs its test binary
// through it:
//
//	expectfail <pkg.test> <args>    run the binary, judge it (wrap.go)
//	expectfail -validate            check $EXPECT_FAIL, print it normalised
//	expectfail -report              the job's summary of its records
//	expectfail -check DIR           the run's check over every job's records
//
// A build or vet error never reaches it (go test stops first), so it fails
// the step as before. Binaries a job runs itself (go test -c, then the
// binary) aren't judged: their listed failures fail the step, never a
// false pass.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: expectfail <pkg.test> args... | -validate | -report | -check DIR")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-validate":
		l, err := parseListed(os.Getenv("EXPECT_FAIL"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "::error title=expect_fail::%v\n", err)
			os.Exit(2)
		}
		fmt.Println(strings.Join(l, ","))
	case "-report":
		os.Exit(report(os.Getenv("XF_DIR"), os.Getenv("GITHUB_STEP_SUMMARY")))
	case "-check":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: expectfail -check DIR")
			os.Exit(2)
		}
		os.Exit(check(os.Args[2], os.Getenv("EXPECT_FAIL"), os.Getenv("NEEDS"), os.Getenv("GITHUB_STEP_SUMMARY"), os.Stdout))
	default:
		os.Exit(wrap(os.Args[1], os.Args[2:]))
	}
}
