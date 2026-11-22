// Package permapguard finds a package's process-wide maps that hold one of
// its per-session objects (core's *App, fakeslack's *Server) and that the
// object's release doesn't empty. An entry in such a map keeps the object,
// and with it a whole session, reachable for the rest of the process:
// ci-speed-3 found internal/ui's tests holding 9 GB of finished Apps that
// way, and karim-1118 a bare sync.Map (core's listPrefsStates) that no
// release knew of. ci-speed-3's newPerApp / newPerServer maps register
// themselves with the release; this finds what isn't one of them.
//
// It reads the package's source (go/parser, no type check):
//
//   - a package-level map whose key or value type holds the object (a
//     *App, or a struct or slice of the package's that holds one);
//   - a package-level sync.Map that a function of the package calls with
//     such a key or value: a parameter or receiver of a type that holds
//     the object (*App, catchUpKey, []catchUpKey), a composite literal of
//     one (catchUpKey{a, ch}), or a local assigned one of those;
//
// and each must be referred to in the release function's body, or in the
// body of a function or method of the package it calls (one level), or
// carry a "permapguard:" comment saying why its entries go anyway
// (deleted when a request ends). Maps made by the package's registering
// constructor (newPerApp) are released by construction.
//
// It matches names, not objects (no type check), so it misses:
//
//   - a sync.Map reached other than by its own name (m := &bare; m.Store,
//     a field, a function's result), and a value it can't see holds the
//     object: one passed as any, an interface, a field read (s.app), or a
//     call's result;
//   - a release that only names the map (a read counts as a release, a
//     local of the same name too), or drops it two calls down.
//
// A miss of the first kind passes; of the second, it fails loudly (add a
// "permapguard:" comment, or name the map in the release).
package permapguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config names what Check looks for in a package.
type Config struct {
	Dir     string // the package's directory
	Owner   string // the per-session type: "App" (pointers to it are held)
	Release string // the method on *Owner that drops it ("Release", "forget")
	Maker   string // the constructor of self-registering maps ("newPerApp")
}

// Finding is one map the release misses.
type Finding struct {
	Var, File, Why string
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s %s", f.File, f.Var, f.Why) }

// Report is what Check found.
type Report struct {
	Missing    []Finding
	Registered int      // maps made by Maker
	Checked    []string // maps found to hold the owner, released or exempt
}

var syncMapMethods = map[string]bool{"Load": true, "Store": true, "LoadOrStore": true, "LoadAndDelete": true,
	"Delete": true, "Swap": true, "CompareAndSwap": true, "CompareAndDelete": true}

// Check reads cfg.Dir's non-test Go files.
func Check(cfg Config) (Report, error) {
	fset := token.NewFileSet()
	names, err := filepath.Glob(filepath.Join(cfg.Dir, "*.go"))
	if err != nil {
		return Report{}, err
	}
	var files []*ast.File
	fileOf := map[*ast.File]string{}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			return Report{}, err
		}
		f, err := parser.ParseFile(fset, n, b, parser.ParseComments)
		if err != nil {
			return Report{}, err
		}
		files = append(files, f)
		fileOf[f] = filepath.Base(n)
	}
	types := map[string]ast.Expr{}
	for _, f := range files {
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					types[ts.Name.Name] = ts.Type
				}
			}
		}
	}
	holds := func(e ast.Expr) bool { return holdsOwner(e, cfg.Owner, types, map[string]bool{}) }

	type pkgVar struct {
		file    string
		exempt  bool
		syncMap bool
		why     string
	}
	vars := map[string]*pkgVar{}
	var rep Report
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.VAR {
				continue
			}
			for _, s := range g.Specs {
				vs := s.(*ast.ValueSpec)
				exempt := strings.Contains(commentText(g.Doc, vs.Doc, vs.Comment), "permapguard:")
				for i, name := range vs.Names {
					var val ast.Expr
					if i < len(vs.Values) {
						val = vs.Values[i]
					}
					if c, ok := val.(*ast.CallExpr); ok {
						if id, ok := c.Fun.(*ast.Ident); ok && id.Name == cfg.Maker {
							rep.Registered++
							continue
						}
					}
					typ := vs.Type
					if typ == nil {
						typ = typeOfValue(val)
					}
					switch {
					case typ == nil:
					case isSyncMap(typ):
						vars[name.Name] = &pkgVar{file: fileOf[f], exempt: exempt, syncMap: true}
					case holds(typ) && isMapish(typ, types):
						vars[name.Name] = &pkgVar{file: fileOf[f], exempt: exempt, why: "is a map whose key or value holds a *" + cfg.Owner}
					}
				}
			}
		}
	}
	// a sync.Map holds the owner if any call passes it one
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			owned := ownerParams(fn, holds)
			// locals that hold the owner: k := catchUpKey{a, ch}, x := a
			ast.Inspect(fn, func(n ast.Node) bool {
				switch st := n.(type) {
				case *ast.AssignStmt:
					for i, l := range st.Lhs {
						id, ok := l.(*ast.Ident)
						if ok && i < len(st.Rhs) && holdsValue(st.Rhs[i], owned, holds) {
							owned[id.Name] = true
						}
					}
				case *ast.ValueSpec:
					for i, id := range st.Names {
						if (i < len(st.Values) && holdsValue(st.Values[i], owned, holds)) || (st.Type != nil && holds(st.Type)) {
							owned[id.Name] = true
						}
					}
				}
				return true
			})
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !syncMapMethods[sel.Sel.Name] {
					return true
				}
				recv, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				v := vars[recv.Name]
				if v == nil || !v.syncMap || v.why != "" {
					return true
				}
				for _, arg := range call.Args {
					if holdsValue(arg, owned, holds) {
						v.why = fmt.Sprintf("is a sync.Map that %s passes a *%s (%s)", fn.Name.Name, cfg.Owner, sel.Sel.Name)
						break
					}
				}
				return true
			})
			return false
		})
	}
	// what the release refers to, and the functions and methods of the
	// package it calls (one level: a release that hands the maps to a
	// helper, a.dropLists())
	funcs := map[string][]*ast.FuncDecl{}
	var releases []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
			if fn.Name.Name == cfg.Release && fn.Recv != nil {
				releases = append(releases, fn)
			}
		}
	}
	released := map[string]bool{}
	refers := func(body *ast.BlockStmt) {
		ast.Inspect(body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				released[id.Name] = true
			}
			return true
		})
	}
	for _, fn := range releases {
		refers(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			switch f := call.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			for _, h := range funcs[name] {
				if h != fn {
					refers(h.Body)
				}
			}
			return true
		})
	}
	if len(releases) == 0 {
		return rep, fmt.Errorf("no method %s in %s", cfg.Release, cfg.Dir)
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		v := vars[name]
		if v.why == "" {
			continue // a sync.Map nothing passes the owner
		}
		rep.Checked = append(rep.Checked, name)
		if v.exempt || released[name] {
			continue
		}
		rep.Missing = append(rep.Missing, Finding{Var: name, File: v.file, Why: v.why + ", and " + cfg.Release + " doesn't drop it (make it with " + cfg.Maker + ", or say why it empties with a \"permapguard:\" comment)"})
	}
	return rep, nil
}

func commentText(gs ...*ast.CommentGroup) string {
	var b strings.Builder
	for _, g := range gs {
		if g != nil {
			b.WriteString(g.Text())
		}
	}
	return b.String()
}

func isSyncMap(e ast.Expr) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "sync" && sel.Sel.Name == "Map"
}

// isMapish: a map type, or a local type that is one.
func isMapish(e ast.Expr, types map[string]ast.Expr) bool {
	for i := 0; i < 8; i++ {
		switch t := e.(type) {
		case *ast.MapType:
			return true
		case *ast.Ident:
			u, ok := types[t.Name]
			if !ok {
				return false
			}
			e = u
		default:
			return false
		}
	}
	return false
}

// typeOfValue is a package var's type from its value: map[...]{...},
// make(map[...]...), &sync.Map{}, sync.Map{}.
func typeOfValue(v ast.Expr) ast.Expr {
	switch x := v.(type) {
	case *ast.CompositeLit:
		return x.Type
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return typeOfValue(x.X)
		}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "make" && len(x.Args) > 0 {
			return x.Args[0]
		}
	}
	return nil
}

// holdsOwner: the type is or holds a pointer to the owner.
func holdsOwner(e ast.Expr, owner string, types map[string]ast.Expr, seen map[string]bool) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok && id.Name == owner {
			return true
		}
		return holdsOwner(t.X, owner, types, seen)
	case *ast.Ident:
		if seen[t.Name] {
			return false
		}
		seen[t.Name] = true
		if u, ok := types[t.Name]; ok {
			return holdsOwner(u, owner, types, seen)
		}
	case *ast.MapType:
		return holdsOwner(t.Key, owner, types, seen) || holdsOwner(t.Value, owner, types, seen)
	case *ast.ArrayType:
		return holdsOwner(t.Elt, owner, types, seen)
	case *ast.ChanType:
		return holdsOwner(t.Value, owner, types, seen)
	case *ast.StructType:
		for _, f := range t.Fields.List {
			if holdsOwner(f.Type, owner, types, seen) {
				return true
			}
		}
	}
	return false
}

// ownerParams are fn's receiver and parameters of a type that holds the
// owner (*App, catchUpKey{a *App; ...}, []catchUpKey).
func ownerParams(fn *ast.FuncDecl, holds func(ast.Expr) bool) map[string]bool {
	m := map[string]bool{}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if !holds(f.Type) {
				continue
			}
			for _, n := range f.Names {
				m[n.Name] = true
			}
		}
	}
	add(fn.Recv)
	add(fn.Type.Params)
	return m
}

// holdsValue: the argument is an owner-typed parameter, or a composite
// literal (or its address) of a type that holds the owner.
func holdsValue(e ast.Expr, owned map[string]bool, holds func(ast.Expr) bool) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return owned[x.Name]
	case *ast.UnaryExpr:
		return holdsValue(x.X, owned, holds)
	case *ast.CompositeLit:
		if x.Type != nil && holds(x.Type) {
			return true
		}
		for _, el := range x.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
			}
			if holdsValue(el, owned, holds) {
				return true
			}
		}
	}
	return false
}
