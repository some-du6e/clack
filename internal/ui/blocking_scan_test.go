package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Source scans for the macOS freeze (2026-11-08): Gio runs window events
// and layout on the AppKit main thread, so the UI goroutine must never
// exec, and the OS colour scheme may only be read by its watcher.

// execAllowed are the files under internal/ui that may import os/exec,
// each because its exec calls only run off the UI goroutine.
var execAllowed = map[string]string{
	"openurl/openurl.go":       "Open starts the browser on its own goroutine",
	"composer/attach.go":       "attRun/attHave run on the picker and clipboard-image goroutines",
	"composer/attach_linux.go": "attNativePick runs on Pick's goroutine",
	"composer/attach_other.go": "attNativePick runs on Pick's goroutine",
	"popups/emoji_add.go":      "the chooser lookup and dialog run in background goroutines",
}

type uiFile struct {
	rel  string // relative to internal/ui, slash-separated
	fset *token.FileSet
	f    *ast.File
}

func parseUI(t *testing.T) []uiFile {
	t.Helper()
	var out []uiFile
	err := filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		out = append(out, uiFile{rel: filepath.ToSlash(p), fset: fset, f: f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func importsPath(f *ast.File, path string) (name string, ok bool) {
	for _, im := range f.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p == path {
			name = filepath.Base(path)
			if im.Name != nil {
				name = im.Name.Name
			}
			return name, true
		}
	}
	return "", false
}

// TestNoExecOnUIPaths: only execAllowed files import os/exec, and every
// function (or function literal) there that calls into exec either runs as
// a goroutine or calls uiguard.OffUI first, so a UI-goroutine caller shows
// up in tests and logs.
func TestNoExecOnUIPaths(t *testing.T) {
	seen := map[string]bool{}
	for _, uf := range parseUI(t) {
		name, ok := importsPath(uf.f, "os/exec")
		if !ok {
			continue
		}
		seen[uf.rel] = true
		if _, ok := execAllowed[uf.rel]; !ok {
			t.Errorf("%s imports os/exec: run commands off the UI goroutine and add the file to execAllowed with why (macOS freezes on any wait in a frame)", uf.rel)
			continue
		}
		checkExecGuarded(t, uf, name)
	}
	for rel := range execAllowed {
		if !seen[rel] {
			t.Errorf("execAllowed lists %s, which doesn't import os/exec any more: drop it", rel)
		}
	}
}

func checkExecGuarded(t *testing.T, uf uiFile, execName string) {
	t.Helper()
	type fn struct {
		node    ast.Node
		guarded bool // calls uiguard.OffUI, or is a go statement's func literal
	}
	var stack []*fn
	goLits := map[*ast.FuncLit]bool{}
	ast.Inspect(uf.f, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		switch x := n.(type) {
		case *ast.GoStmt:
			if fl, ok := x.Call.Fun.(*ast.FuncLit); ok {
				goLits[fl] = true
			}
		case *ast.FuncDecl, *ast.FuncLit:
			f := &fn{node: n}
			if fl, ok := n.(*ast.FuncLit); ok && goLits[fl] {
				f.guarded = true
			}
			var body *ast.BlockStmt
			if d, ok := n.(*ast.FuncDecl); ok {
				body = d.Body
			} else {
				body = n.(*ast.FuncLit).Body
			}
			if body != nil && callsOffUI(body) {
				f.guarded = true
			}
			stack = append(stack, f)
			return true
		}
		// keep the stack balanced: push a placeholder for every other node
		stack = append(stack, nil)
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == execName {
				guarded := false
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] != nil {
						guarded = stack[i].guarded
						break
					}
				}
				if !guarded {
					t.Errorf("%s: %s.%s in a function that may run on the UI goroutine: call it from a goroutine, or call uiguard.OffUI first",
						uf.fset.Position(sel.Pos()), execName, sel.Sel.Name)
				}
			}
		}
		return true
	})
}

// callsOffUI reports whether body calls uiguard.OffUI directly (not in a
// nested function literal).
func callsOffUI(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "OffUI" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "uiguard" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// TestOSPrefersDarkOffUI: osPrefersDark (the platform's blocking read: it
// was an exec of `defaults` on macOS, run by the Appearance page on every
// frame) is only called from goroutines, and nothing reachable from a
// layout function calls it; layout reads osSchemeDark's atomic.
func TestOSPrefersDarkOffUI(t *testing.T) {
	type decl struct {
		name  string
		calls map[string]bool // callee names, outside go statements
		pos   token.Position
	}
	var decls []*decl
	layoutRoots := map[string]bool{}
	for _, uf := range parseUI(t) {
		if strings.Contains(uf.rel, "/") { // package ui only
			continue
		}
		for _, d := range uf.f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := fd.Name.Name
			de := &decl{name: name, calls: map[string]bool{}, pos: uf.fset.Position(fd.Pos())}
			decls = append(decls, de)
			if takesLayoutContext(fd.Type) || name == "Layout" || name == "frame" || name == "drain" {
				layoutRoots[name] = true
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if g, ok := n.(*ast.GoStmt); ok {
					// a goroutine is off the UI goroutine; its arguments
					// are still evaluated here
					for _, a := range g.Call.Args {
						collectCalls(a, de.calls)
					}
					if _, lit := g.Call.Fun.(*ast.FuncLit); !lit {
						collectCalls(g.Call.Fun, nil)
					}
					return false
				}
				if c, ok := n.(*ast.CallExpr); ok {
					if n := calleeName(c.Fun); n != "" {
						de.calls[n] = true
					}
				}
				// function values handed to Post, dispatch, callbacks… run
				// on the UI goroutine later: count references too
				if id, ok := n.(*ast.Ident); ok {
					de.calls[id.Name] = true
				}
				if sel, ok := n.(*ast.SelectorExpr); ok {
					de.calls[sel.Sel.Name] = true
				}
				return true
			})
			if de.calls["osPrefersDark"] {
				t.Errorf("%s: %s calls osPrefersDark outside a goroutine (read osSchemeDark instead)", de.pos, name)
			}
		}
	}
	byName := map[string][]*decl{}
	for _, d := range decls {
		byName[d.name] = append(byName[d.name], d)
	}
	// reachability from layout functions, by name (an over-approximation:
	// any method with the name counts)
	reach := map[string]string{} // name -> how it was reached
	var queue []string
	for r := range layoutRoots {
		reach[r] = r
		queue = append(queue, r)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range byName[n] {
			for c := range d.calls {
				if _, ok := reach[c]; ok {
					continue
				}
				if c == "osPrefersDark" {
					t.Errorf("osPrefersDark is reachable from layout: %s -> %s", reach[n], c)
				}
				if len(byName[c]) > 0 {
					reach[c] = reach[n] + " -> " + c
					queue = append(queue, c)
				}
			}
		}
	}
	if !layoutRoots["appearancePage"] {
		t.Error("the scan didn't find appearancePage as a layout function")
	}
	if _, ok := reach["followOSTheme"]; !ok {
		t.Log("followOSTheme isn't reachable from layout (fine)")
	}
}

func collectCalls(n ast.Node, into map[string]bool) {
	if into == nil {
		return
	}
	ast.Inspect(n, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if name := calleeName(c.Fun); name != "" {
				into[name] = true
			}
		}
		return true
	})
}

func calleeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr:
		return calleeName(x.X)
	}
	return ""
}

func takesLayoutContext(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Context" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "layout" {
				return true
			}
		}
	}
	return false
}
