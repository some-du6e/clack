package applog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The log file keeps no message text: no log call in Clack passes a
// message's (or a draft's, a search's, a response body's) text. Redact
// catches tokens and cookies, not prose, so this is checked in the
// source: an argument to log.Print*/Fatal*/Panic* (or a .logf wrapper)
// that reads a field or variable named like text fails. Dev tools
// (probes, demos, the fake server) are skipped.
func TestLogCallsCarryNoMessageText(t *testing.T) {
	root := filepath.Join("..", "..")
	skip := func(p string) bool {
		p = filepath.ToSlash(p)
		for _, s := range []string{"/probe/", "-demo/", "/fakeslack/", "/genpack/"} {
			if strings.Contains(p, s) {
				return true
			}
		}
		return strings.HasSuffix(p, "_test.go")
	}
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || skip(p) {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		n, bad := textInLogCalls(fset, f)
		checked += n
		for _, b := range bad {
			t.Error(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("checked only %d log calls; the scan isn't finding Clack's source", checked)
	}
}

// The scan itself catches what it should.
func TestLogTextScan(t *testing.T) {
	src := `package x
func f() {
	log.Printf("sent %s", m.Text)
	log.Print(text)
	t.logf("q %s", strings.ToLower(query))
	log.Printf("ok %s %d", m.Channel, len(items))
	fmt.Printf("%s", m.Text)
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n, bad := textInLogCalls(fset, f)
	if n != 4 || len(bad) != 3 {
		t.Errorf("%d log calls, flagged %q; want 4 calls, 3 flagged", n, bad)
	}
}

var (
	textNames = map[string]bool{"Text": true, "text": true, "Query": true, "query": true, "Draft": true, "draft": true,
		"Body": true, "body": true, "Content": true, "content": true, "Plain": true, "Markdown": true, "Mrkdwn": true}
	logFuncs = map[string]bool{"Print": true, "Printf": true, "Println": true, "Fatal": true, "Fatalf": true, "Fatalln": true,
		"Panic": true, "Panicf": true, "Panicln": true}
)

// textInLogCalls counts f's log calls and describes the ones that pass
// something named like text.
func textInLogCalls(fset *token.FileSet, f *ast.File) (calls int, bad []string) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, _ := sel.X.(*ast.Ident)
		isLog := x != nil && x.Name == "log" && logFuncs[sel.Sel.Name]
		if !isLog && sel.Sel.Name != "logf" { // taut's t.logf wraps log.Printf
			return true
		}
		calls++
		for _, arg := range call.Args {
			ast.Inspect(arg, func(n ast.Node) bool {
				name := ""
				switch e := n.(type) {
				case *ast.SelectorExpr:
					name = e.Sel.Name
				case *ast.Ident:
					name = e.Name
				}
				if textNames[name] {
					bad = append(bad, fset.Position(n.Pos()).String()+": a log call passes "+name+" (message text must not reach the log file)")
					return false
				}
				return true
			})
		}
		return true
	})
	return calls, bad
}
