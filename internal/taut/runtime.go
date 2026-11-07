package taut

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

// The TautAPI is a JS library (js/*.js, Clack's own implementation of
// Taut's contract) that runs on jsrt next to the plugins. It reads Slack
// state from Go through host functions (__clack.taut_*) and Go drives it
// through __taut.* (runtime.call).

//go:embed js/*.js
var jsFiles embed.FS

// prelude is every js/ file in name order inside one function scope, so the
// files share their top-level names without leaking them to plugins.
var prelude = func() string {
	names, _ := fs.Glob(jsFiles, "js/*.js")
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("(function () {\n'use strict';\n")
	for _, n := range names {
		src, _ := jsFiles.ReadFile(n)
		fmt.Fprintf(&b, "// ---- %s ----\n", n)
		b.Write(src)
		b.WriteString("\n")
	}
	b.WriteString("})();\n")
	return b.String()
}()

// engine is the slice of jsrt the manager uses; tests and the ui root see
// the real runtime through Taut.Runtime.
type runtime struct {
	rt     *jsrt.Runtime
	t      *Taut
	closed chan struct{}
	once   sync.Once
}

// startRuntime creates the JS runtime, installs the host functions and
// evaluates the TautAPI. It runs once, when the first plugin is enabled (or
// a settings editor needs JS), never at Clack start without plugins.
func (t *Taut) startRuntime() (*runtime, error) {
	opts := jsrt.Options{
		Fetch: t.opts.Fetch,
		Storage: func(ns string) jsrt.KV {
			kv := t.data.localStorage(ns)
			if ns == "sessionStorage" {
				return kv
			}
			return &virtualKV{base: kv, t: t}
		},
		Log:            t.jsLog,
		CacheDir:       t.opts.CacheDir,
		OpenURL:        t.opts.OpenURL,
		ClipboardWrite: t.opts.ClipboardWrite,
		ClipboardRead:  t.opts.ClipboardRead,
		Timeout:        t.opts.TaskTimeout,
		PickFiles:      t.opts.PickFiles,
	}
	rt, err := jsrt.New(opts)
	if err != nil {
		return nil, fmt.Errorf("taut: start JS runtime: %w", err)
	}
	r := &runtime{rt: rt, t: t, closed: make(chan struct{})}
	t.installHost(r)
	rt.OnCommit(func(id uint64, tree []byte) {
		if d := t.opts.UI.Deliver; d != nil {
			d(id, tree)
		}
	})
	if f := t.opts.UI.OnDocument; f != nil {
		rt.OnDocument(f)
	}
	if f := t.opts.UI.OnDocumentProps; f != nil {
		rt.OnDocumentProps(f)
	}
	if _, err := rt.Eval("taut-runtime.js", prelude); err != nil {
		rt.Close()
		return nil, fmt.Errorf("taut: load the TautAPI: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := rt.Call(ctx, "__taut.init"); err != nil {
		rt.Close()
		return nil, fmt.Errorf("taut: init the TautAPI: %w", err)
	}
	if err := t.installExtensions(rt); err != nil {
		rt.Close()
		return nil, err
	}
	return r, nil
}

// call runs __taut.<fn>(args…) and decodes its (awaited) result into out.
func (r *runtime) call(ctx context.Context, fn string, out any, args ...any) error {
	raw, err := r.rt.Call(ctx, "__taut."+fn, args...)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// post runs __taut.<fn>(args…) without waiting (events, invalidations).
func (r *runtime) post(fn string, args ...any) {
	r.rt.Post(func(js *jsrt.Ctx) {
		f := js.Global().Get("__taut").Get(fn)
		if !f.IsFunction() {
			r.t.logf("taut: __taut.%s is not a function", fn)
			return
		}
		vals := make([]any, len(args))
		for i, a := range args {
			b, err := json.Marshal(a)
			if err != nil {
				return
			}
			v, err := js.ParseJSON(b)
			if err != nil {
				return
			}
			vals[i] = v
		}
		if _, err := f.CallThis(js.Global().Get("__taut"), vals...); err != nil {
			r.t.logf("taut: %s: %v", fn, err)
		}
	})
}

func (r *runtime) close() {
	r.once.Do(func() {
		close(r.closed)
		r.rt.Close()
	})
}

func (t *Taut) jsLog(level, msg string) {
	if t.opts.Log != nil {
		t.opts.Log(level, msg)
		return
	}
	if level == "error" || level == "warn" {
		t.logf("taut js %s: %s", level, msg)
	}
}

// jsonArg decodes args[i] into dst, leaving dst as is when absent or null.
func jsonArg(args []json.RawMessage, i int, dst any) {
	if i >= len(args) || len(args[i]) == 0 || string(args[i]) == "null" {
		return
	}
	_ = json.Unmarshal(args[i], dst)
}

func strArg(args []json.RawMessage, i int) string {
	var s string
	jsonArg(args, i, &s)
	return s
}
