package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fixtureTests = `package p

import (
	"os"
	"testing"
	"time"
)

func TestPass(t *testing.T) {}

func TestFail(t *testing.T) { t.Fatal("boom, as expected") }

func TestSub(t *testing.T) {
	t.Run("bad", func(t *testing.T) { t.Fatal("sub boom") })
	t.Run("good", func(t *testing.T) {})
}

func TestPanic(t *testing.T) {
	if os.Getenv("XF_PANIC") != "" {
		panic("kaboom")
	}
}

func TestHang(t *testing.T) {
	if os.Getenv("XF_HANG") != "" {
		time.Sleep(time.Minute)
	}
}
`

// The wrapper under go test, as the workflows run it (GOFLAGS -exec):
// a module of tiny tests, each case a go test with its own expect_fail.
func TestWrapUnderGoTest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs go test")
	}
	tmp := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	xf := filepath.Join(tmp, "xf"+exe)
	if out, err := exec.Command(goTool(), "build", "-o", xf, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	mod := filepath.Join(tmp, "mod")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"go.mod": "module xftest\n\ngo 1.24\n", "p_test.go": fixtureTests} {
		if err := os.WriteFile(filepath.Join(mod, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		code int
		out  string
		recs []Record
	}
	n := 0
	gotest := func(list string, env []string, args ...string) result {
		t.Helper()
		n++
		dir := filepath.Join(tmp, "xf-dir", strings.Repeat("x", n))
		cmd := exec.Command(goTool(), append([]string{"test", "-count=1"}, args...)...)
		cmd.Dir = mod
		cmd.Env = append(os.Environ(), "GOFLAGS=-exec="+xf, "EXPECT_FAIL="+list, "XF_DIR="+dir, "GITHUB_WORKSPACE="+mod,
			"GOWORK=off", "GOTOOLCHAIN=local", "GO111MODULE=on")
		cmd.Env = append(cmd.Env, env...)
		var b bytes.Buffer
		cmd.Stdout, cmd.Stderr = &b, &b
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		recs, err := readRecords(filepath.Join(dir, "results"))
		if err != nil {
			t.Fatal(err)
		}
		return result{code, b.String(), recs}
	}
	t.Run("the listed test fails, nothing else: green", func(t *testing.T) {
		// -v: go test shows a package's output only when it fails or with
		// -v, and to go test it passed (the job's summary has the failure)
		r := gotest("TestFail", nil, "-v", "-run", "^(TestPass|TestFail)$", ".")
		if r.code != 0 || !strings.Contains(r.out, "--- FAIL: TestFail") || !strings.Contains(r.out, "TestFail failed as expected") {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
		if len(r.recs) != 1 || len(r.recs[0].Listed) != 1 || !strings.Contains(strings.Join(r.recs[0].Listed[0].Head, "\n"), "boom, as expected") {
			t.Errorf("records %+v", r.recs)
		}
	})
	t.Run("a listed test passes: red", func(t *testing.T) {
		if r := gotest("TestFail,TestPass", nil, "-run", "^(TestPass|TestFail)$", "."); r.code == 0 || !strings.Contains(r.out, "TestPass passed") {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("another test fails: red", func(t *testing.T) {
		if r := gotest("TestFail", nil, "-run", "^(TestFail|TestSub)$", "."); r.code == 0 || !strings.Contains(r.out, "TestSub/bad failed") {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("a listed subtest fails its parent: green", func(t *testing.T) {
		if r := gotest("TestSub/bad", nil, "-run", "^TestSub$", "."); r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("go test -json: the stream as go test makes it, green", func(t *testing.T) {
		r := gotest("TestFail", nil, "-json", "-run", "^(TestPass|TestFail)$", ".")
		if r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
		var failed, passed bool
		sc := bufio.NewScanner(strings.NewReader(r.out))
		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			failed = failed || e.Action == "fail" && e.Test == "TestFail"
			passed = passed || e.Action == "pass" && e.Test == "TestPass"
		}
		if !failed || !passed {
			t.Errorf("events: TestFail failed %v, TestPass passed %v:\n%s", failed, passed, r.out)
		}
	})
	t.Run("a panic in the listed test: green", func(t *testing.T) {
		if r := gotest("TestPanic", []string{"XF_PANIC=1"}, "-run", "^TestPanic$", "."); r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("the listed test times out: green", func(t *testing.T) {
		if r := gotest("TestHang", []string{"XF_HANG=1"}, "-timeout", "3s", "-run", "^TestHang$", "."); r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("an unlisted test times out: red", func(t *testing.T) {
		if r := gotest("TestFail", []string{"XF_HANG=1"}, "-timeout", "3s", "-run", "^(TestFail|TestHang)$", "."); r.code == 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("XF_OFF=1: the binary's own status (a step that needs a real failure)", func(t *testing.T) {
		if r := gotest("TestFail", []string{"XF_OFF=1"}, "-run", "^TestFail$", "."); r.code == 0 || len(r.recs) != 0 {
			t.Fatalf("exit %d, %d records:\n%s", r.code, len(r.recs), r.out)
		}
	})
	t.Run("go test -list goes through untouched", func(t *testing.T) {
		if r := gotest("TestFail", nil, "-list", ".", "."); r.code != 0 || !strings.Contains(r.out, "TestHang") {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
	t.Run("a build error stays one", func(t *testing.T) {
		bad := filepath.Join(mod, "bad_test.go")
		if err := os.WriteFile(bad, []byte("package p\n\nfunc TestBad(t *testing.T) { undefined() }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(bad)
		if r := gotest("TestBad", nil, "-run", "^TestBad$", "."); r.code == 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
	})
}
