package ui

import (
	"fmt"
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
	"popups/emoji_add.go":      "the chooser lookup and dialog run in background goroutines",
	"clipimg/clipimg_linux.go": "clipimg.Write runs on popups' run goroutine (Copy image)",
	"intro_preview.go":         "the preview renderer starts and is waited for on its own goroutine",
	"intro_preview_windows.go": "only sets the renderer's SysProcAttr",
	"intro_preview_linux.go":   "only sets the renderer's SysProcAttr",
	"intro_preview_unix.go":    "only the no-op configureChild",
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
		// exec.Command, CommandContext, LookPath: calls only (exec.ExitError
		// and *exec.Cmd are just types)
		if call, ok := n.(*ast.CallExpr); ok {
			sel, _ := call.Fun.(*ast.SelectorExpr)
			if id, ok := selIdent(sel); ok && id.Name == execName {
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

func selIdent(sel *ast.SelectorExpr) (*ast.Ident, bool) {
	if sel == nil {
		return nil, false
	}
	id, ok := sel.X.(*ast.Ident)
	return id, ok
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

// TestEveryExecSetsWaitDelay (the whole module): every function that makes
// an exec.Cmd bounds its Wait with Cmd.WaitDelay (directly, or through a
// waitDelay helper). Without it, Output/Run wait for the stdio copiers until
// every holder of the pipe exits: a killed child whose grandchild kept
// stdout open blocks forever. And nothing runs macOS' `defaults`: the
// pre-fix osPrefersDark did, on the UI goroutine at every launch with
// "Sync with OS" on (2026-11-08).
func TestEveryExecSetsWaitDelay(t *testing.T) {
	var files []string
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, p := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		execName, ok := importsPath(f, "os/exec")
		if !ok {
			continue
		}
		var stack []ast.Node
		ast.Inspect(f, func(nd ast.Node) bool {
			if nd == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, nd)
			call, ok := nd.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, _ := call.Fun.(*ast.SelectorExpr)
			id, ok := selIdent(sel)
			if !ok || id.Name != execName || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			n++
			if len(call.Args) > 0 {
				arg := call.Args[0]
				if sel.Sel.Name == "CommandContext" && len(call.Args) > 1 {
					arg = call.Args[1]
				}
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Value == `"defaults"` {
					t.Errorf("%s: execs defaults (read the appearance in process: osdark_darwin.m)", fset.Position(call.Pos()))
				}
			}
			var fn ast.Node // the innermost function
			for i := len(stack) - 1; i >= 0 && fn == nil; i-- {
				switch stack[i].(type) {
				case *ast.FuncDecl, *ast.FuncLit:
					fn = stack[i]
				}
			}
			if fn == nil || !setsWaitDelay(fn) {
				t.Errorf("%s: %s.%s without Cmd.WaitDelay in the same function", fset.Position(call.Pos()), execName, sel.Sel.Name)
			}
			return true
		})
	}
	if n < 10 {
		t.Fatalf("found only %d exec.Command calls: is the scan looking in the right place?", n)
	}
}

func setsWaitDelay(fn ast.Node) bool {
	found := false
	ast.Inspect(fn, func(nd ast.Node) bool {
		switch x := nd.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if s, ok := l.(*ast.SelectorExpr); ok && s.Sel.Name == "WaitDelay" {
					found = true
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "waitDelay" {
				found = true
			}
		}
		return !found
	})
	return found
}

// bootWaitHelpers are the internal/ui functions that call core's Snapshot,
// which waits for Boot (Slack's answers, seconds on a slow network), in
// their own body: each is called only off the UI goroutine, which
// TestNoBootWaitOnUIPaths checks at every call. Everything else reads the
// backend's model, RecentSnapshot or SnapshotNow, which never wait: a
// launch restored to Activity painted nothing until Boot finished
// (2026-11-15), and with the warm boot the window shows before Boot does.
var bootWaitHelpers = map[string]string{
	"directoryUsers": "users_select's empty query (blockkit/menus.go), in run's work function",
	"dirKnownPeople": "the people directory's fallback (views/directories.go), in its load's work function",
}

// bootWaits are core's calls that wait for Boot; bootReadyChans its
// channels whose receive waits for it.
var (
	bootWaits      = map[string]bool{"Snapshot": true, "BootErr": true}
	bootReadyChans = map[string]bool{"Ready": true, "DataReady": true}
	// asyncRunners run a function literal argument whose first parameter
	// is a context.Context off the UI goroutine (blockkit's and popups'
	// run, views' async.load); AfterFunc runs any on its own goroutine.
	asyncRunners = map[string]bool{"run": true, "load": true}
	// uiPosters run a function literal argument on the UI goroutine, even
	// when called from another one.
	uiPosters = map[string]bool{"Post": true, "post": true, "postLazy": true, "PostLazy": true}
)

// TestNoBootWaitOnUIPaths: nothing under internal/ui waits for core's Boot
// where it may run on the UI goroutine. A wait is a call to Snapshot or
// BootErr (or a method value of one), a call to a bootWaitHelpers
// function, or a receive from Ready or DataReady outside a select with a
// default. It may only be in a go statement's function literal, a
// function literal an asyncRunners call (or time.AfterFunc) runs, or a
// bootWaitHelpers function itself; a function literal handed to a
// uiPosters call runs on the UI goroutine wherever it is written. Every
// function and every package-level function literal is scanned. The
// runtime half is core's boot-wait hook (backend.go's init: uiguard
// records a wait on the UI goroutine).
func TestNoBootWaitOnUIPaths(t *testing.T) {
	seenHelper := map[string]bool{}
	for _, uf := range parseUI(t) {
		scanBootWaits(uf, seenHelper, func(n ast.Node, where, what string) {
			t.Errorf("%s: %s %s, which waits for core's Boot, where it may run on the UI goroutine: read the backend's model, RecentSnapshot or SnapshotNow there; if it runs on a goroutine, start that goroutine around it (go, run's work function), or make the function a bootWaitHelpers entry with why",
				uf.fset.Position(n.Pos()), where, what)
		})
	}
	for k := range bootWaitHelpers {
		if !seenHelper[k] {
			t.Errorf("bootWaitHelpers lists %q, which no function is called any more: drop it", k)
		}
	}
}

// scanBootWaits reports uf's waits for Boot where they may run on the UI
// goroutine (TestNoBootWaitOnUIPaths' rules); seenHelper collects the
// bootWaitHelpers functions it found.
func scanBootWaits(uf uiFile, seenHelper map[string]bool, report func(n ast.Node, where, what string)) {
	{
		pkgs := map[string]bool{} // the file's import names: core.Snapshot is a type
		for _, im := range uf.f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			n := filepath.Base(p)
			if im.Name != nil {
				n = im.Name.Name
			}
			pkgs[n] = true
		}
		var walk func(n ast.Node, async bool, where string)
		walk = func(n ast.Node, async bool, where string) {
			// non-blocking selects: their case receives don't wait
			free := map[ast.Node]bool{}
			ast.Inspect(n, func(x ast.Node) bool {
				if x == nil {
					return true
				}
				if _, ok := x.(*ast.FuncLit); ok && x != n {
					return false // its own walk below
				}
				if sel, ok := x.(*ast.SelectStmt); ok {
					hasDefault := false
					for _, c := range sel.Body.List {
						if cc, ok := c.(*ast.CommClause); ok && cc.Comm == nil {
							hasDefault = true
						}
					}
					if hasDefault {
						for _, c := range sel.Body.List {
							if cc, ok := c.(*ast.CommClause); ok && cc.Comm != nil {
								free[cc.Comm] = true
							}
						}
					}
				}
				return true
			})
			var visit func(x ast.Node) bool
			visit = func(x ast.Node) bool {
				if x == nil {
					return true
				}
				switch v := x.(type) {
				case *ast.GoStmt:
					if fl, ok := v.Call.Fun.(*ast.FuncLit); ok {
						walk(fl.Body, true, where)
					} else if sel, ok := v.Call.Fun.(*ast.SelectorExpr); ok { // go x.f(): f runs off the UI goroutine, x is evaluated here
						ast.Inspect(sel.X, visit)
					}
					for _, a := range v.Call.Args { // evaluated here; a method value handed over runs there
						if !bootWaitValue(a) {
							ast.Inspect(a, visit)
						}
					}
					return false
				case *ast.CallExpr:
					name := calleeName(v.Fun)
					for _, a := range v.Args {
						fl, ok := a.(*ast.FuncLit)
						if !ok {
							continue
						}
						switch {
						case uiPosters[name]:
							walk(fl.Body, false, where)
						case name == "AfterFunc", asyncRunners[name] && firstParamIsContext(fl):
							walk(fl.Body, true, where)
						default:
							walk(fl.Body, async, where)
						}
					}
					if !async && len(v.Args) == 0 {
						if sel, ok := v.Fun.(*ast.SelectorExpr); ok && bootWaits[sel.Sel.Name] && coreReceiver(sel.X, pkgs) {
							report(v, where, "calls "+sel.Sel.Name)
						}
					}
					if !async {
						if _, ok := bootWaitHelpers[name]; ok && name != where {
							report(v, where, "calls "+name)
						}
					}
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok { // the receiver, not the call's own selector
						ast.Inspect(sel.X, visit)
					} else {
						ast.Inspect(v.Fun, visit)
					}
					handsOff := name == "AfterFunc" || asyncRunners[name] // a method value it runs off the UI goroutine
					for _, a := range v.Args {
						if _, ok := a.(*ast.FuncLit); ok || handsOff && bootWaitValue(a) {
							continue
						}
						ast.Inspect(a, visit)
					}
					return false
				case *ast.FuncLit:
					walk(v.Body, async, where)
					return false
				case *ast.SelectorExpr: // a method value (f := c.Snapshot)
					if !async && bootWaits[v.Sel.Name] && coreReceiver(v.X, pkgs) {
						report(v, where, "takes "+v.Sel.Name)
					}
				case *ast.UnaryExpr:
					if v.Op == token.ARROW && !async {
						if c, ok := v.X.(*ast.CallExpr); ok && len(c.Args) == 0 {
							if sel, ok := c.Fun.(*ast.SelectorExpr); ok && bootReadyChans[sel.Sel.Name] && !inFree(free, v) {
								report(v, where, "receives from "+sel.Sel.Name)
							}
						}
					}
				}
				return true
			}
			// the free receives are the case expressions themselves
			ast.Inspect(n, func(x ast.Node) bool {
				if x == n {
					return true
				}
				return visit(x)
			})
		}
		for _, d := range uf.f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				_, helper := bootWaitHelpers[d.Name.Name]
				if helper {
					seenHelper[d.Name.Name] = true
				}
				walk(d.Body, helper, d.Name.Name)
			case *ast.GenDecl: // package-level function literals
				for _, sp := range d.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						ast.Inspect(val, func(x ast.Node) bool {
							if fl, ok := x.(*ast.FuncLit); ok {
								walk(fl.Body, false, "a package-level func")
								return false
							}
							return true
						})
					}
				}
			}
		}
	}
}

// The scan catches the shapes a review found it missed (2026-11-16): a
// Post'ed closure inside a go statement, a wait added to a function whose
// other wait is in run's work function, a call to a helper that waits, a
// method value, a package-level function literal, and a blocking receive
// from Ready; and it lets the safe ones through.
func TestBootWaitScanCatchesShapes(t *testing.T) {
	src := `package x

import (
	"context"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

func postInGo(a *App, c *core.App) {
	go func() {
		a.Post(func() { _ = c.Snapshot() }) // bad: Post runs it on the UI goroutine
		_ = c.Snapshot()                      // fine: the goroutine itself
	}()
}

func refresh(h Host, c *core.App) {
	run(h, time.Second, func(ctx context.Context) (int, error) { return len(c.Snapshot().Convs), nil }, nil) // fine
	_ = c.Snapshot() // bad: refresh itself is on the UI goroutine
}

func callsHelper(c *core.App) { directoryUsers(c, 5) } // bad

func asyncHelper(h Host, c *core.App) {
	run(h, func(ctx context.Context) (int, error) { return len(directoryUsers(c, 5)), nil }, nil) // fine
}

func directoryUsers(c *core.App, n int) []core.User { _ = c.Snapshot(); return nil } // fine: a helper

func methodValue(c *core.App) { f := c.Snapshot; _ = f } // bad

var pkgLevel = func(c *core.App) { _ = c.BootErr() } // bad

func receive(c *core.App) { <-c.Ready() } // bad

func selects(c *core.App) {
	select {
	case <-c.DataReady(): // fine: there's a default
	default:
	}
	select {
	case <-c.Ready(): // bad: no default
	case <-time.After(time.Second):
	}
	go func() { <-c.Ready() }() // fine
	time.AfterFunc(time.Second, func() { _ = c.Snapshot() }) // fine
}

func typesOnly(s core.Snapshot) core.Snapshot { return core.Snapshot{} } // fine: types

func goNamed(v *View, c *core.App) {
	go dirKnownPeople(c)                          // fine: a helper started as a goroutine
	go v.load(c.Snapshot)                         // fine: the method value runs there
	time.AfterFunc(time.Second, v.refreshPeople)  // fine
	go v.show(c.Snapshot())                       // bad: the argument is evaluated here
}

func otherSnapshots(x *store.Store) { _ = x.CustomEmoji.Snapshot() } // fine: not core's
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	scanBootWaits(uiFile{rel: "x.go", fset: fset, f: f}, map[string]bool{}, func(n ast.Node, where, what string) {
		got = append(got, fmt.Sprintf("%d %s %s", fset.Position(n.Pos()).Line, where, what))
	})
	line := func(sub string) int {
		for i, l := range strings.Split(src, "\n") {
			if strings.Contains(l, sub) {
				return i + 1
			}
		}
		t.Fatalf("no line with %q", sub)
		return 0
	}
	want := []string{
		fmt.Sprintf("%d postInGo calls Snapshot", line("bad: Post runs it")),
		fmt.Sprintf("%d refresh calls Snapshot", line("bad: refresh itself")),
		fmt.Sprintf("%d callsHelper calls directoryUsers", line("func callsHelper")),
		fmt.Sprintf("%d methodValue takes Snapshot", line("func methodValue")),
		fmt.Sprintf("%d a package-level func calls BootErr", line("var pkgLevel")),
		fmt.Sprintf("%d receive receives from Ready", line("func receive")),
		fmt.Sprintf("%d selects receives from Ready", line("bad: no default")),
		fmt.Sprintf("%d goNamed calls Snapshot", line("bad: the argument")),
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the scan reported\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// bootWaitValue reports whether x is a method value of a boot wait
// (c.Snapshot): taken, not called.
func bootWaitValue(x ast.Expr) bool {
	sel, ok := x.(*ast.SelectorExpr)
	return ok && bootWaits[sel.Sel.Name]
}

// coreReceiver reports whether x can be the *core.App a Snapshot or
// BootErr is called on: not a package (core.Snapshot is the type), and
// not an exported field of something else (c.S.CustomEmoji.Snapshot() is
// the emoji map's copy). A variable, an unexported field (b.core) and a
// call (h.Core()) can.
func coreReceiver(x ast.Expr, pkgs map[string]bool) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return !pkgs[v.Name]
	case *ast.SelectorExpr:
		return !ast.IsExported(v.Sel.Name)
	}
	return true
}

// inFree reports whether the receive u is one of a non-blocking select's
// case expressions (case <-c.Ready():, case v := <-c.Ready():).
func inFree(free map[ast.Node]bool, u *ast.UnaryExpr) bool {
	for n := range free {
		found := false
		ast.Inspect(n, func(x ast.Node) bool {
			if x == u {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// firstParamIsContext reports whether fl's first parameter is a
// context.Context (an async runner's work function).
func firstParamIsContext(fl *ast.FuncLit) bool {
	ps := fl.Type.Params
	if ps == nil || len(ps.List) == 0 {
		return false
	}
	sel, ok := ps.List[0].Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "context"
}
