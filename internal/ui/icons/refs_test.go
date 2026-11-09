package icons

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestIconRefsExist reads every non-test Go file under internal/ and checks
// that each icon name written as a literal resolves (Has, or a Slack name
// HasSlack maps), so a crossed "missing" box can't ship: icons.Get /
// GetSlack arguments, Icon / icon / menuIcon / IconName fields, `icon = "…"`
// assignments, string returns from funcs named *icon*, and toolbar / icon
// list literals.
func TestIconRefsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	iconish := regexp.MustCompile(`(?i)^(menu)?icon(name)?$|^(sel|on|off|dis|alt|active)?icon$`)
	listish := regexp.MustCompile(`(?i)(toolbar|tools|icons)$`)
	nameRe := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	type ref struct{ name, at string }
	var refs []ref
	add := func(e ast.Expr) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil || !nameRe.MatchString(v) {
			return
		}
		refs = append(refs, ref{v, fset.Position(lit.Pos()).String()})
	}
	identName := func(e ast.Expr) string {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			return x.Sel.Name
		}
		return ""
	}
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if b := d.Name(); path != root && (b == "testdata" || b == "node_modules" || strings.HasPrefix(b, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		n++
		// UI code (internal/ui/...) names icons; elsewhere only icons.Get
		// calls count (core has JSON "icon" fields of its own).
		usesIcons := strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(root, "ui"))+"/")
		inIcons := f.Name.Name == "icons"
		ast.Inspect(f, func(nd ast.Node) bool {
			switch x := nd.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && identName(sel.X) == "icons" && len(x.Args) > 0 {
					switch sel.Sel.Name {
					case "Get", "GetSlack":
						add(x.Args[0])
					}
				}
				if id, ok := x.Fun.(*ast.Ident); ok && inIcons && (id.Name == "Get" || id.Name == "lazy") && len(x.Args) > 0 {
					add(x.Args[0])
				}
			case *ast.KeyValueExpr:
				if (usesIcons || inIcons) && iconish.MatchString(identName(x.Key)) {
					add(x.Value)
				}
			case *ast.AssignStmt:
				if usesIcons && len(x.Lhs) == len(x.Rhs) {
					for i, l := range x.Lhs {
						if iconish.MatchString(identName(l)) {
							add(x.Rhs[i])
						}
					}
				}
			case *ast.ValueSpec:
				for i, nm := range x.Names {
					if i < len(x.Values) && usesIcons && listish.MatchString(nm.Name) {
						if cl, ok := x.Values[i].(*ast.CompositeLit); ok {
							for _, el := range cl.Elts {
								add(el)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if usesIcons && strings.Contains(strings.ToLower(x.Name.Name), "icon") && x.Body != nil {
					ast.Inspect(x.Body, func(m ast.Node) bool {
						if r, ok := m.(*ast.ReturnStmt); ok {
							for _, e := range r.Results {
								add(e)
							}
						}
						return true
					})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 || len(refs) < 200 {
		t.Fatalf("scanned %d files, %d icon refs: the walk missed the tree", n, len(refs))
	}
	found := map[string]bool{}
	for _, r := range refs {
		found[r.name] = true
	}
	// the scanner must see the call-site shapes it claims to (menu items,
	// the composer's table, the thread toolbar list)
	for _, want := range []string{"mark-as-read", "share-message", "user-add", "bold", "code-block", "list-ordered"} {
		if !found[want] {
			t.Errorf("scanner didn't find the %q reference", want)
		}
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if Has(r.name) || HasSlack(r.name) || seen[r.name] {
			continue
		}
		seen[r.name] = true
		t.Errorf("%s: icon %q isn't in the set (it would draw the crossed box)", r.at, r.name)
	}
	t.Logf("%d files, %d icon name literals", n, len(refs))
}
