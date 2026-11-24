package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No toast shows a Go error: Karim's "Couldn't download: core: Fetch:
// file over 64 MB: slack: response too large" was one of about 100. A
// failure toasts Slack's own text for it (or Slack's generic one,
// popups.FailedToast) and the error goes to the log; where Slack only
// logs, Clack only logs. This parses internal/ui and fails any toast call
// (Toast, ToastParts, a lowercase toast wrapper, a notes queue's add)
// whose arguments read an error's text: a .Error() call, one of the
// helpers that return it (errText, friendlyErr, dirErr, cdErr, cdErrCode,
// friendly), a format call (Sprintf, Sprint, Errorf) given an error
// variable, or a local built from any of those in the same function. A
// message split over lines counts as the call it is.
func TestNoErrorTextInToasts(t *testing.T) {
	helpers := map[string]bool{"errText": true, "friendlyErr": true, "dirErr": true, "cdErr": true, "cdErrCode": true, "friendly": true}
	errName := regexp.MustCompile(`^(err|e|ferr|rerr|werr|cerr|perr|lerr|[a-z]*Err)$`)
	formats := map[string]bool{"Sprintf": true, "Sprint": true, "Sprintln": true, "Errorf": true}
	toastish := func(c *ast.CallExpr) bool {
		switch f := c.Fun.(type) {
		case *ast.Ident:
			return strings.Contains(strings.ToLower(f.Name), "toast")
		case *ast.SelectorExpr:
			if strings.Contains(strings.ToLower(f.Sel.Name), "toast") {
				return true
			}
			if f.Sel.Name == "add" { // views' notes queues: v.notes.add(h, text)
				if x, ok := f.X.(*ast.SelectorExpr); ok && x.Sel.Name == "notes" {
					return true
				}
			}
		}
		return false
	}
	// reads reports whether e reads an error's text, tainted being the
	// function's locals that hold one
	var reads func(e ast.Node, tainted map[string]bool) bool
	reads = func(e ast.Node, tainted map[string]bool) bool {
		found := false
		ast.Inspect(e, func(n ast.Node) bool {
			if found {
				return false
			}
			switch x := n.(type) {
			case *ast.FuncLit:
				return false // a callback's own body is its own concern
			case *ast.CallExpr:
				switch f := x.Fun.(type) {
				case *ast.SelectorExpr:
					if f.Sel.Name == "Error" && len(x.Args) == 0 {
						found = true
					}
					if formats[f.Sel.Name] {
						for _, a := range x.Args[1:] {
							if id, ok := a.(*ast.Ident); ok && errName.MatchString(id.Name) {
								found = true
							}
						}
					}
					if helpers[f.Sel.Name] {
						found = true
					}
				case *ast.Ident:
					if helpers[f.Name] {
						found = true
					}
				}
			case *ast.Ident:
				if tainted[x.Name] {
					found = true
				}
			}
			return !found
		})
		return found
	}
	var bad []string
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			return err
		}
		check := func(body ast.Node) {
			tainted := map[string]bool{}
			ast.Inspect(body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for i, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok && i < len(x.Rhs) && reads(x.Rhs[i], tainted) {
							tainted[id.Name] = true
						}
					}
				case *ast.CallExpr:
					if toastish(x) {
						for _, a := range x.Args {
							if reads(a, tainted) {
								bad = append(bad, fs.Position(x.Pos()).String())
								break
							}
						}
					}
				}
				return true
			})
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				check(fd.Body)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Errorf("a toast shows an error's text (log it; toast Slack's text or popups.FailedToast): %s", b)
	}
}
