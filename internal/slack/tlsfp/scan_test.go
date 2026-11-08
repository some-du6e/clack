package tlsfp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Exceptions to TestOnlyTLSFPDials, by file (relative to the module root)
// and rule, each with the reason.
var scanAllow = map[string]map[string]string{
	// Amazon Chime's signalling socket for huddles: a Chime host and Chime's
	// join token, never a Slack host or Slack credentials.
	"internal/huddle/session.go": {"websocket.Dial without HTTPClient": "Chime, not Slack"},
}

// Every connection that can reach Slack is made by this package: no other
// non-test file may import crypto/tls or utls, build an http.Transport,
// use Go's default client or transport, build an http.Client without a
// Transport (which means Go's default), call http.Get and friends, dial a
// websocket without an HTTPClient, import x/net/http2, or change a
// transport's dial or TLS fields. A new client toward Slack must use
// NewTransport (credentials) or Mixed/Client (anything else).
func TestOnlyTLSFPDials(t *testing.T) {
	root := moduleRoot(t)
	files := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == "node_modules" || (strings.HasPrefix(n, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		files++
		for _, v := range scanFile(t, path, rel) {
			if _, ok := scanAllow[rel][v.rule]; ok {
				continue
			}
			t.Errorf("%s:%d: %s (go through internal/slack/tlsfp)", rel, v.line, v.rule)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 {
		t.Fatalf("scanned only %d files from %s", files, root)
	}
}

// The scanner itself catches each pattern.
func TestScanCatches(t *testing.T) {
	src := `package x
import (
	"crypto/tls"
	"net/http"
	ws "github.com/coder/websocket"
	_ "golang.org/x/net/http2"
)
var a = &http.Transport{}
var b = http.DefaultClient
var c = http.Client{Timeout: 1}
var d = new(http.Transport)
func f(tr *http.Transport) {
	http.Get("https://slack.com")
	tls.Dial("tcp", "slack.com:443", nil)
	ws.Dial(nil, "wss://x", nil)
	ws.Dial(nil, "wss://x", &ws.DialOptions{})
	tr.TLSClientConfig = nil
	_ = http.DefaultTransport
}
`
	p := filepath.Join(t.TempDir(), "x.go")
	os.WriteFile(p, []byte(src), 0o600)
	got := map[string]int{}
	for _, v := range scanFile(t, p, "internal/x/x.go") {
		got[v.rule]++
	}
	for rule, n := range map[string]int{
		"imports crypto/tls": 1, "imports golang.org/x/net/http2": 1, "builds an http.Transport": 2,
		"uses http.DefaultClient": 1, "uses http.DefaultTransport": 1, "http.Client without Transport": 1,
		"calls http.Get": 1, "websocket.Dial without HTTPClient": 2, "sets TLSClientConfig": 1,
	} {
		if got[rule] != n {
			t.Errorf("%q: %d hits, want %d (all: %v)", rule, got[rule], n, got)
		}
	}
	for _, v := range scanFile(t, p, "internal/slack/tlsfp/transport.go") {
		switch v.rule {
		case "websocket.Dial without HTTPClient", "calls http.Get", "imports golang.org/x/net/http2", "http.Client without Transport":
		default:
			t.Errorf("transport.go may %s", v.rule)
		}
	}
}

type violation struct {
	line int
	rule string
}

func scanFile(t *testing.T, path, rel string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	inPkg := strings.HasPrefix(rel, "internal/slack/tlsfp/")
	isTransport := rel == "internal/slack/tlsfp/transport.go"
	var out []violation
	add := func(n ast.Node, rule string) { out = append(out, violation{fset.Position(n.Pos()).Line, rule}) }

	names := map[string]string{} // local name -> import path
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		names[name] = p
		switch p {
		case "crypto/tls", "github.com/refraction-networking/utls":
			if !inPkg {
				add(im, "imports "+p)
			}
		case "golang.org/x/net/http2":
			add(im, "imports "+p)
		}
	}
	// is reports whether e is pkg.Sel for the package at path.
	is := func(e ast.Expr, path, sel string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != sel {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && names[id.Name] == path
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			switch {
			case is(n.Type, "net/http", "Transport"):
				if !isTransport {
					add(n, "builds an http.Transport")
				}
			case is(n.Type, "net/http", "Client"):
				has := false
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Transport" {
							has = true
						}
					}
				}
				if !has {
					add(n, "http.Client without Transport")
				}
			}
		case *ast.ValueSpec:
			if n.Type != nil && is(n.Type, "net/http", "Transport") && !isTransport {
				add(n, "builds an http.Transport")
			}
		case *ast.SelectorExpr:
			for _, sel := range []string{"DefaultClient", "DefaultTransport"} {
				if is(n, "net/http", sel) && !isTransport {
					add(n, "uses http."+sel)
				}
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "new" && len(n.Args) == 1 && is(n.Args[0], "net/http", "Transport") && !isTransport {
				add(n, "builds an http.Transport")
			}
			for _, fn := range []string{"Get", "Post", "Head", "PostForm"} {
				if is(n.Fun, "net/http", fn) {
					add(n, "calls http."+fn)
				}
			}
			if is(n.Fun, "github.com/coder/websocket", "Dial") && !wsHasClient(n) {
				add(n, "websocket.Dial without HTTPClient")
			}
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if s, ok := l.(*ast.SelectorExpr); ok && !inPkg {
					switch s.Sel.Name {
					case "DialTLSContext", "DialTLS", "TLSClientConfig", "TLSNextProto", "ForceAttemptHTTP2":
						add(n, "sets "+s.Sel.Name)
					}
				}
			}
		}
		return true
	})
	return out
}

// wsHasClient reports whether a websocket.Dial call passes DialOptions
// with an HTTPClient (the library's dial hook).
func wsHasClient(call *ast.CallExpr) bool {
	if len(call.Args) < 3 {
		return false
	}
	u, ok := call.Args[2].(*ast.UnaryExpr)
	if !ok {
		return false
	}
	lit, ok := u.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "HTTPClient" {
				return true
			}
		}
	}
	return false
}

func moduleRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = up
	}
}
