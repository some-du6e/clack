package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The body of `for e := range app.Events` runs inside the OS's open-URL
// callback on the main thread (macOS, Windows). Anything that waits for the
// main thread there, such as Window.Perform (a.Raise), deadlocks: Clack
// beachballed on 2026-11-08 when a browser handed it a sign-in link. So
// the body may only inspect the event and start goroutines.
func TestEventsLoopOnlyHandsOff(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		sel, ok := rs.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Events" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "app" {
			return true
		}
		found = true
		ast.Inspect(rs.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.GoStmt:
				return false // a goroutine may do anything
			case *ast.FuncLit:
				return false // only reachable through a go statement (checked above) or unused
			case *ast.CallExpr:
				// u.URL.String() is a plain method on the event; anything else
				// could block the main thread.
				if s, ok := x.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "String" {
					return true
				}
				t.Errorf("%s: call in the app.Events loop body; hand it to a goroutine", fset.Position(x.Pos()))
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("no `range app.Events` loop in main.go")
	}
}
