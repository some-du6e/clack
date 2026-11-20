package ui

// Every overlay kind boxes its layer for the inspect tool (reviews of
// a8fa243c and 3a75558d, inspect-blur-fast): a popup whose layer ends with
// no box, neither an InspectCard at its card nor a Rect for drawOverlay's
// safety net, has its picks fall through to the modal or the window column
// under it (date pickers, time and select lists, the shortcuts picker,
// Block Kit's wrapper that hid its popup's Rect). This reads internal/ui's
// sources (go/ast, no type checking): every type whose methods, its
// embedded types' included, have Layout and Dismissed and that isn't
// Invisible is an overlay kind ShowOverlay can show, and each must box its
// layer one of three ways: it has a Rect; a method of its own calls
// InspectCard or EndCardAt; or its Layout lays out a field (directly or
// through a local copy of it) of a type that does. The ones that can't are
// listed below with why, and the list is kept to what still needs it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// unboxedOverlays are the overlay kinds with no card box, and why.
var unboxedOverlays = map[string]string{
	"popups.pfPreviewDim": "the profile preview's dim around the pane: no card of its own; what shows through it is the window, which the picker reports",
}

const uiModule = "github.com/some-du6e/clack/internal/ui"

type okRef struct{ dir, name string }

func (r okRef) String() string {
	d := r.dir
	if d == "." {
		d = "ui"
	}
	return filepath.Base(d) + "." + r.name
}

type okType struct {
	embeds  []okRef
	fields  map[string]okRef
	iface   []string // an interface type's methods
	isIface bool
	methods map[string]*ast.FuncDecl
}

func TestOverlayKindsBoxTheirLayer(t *testing.T) {
	types := map[okRef]*okType{}
	get := func(r okRef) *okType {
		ty := types[r]
		if ty == nil {
			ty = &okType{fields: map[string]okRef{}, methods: map[string]*ast.FuncDecl{}}
			types[r] = ty
		}
		return ty
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		imports := map[string]string{} // alias → dir, internal/ui's packages only
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			var to string
			switch {
			case p == uiModule:
				to = "."
			case strings.HasPrefix(p, uiModule+"/"):
				to = strings.TrimPrefix(p, uiModule+"/")
			default:
				continue
			}
			alias := filepath.Base(p)
			if im.Name != nil {
				alias = im.Name.Name
			}
			imports[alias] = to
		}
		ref := func(e ast.Expr) (okRef, bool) {
			for {
				switch x := e.(type) {
				case *ast.StarExpr:
					e = x.X
					continue
				case *ast.IndexExpr:
					e = x.X
					continue
				case *ast.IndexListExpr:
					e = x.X
					continue
				case *ast.Ident:
					return okRef{dir, x.Name}, true
				case *ast.SelectorExpr:
					if pk, ok := x.X.(*ast.Ident); ok {
						if to, ok := imports[pk.Name]; ok {
							return okRef{to, x.Sel.Name}, true
						}
					}
				}
				return okRef{}, false
			}
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					ty := get(okRef{dir, ts.Name.Name})
					switch st := ts.Type.(type) {
					case *ast.StructType:
						for _, fl := range st.Fields.List {
							r, ok := ref(fl.Type)
							if !ok {
								continue
							}
							if len(fl.Names) == 0 {
								ty.embeds = append(ty.embeds, r)
							}
							for _, n := range fl.Names {
								ty.fields[n.Name] = r
							}
						}
					case *ast.InterfaceType:
						ty.isIface = true
						for _, m := range st.Methods.List {
							for _, n := range m.Names {
								ty.iface = append(ty.iface, n.Name)
							}
							if len(m.Names) == 0 {
								if r, ok := ref(m.Type); ok {
									ty.embeds = append(ty.embeds, r)
								}
							}
						}
					default:
						if r, ok := ref(ts.Type); ok && ts.Assign != 0 {
							ty.embeds = append(ty.embeds, r) // an alias: its target's methods
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 || d.Body == nil {
					continue
				}
				if r, ok := ref(d.Recv.List[0].Type); ok {
					get(r).methods[d.Name.Name] = d
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var mset func(r okRef, seen map[okRef]bool) map[string]bool
	mset = func(r okRef, seen map[okRef]bool) map[string]bool {
		out := map[string]bool{}
		ty := types[r]
		if ty == nil || seen[r] {
			return out
		}
		seen[r] = true
		for n := range ty.methods {
			out[n] = true
		}
		for _, n := range ty.iface {
			out[n] = true
		}
		for _, e := range ty.embeds {
			for n := range mset(e, seen) {
				out[n] = true
			}
		}
		return out
	}
	boxes := map[string]bool{"InspectCard": true, "EndCardAt": true}
	memo := map[okRef]bool{}
	var boxed func(r okRef, busy map[okRef]bool) bool
	boxed = func(r okRef, busy map[okRef]bool) bool {
		if v, ok := memo[r]; ok {
			return v
		}
		ty := types[r]
		if ty == nil || busy[r] {
			return false
		}
		busy[r] = true
		defer delete(busy, r)
		ok := mset(r, map[okRef]bool{})["Rect"]
		for _, fd := range ty.methods {
			if ok {
				break
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, is := n.(*ast.CallExpr); is {
					switch fn := c.Fun.(type) {
					case *ast.SelectorExpr:
						ok = ok || boxes[fn.Sel.Name]
					case *ast.Ident:
						ok = ok || boxes[fn.Name]
					}
				}
				return !ok
			})
		}
		if lay := ty.methods["Layout"]; !ok && lay != nil && len(lay.Recv.List[0].Names) > 0 {
			recv := lay.Recv.List[0].Names[0].Name
			field := func(e ast.Expr) string { // recv.f
				if s, is := e.(*ast.SelectorExpr); is {
					if id, is := s.X.(*ast.Ident); is && id.Name == recv {
						return s.Sel.Name
					}
				}
				return ""
			}
			copies := map[string]string{} // m := recv.f
			ast.Inspect(lay.Body, func(n ast.Node) bool {
				if as, is := n.(*ast.AssignStmt); is && len(as.Lhs) == len(as.Rhs) {
					for k, l := range as.Lhs {
						if id, is := l.(*ast.Ident); is {
							if f := field(as.Rhs[k]); f != "" {
								copies[id.Name] = f
							}
						}
					}
				}
				return true
			})
			ast.Inspect(lay.Body, func(n ast.Node) bool {
				c, is := n.(*ast.CallExpr)
				if !is || ok {
					return !ok
				}
				s, is := c.Fun.(*ast.SelectorExpr)
				if !is || s.Sel.Name != "Layout" {
					return true
				}
				f := field(s.X)
				if id, is := s.X.(*ast.Ident); is && f == "" {
					f = copies[id.Name]
				}
				if fr, has := ty.fields[f]; has && boxed(fr, busy) {
					ok = true
				}
				return !ok
			})
		}
		for _, e := range ty.embeds {
			if !ok && boxed(e, busy) {
				ok = true
			}
		}
		memo[r] = ok
		return ok
	}

	var kinds, unboxed []string
	isKind := map[string]bool{}
	for r, ty := range types {
		if ty.isIface {
			continue
		}
		ms := mset(r, map[okRef]bool{})
		if !ms["Layout"] || !ms["Dismissed"] || ms["Invisible"] {
			continue
		}
		kinds = append(kinds, r.String())
		isKind[r.String()] = true
		if !boxed(r, map[okRef]bool{}) {
			unboxed = append(unboxed, r.String())
		}
	}
	sort.Strings(unboxed)
	if len(kinds) < 40 {
		t.Fatalf("found %d overlay kinds in internal/ui, want 40 or more: the scan is broken (%v)", len(kinds), kinds)
	}
	still := map[string]bool{}
	for _, k := range unboxed {
		if why, ok := unboxedOverlays[k]; ok {
			still[k] = true
			t.Logf("%s: no card box (%s)", k, why)
			continue
		}
		t.Errorf("overlay kind %s boxes no layer: give it a Rect (its card, window coords) and an InspectCard at its card", k)
	}
	for k := range unboxedOverlays {
		if !isKind[k] || !still[k] {
			t.Errorf("unboxedOverlays lists %s, which is no unboxed overlay kind now: drop it", k)
		}
	}
	// what the list exempts says so at run time (InspectNoCard), so a
	// session over it logs no missing box, and nothing else does
	for r := range types {
		k := r.String()
		if !isKind[k] {
			continue
		}
		_, listed := unboxedOverlays[k]
		switch says := mset(r, map[okRef]bool{})["InspectNoCard"]; {
		case says && !listed:
			t.Errorf("overlay kind %s says it has no card (InspectNoCard), which unboxedOverlays doesn't list: list it with why, or box it", k)
		case listed && !says:
			t.Errorf("unboxedOverlays lists %s, which doesn't say so (InspectNoCard): a session over it logs a missing box", k)
		}
	}
	t.Logf("%d overlay kinds, %d with no card box listed", len(kinds), len(still))
}
