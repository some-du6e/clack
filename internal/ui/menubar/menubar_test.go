package menubar

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	a, b := Item{ID: "a", Title: "A"}, Item{ID: "b", Title: "B"}
	got := Clean([]Menu{
		{Title: "M", Items: []Item{Sep, a, Sep, Sep, b, Sep, {Title: "Empty", Sub: []Item{Sep}}, Sep}},
		{Title: "Gone", Items: []Item{Sep}},
		{Title: "H", Items: []Item{a, {Sep: true, Recent: true}}},
	})
	var s []string
	for _, m := range got {
		s = append(s, m.Title+":")
		for _, it := range m.Items {
			if it.Sep {
				s = append(s, "-")
			} else {
				s = append(s, it.Title)
			}
		}
	}
	if j := strings.Join(s, " "); j != "M: A - B H: A -" {
		t.Errorf("Clean = %q", j)
	}
}

func TestAccel(t *testing.T) {
	for _, c := range []struct {
		it   Item
		want string
	}{
		{Item{Key: "k", Mods: Cmd | Shift}, "⇧⌘K"},
		{Item{Key: "h", Mods: Option | Cmd}, "⌥⌘H"},
		{Item{Key: "f", Mods: Ctrl | Cmd}, "⌃⌘F"},
		{Item{Key: ",", Mods: Cmd}, "⌘,"},
		{Item{Key: "+", Mods: Cmd}, "⌘+"},
		{Item{}, ""},
	} {
		if got := c.it.Accel(); got != c.want {
			t.Errorf("Accel(%q, %d) = %q, want %q", c.it.Key, c.it.Mods, got, c.want)
		}
	}
}

// Tags are Flatten's depth-first order, which the native menus use.
func TestTagsAndState(t *testing.T) {
	ran := make(chan string, 4)
	menus := []Menu{
		{Title: "One", Items: []Item{
			{ID: "x", Title: "X", Do: func() { ran <- "x" }},
			{ID: "sub", Title: "Sub", Sub: []Item{{ID: "y", Title: "Y", Do: func() { ran <- "y" }}}},
		}},
		{Title: "Two", Items: []Item{Sep, {ID: "z", Title: "Z", Do: func() { ran <- "z" }}}},
	}
	Install(menus, func(route string) { ran <- "recent " + route })
	var ids []string
	for _, it := range flat {
		ids = append(ids, it.ID)
	}
	if j := strings.Join(ids, ","); j != "x,sub,y,z" { // Two's leading separator is cleaned away
		t.Fatalf("tags = %q", j)
	}
	SetState(&State{Disabled: map[string]bool{"y": true}, Titles: map[string]string{"x": "Ex"},
		Recent: []Recent{{Title: "#general", Route: "c/C1"}}})
	if enabled(2) || !enabled(0) || enabled(99) {
		t.Errorf("enabled: y %v x %v out of range %v", enabled(2), enabled(0), enabled(99))
	}
	if title(0) != "Ex" || title(3) != "" {
		t.Errorf("titles %q %q", title(0), title(3))
	}
	choose(2)
	if r := recv(t, ran); r != "y" {
		t.Errorf("choose(2) ran %q", r)
	}
	if n := len(recentShown()); n != 1 {
		t.Fatalf("recent %d", n)
	}
	SetState(&State{}) // the open menu keeps the list it showed
	chooseRecent(0)
	if r := recv(t, ran); r != "recent c/C1" {
		t.Errorf("chooseRecent ran %q", r)
	}
	chooseRecent(5) // out of range: nothing
	select {
	case r := <-ran:
		t.Errorf("chooseRecent(5) ran %q", r)
	case <-time.After(50 * time.Millisecond):
	}
}

func recv(t *testing.T, c chan string) string {
	t.Helper()
	select {
	case r := <-c:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("action never ran")
		return ""
	}
}

func TestStateEqual(t *testing.T) {
	a := &State{Disabled: map[string]bool{"x": true}, Titles: map[string]string{"t": "T"}, Recent: []Recent{{"a", "r"}}}
	b := &State{Disabled: map[string]bool{"x": true}, Titles: map[string]string{"t": "T"}, Recent: []Recent{{"a", "r"}}}
	if !a.Equal(b) {
		t.Error("equal states differ")
	}
	b.Recent[0].Route = "s"
	if a.Equal(b) {
		t.Error("different recent routes are equal")
	}
}

// AppKit calls the exported callbacks on the main thread, where waiting
// for Gio's window deadlocks (cmd/clack's TestEventsLoopOnlyHandsOff). They
// may only read state; choosing an item starts a goroutine.
func TestCallbacksOnlyHandOff(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "menubar_darwin.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"choose": true, "enabled": true, "title": true, "recentShown": true, "chooseRecent": true,
		"C.CString": true, "C.int": true, "int": true, "len": true, "shown.Load": true,
	}
	exported := 0
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !isExport(fd) {
			continue
		}
		exported++
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok {
					name = x.Name + "." + fn.Sel.Name
				}
			}
			if !allowed[name] {
				t.Errorf("%s: %s calls %s on AppKit's main thread", fset.Position(call.Pos()), fd.Name.Name, name)
			}
			return true
		})
	}
	if exported < 6 {
		t.Fatalf("found %d exported callbacks", exported)
	}
	// and the two that run actions do so on a goroutine
	g, err := parser.ParseFile(fset, "menubar.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range g.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || (fd.Name.Name != "choose" && fd.Name.Name != "chooseRecent") {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.GoStmt); ok {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "f" || id.Name == "Do") {
					t.Errorf("%s calls the action without go", fd.Name.Name)
				}
				if s, ok := call.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Do" {
					t.Errorf("%s calls the action without go", fd.Name.Name)
				}
			}
			return true
		})
	}
}

func isExport(fd *ast.FuncDecl) bool {
	if fd.Doc == nil {
		return false
	}
	for _, c := range fd.Doc.List { // Doc.Text() drops //export directives
		if strings.HasPrefix(c.Text, "//export ") {
			return true
		}
	}
	return false
}
