package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// wrap runs test binary bin with args as go test -exec does, its output
// through test2json, and judges it against $EXPECT_FAIL (verdict): it
// exits 0 when every failure is a listed test's, the binary's status
// otherwise (1 for a listed test that passed). What the caller sees is
// what go test would have shown: the raw stream when go test -json asked
// for it (-test.v=test2json), else the text, verbose.
func wrap(bin string, args []string) int {
	listed, err := parseListed(os.Getenv("EXPECT_FAIL"))
	base := strings.TrimSuffix(filepath.Base(bin), ".exe")
	if err != nil || !strings.HasSuffix(base, ".test") || hasArg(args, "-test.list") || os.Getenv("XF_OFF") != "" {
		// go run's binaries (GOFLAGS -exec reaches them too), test
		// listings, no list, or a step that needs a binary's real status
		// (XF_OFF=1: gio-layer-fix.sh's tests that must panic on stock
		// Gio): as if it weren't here
		return passthrough(bin, args)
	}
	jsonMode := false
	var run []string
	for _, a := range args {
		if a == "-test.v=test2json" {
			jsonMode = true
		}
		if a == "-test.v" || strings.HasPrefix(a, "-test.v=") {
			continue
		}
		run = append(run, a)
	}
	run = append(run, "-test.v=test2json")
	pkg := pkgName()

	cmd := exec.Command(bin, run...)
	cmd.Stdin = os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
		return 1
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	t2j := exec.Command(goTool(), "tool", "test2json", "-p", pkg)
	t2jIn, err1 := t2j.StdinPipe()
	t2jOut, err2 := t2j.StdoutPipe()
	t2j.Stderr = os.Stderr
	if err := errors.Join(err1, err2, t2j.Start()); err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: go tool test2json: %v\n", err)
		return 1
	}
	if err := cmd.Start(); err != nil {
		pw.Close()
		t2jIn.Close()
		_ = t2j.Wait()
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
		return 1
	}
	pw.Close() // the binary holds its own copy

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the binary's bytes: to test2json, and raw to go test -json
		defer wg.Done()
		defer t2jIn.Close()
		buf := make([]byte, 32<<10)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				if jsonMode {
					_, _ = os.Stdout.Write(buf[:n])
				}
				_, _ = t2jIn.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	r := newBinRun()
	go func() { // test2json's events: judged, and as text to the log
		defer wg.Done()
		sc := bufio.NewScanner(t2jOut)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			r.add(e)
			if !jsonMode && e.Action == "output" {
				_, _ = io.WriteString(os.Stdout, e.Output)
			}
		}
	}()
	werr := cmd.Wait()
	wg.Wait()
	_ = t2j.Wait()
	exit := 0
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			exit = ee.ExitCode()
		}
		if exit <= 0 {
			exit = 1
		}
	}
	rec := verdict(r, exit, listed)
	rec.Job, rec.Package, rec.Leg = jobName(), pkg, os.Getenv("XF_LEG")
	rec.Attempt, _ = strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	save(rec)
	say(os.Stdout, rec)
	switch {
	case rec.OK():
		return 0
	case exit != 0:
		return exit
	}
	return 1
}

// say prints the record's lines: what failed as expected, and as GitHub
// annotations what makes the run fail.
func say(w io.Writer, rec Record) {
	for _, l := range rec.Listed {
		if l.failed() {
			fmt.Fprintf(w, "expect_fail: %s: %s failed as expected (%d of %d runs)\n", rec.Package, l.Name, l.Fails+l.Open, l.Fails+l.Open+l.Passes+l.Skips)
		}
	}
	for _, u := range rec.Unexpected {
		fmt.Fprintf(w, "::error title=expect_fail::%s: %s, and expect_fail doesn't list it\n", rec.Package, u)
	}
	for _, v := range rec.Violations {
		fmt.Fprintf(w, "::error title=expect_fail::%s: %s\n", rec.Package, v)
	}
	if rec.Masked {
		fmt.Fprintf(w, "expect_fail: %s: the binary's exit status %d is the listed tests' failure: 0\n", rec.Package, rec.Exit)
	}
}

func hasArg(args []string, prefix string) bool {
	for _, a := range args {
		if a == prefix || strings.HasPrefix(a, prefix+"=") {
			return true
		}
	}
	return false
}

// passthrough runs bin with args and the caller's stdio, its exit status
// the caller's.
func passthrough(bin string, args []string) int {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() > 0 {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
		return 1
	}
	return 0
}

// goTool is the go command this was built with (setup-go's), else PATH's.
func goTool() string {
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	if g := filepath.Join(runtime.GOROOT(), "bin", "go"+exe); fileExists(g) {
		return g
	}
	return "go"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// pkgName is the package go test runs the binary in (its directory),
// relative to the checkout.
func pkgName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	if ws := os.Getenv("GITHUB_WORKSPACE"); ws != "" {
		if rel, err := filepath.Rel(ws, wd); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(wd)
}

func jobName() string {
	j := os.Getenv("GITHUB_JOB")
	if j == "" {
		j = "local"
	}
	if o := os.Getenv("RUNNER_OS"); o != "" {
		j += " (" + o + ")"
	}
	return j
}

// save writes rec to $XF_DIR/results for the job's report and the run's
// check (nowhere without XF_DIR).
func save(rec Record) {
	dir := os.Getenv("XF_DIR")
	if dir == "" {
		return
	}
	dir = filepath.Join(dir, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
		return
	}
	b, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return
	}
	name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(rec.Package)
	p := filepath.Join(dir, fmt.Sprintf("%s-%d-%d.json", name, os.Getpid(), time.Now().UnixNano()))
	if err := os.WriteFile(p, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
	}
}
