package jsrt

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// bootScripts are the browser environment (js/env.js …), vendored React
// (vendor/) and the React host renderer (js/react-host.js), in evaluation
// order. They come from assets.pack, decompressed for each boot and dropped
// afterwards: nothing is held while no plugin runtime runs.
func bootScripts() ([]script, error) {
	f, err := unpack()
	if err != nil {
		return nil, err
	}
	s := func(name string) string { return string(f[name]) }
	scripts := []script{{"env.js", s("js/env.js")}, {"web.js", s("js/web.js")}, {"intl.js", s("js/intl.js")}, {"dom.js", s("js/dom.js")}}
	scripts = append(scripts, vendorScripts(f)...)
	return append(scripts, script{"react-host.js", s("js/react-host.js")}), nil
}

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type cryptoRand struct{}

func (cryptoRand) Read(p []byte) (int, error) { return rand.Read(p) }

func (r *Runtime) logError(prefix string, err error) {
	msg := err.Error()
	var je *JSError
	if errors.As(err, &je) && je.Stack != "" {
		msg += "\n" + je.Stack
	}
	r.opts.Log("error", prefix+": "+msg)
}

func (r *Runtime) moduleSource(name string) (string, bool) {
	r.mu.Lock()
	src, ok := r.modules[name]
	r.mu.Unlock()
	if ok {
		return src, true
	}
	src, ok = r.builtins[name]
	return src, ok
}

// builtinModules re-export the globals set up by env.js / react-host.js.
var builtinModules = map[string]string{
	"react":                 reexport("React"),
	"react/jsx-runtime":     reexport("__clack_jsx"),
	"react/jsx-dev-runtime": reexport("__clack_jsx"),
	"react-dom":             reexport("ReactDOM"),
	"react-dom/client":      reexport("ReactDOM"),
	"scheduler":             reexport("__clack_scheduler"),
}

func reexport(global string) string {
	// Named exports are enumerated at import time from the global object.
	return fmt.Sprintf(`const m = globalThis[%q]; export default m;
export const {%s} = m;`, global, "__NAMES__")
}

// boot runs on the JS thread: engine, host functions, environment, React.
func (r *Runtime) boot() error {
	t0 := time.Now()
	e, err := newEngine(r)
	if err != nil {
		return err
	}
	r.eng = e
	r.phases = append(r.phases, Phase{"engine", time.Since(t0)})
	r.installHost()
	scripts, err := bootScripts()
	if err != nil {
		return err
	}
	for _, v := range scripts {
		t1 := time.Now()
		if err := e.evalBoot("clack:"+v.name, v.src); err != nil {
			return fmt.Errorf("jsrt: %s: %w", v.name, err)
		}
		r.phases = append(r.phases, Phase{v.name, time.Since(t1)})
	}
	r.fillModuleNames()
	e.runJobs(r.js)
	r.bootTime = time.Since(t0)
	return nil
}

// fillModuleNames replaces __NAMES__ in the builtin re-export modules with
// the current own keys of each global (valid identifiers only).
func (r *Runtime) fillModuleNames() {
	g := r.js.Global()
	r.builtins = map[string]string{}
	for name, src := range builtinModules {
		r.builtins[name] = src
		if !strings.Contains(src, "__NAMES__") {
			continue
		}
		i := strings.Index(src, `globalThis["`)
		j := strings.Index(src[i+12:], `"`)
		obj := g.Get(src[i+12 : i+12+j])
		var keys []string
		for _, k := range obj.Keys() {
			if isIdent(k) && k != "default" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		r.builtins[name] = strings.Replace(src, "__NAMES__", strings.Join(keys, ", "), 1)
	}
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	switch s {
	case "default", "delete", "new", "class", "function", "import", "export", "in", "of", "if", "for", "var", "let", "const", "this", "null", "true", "false", "void", "typeof", "return", "switch", "case", "do", "while", "try", "catch", "finally", "throw", "with", "yield", "await", "super", "enum", "extends", "static", "instanceof", "debugger", "continue", "break", "else":
		return false
	}
	return true
}

func (e *engine) evalChecked(name, src string) (uint32, error) {
	h, err := e.eval(name, src, false)
	if err == nil {
		e.release(h)
	}
	return 0, err
}

// installHost defines globalThis.__jrh: the private host functions env.js
// builds the Web APIs on (env.js captures and deletes it).
func (r *Runtime) installHost() {
	js := r.js
	h := js.NewObject()
	_ = js.Global().Set("__jrh", h)
	def := func(name string, fn HostFunc) { _ = h.Set(name, js.Func(name, fn)) }
	str := func(args []Value, i int) string {
		if i < len(args) {
			return args[i].String()
		}
		return ""
	}
	num := func(args []Value, i int) float64 {
		if i < len(args) {
			return args[i].Float()
		}
		return 0
	}

	def("log", func(js *Ctx, _ Value, a []Value) (any, error) {
		r.opts.Log(str(a, 0), str(a, 1))
		return nil, nil
	})
	def("timer", func(js *Ctx, _ Value, a []Value) (any, error) {
		d := time.Duration(num(a, 1) * float64(time.Millisecond))
		if d < 0 {
			d = 0
		}
		r.addTimer(int(num(a, 0)), d)
		return nil, nil
	})
	def("clearTimer", func(js *Ctx, _ Value, a []Value) (any, error) {
		r.timers.remove(int(num(a, 0)))
		return nil, nil
	})
	def("wake", func(js *Ctx, _ Value, a []Value) (any, error) {
		// a macrotask for setImmediate / MessageChannel / scheduler
		r.enqueue(func() {
			fn := r.js.Global().Get("__jr").Get("runImmediates")
			if fn.IsFunction() {
				if _, err := fn.Call(); err != nil {
					r.logError("uncaught", err)
				}
			}
		})
		return nil, nil
	})
	def("now", func(js *Ctx, _ Value, a []Value) (any, error) {
		return float64(time.Since(r.start).Nanoseconds()) / 1e6, nil
	})
	def("timeOrigin", func(js *Ctx, _ Value, a []Value) (any, error) {
		return float64(r.start.UnixNano()) / 1e6, nil
	})
	def("random", func(js *Ctx, _ Value, a []Value) (any, error) {
		n := int(num(a, 0))
		if n < 0 || n > 65536 {
			return nil, &JSError{Name: "QuotaExceededError", Message: "getRandomValues: too many bytes"}
		}
		b := make([]byte, n)
		_, _ = rand.Read(b)
		return b, nil
	})
	def("digest", func(js *Ctx, _ Value, a []Value) (any, error) {
		var hh hash.Hash
		switch strings.ToUpper(strings.ReplaceAll(str(a, 0), "-", "")) {
		case "SHA1":
			hh = sha1.New()
		case "SHA256":
			hh = sha256.New()
		case "SHA384":
			hh = sha512.New384()
		case "SHA512":
			hh = sha512.New()
		case "MD5":
			hh = md5.New()
		default:
			return nil, &JSError{Name: "NotSupportedError", Message: "unsupported digest " + str(a, 0)}
		}
		if len(a) > 1 {
			hh.Write(a[1].Bytes())
		}
		return hh.Sum(nil), nil
	})
	def("dirty", func(js *Ctx, _ Value, a []Value) (any, error) {
		r.eng.dirty = true
		return nil, nil
	})
	def("commit", func(js *Ctx, _ Value, a []Value) (any, error) {
		id := uint64(num(a, 0))
		r.deliver(id, []byte(str(a, 1)))
		return nil, nil
	})
	def("document", func(js *Ctx, _ Value, a []Value) (any, error) {
		if r.onDoc == nil {
			return nil, nil
		}
		var css []string
		_ = json.Unmarshal([]byte(str(a, 1)), &css)
		r.onDoc([]byte(str(a, 0)), css)
		return nil, nil
	})
	def("documentProps", func(js *Ctx, _ Value, a []Value) (any, error) {
		if r.onDocProps != nil {
			r.onDocProps([]byte(str(a, 0)), []byte(str(a, 1)))
		}
		return nil, nil
	})
	def("storage", func(js *Ctx, _ Value, a []Value) (any, error) {
		kv := r.kv(str(a, 0))
		key := str(a, 2)
		switch str(a, 1) {
		case "get":
			v, ok := kv.Get(key)
			if !ok {
				return nil, nil
			}
			return v, nil
		case "set":
			kv.Set(key, str(a, 3))
		case "del":
			kv.Delete(key)
		case "keys":
			return kv.Keys(), nil
		case "clear":
			kv.Clear()
		}
		return nil, nil
	})
	def("fetch", func(js *Ctx, _ Value, a []Value) (any, error) {
		id := int(num(a, 0))
		var req struct {
			Method   string      `json:"method"`
			URL      string      `json:"url"`
			Headers  [][2]string `json:"headers"`
			Redirect string      `json:"redirect"`
		}
		if err := json.Unmarshal([]byte(str(a, 1)), &req); err != nil {
			return nil, err
		}
		var body []byte
		if len(a) > 2 && !a[2].IsNullish() {
			body = a[2].Bytes()
		}
		r.startFetch(id, req.Method, req.URL, req.Headers, body)
		return nil, nil
	})
	def("fetchAbort", func(js *Ctx, _ Value, a []Value) (any, error) {
		if c := r.fetches[int(num(a, 0))]; c != nil {
			c()
		}
		return nil, nil
	})
	def("openURL", func(js *Ctx, _ Value, a []Value) (any, error) {
		if r.opts.OpenURL != nil {
			u := str(a, 0)
			go r.opts.OpenURL(u)
		}
		return nil, nil
	})
	def("clipboardWrite", func(js *Ctx, _ Value, a []Value) (any, error) {
		if r.opts.ClipboardWrite == nil {
			return nil, &JSError{Name: "NotAllowedError", Message: "clipboard unavailable"}
		}
		return nil, r.opts.ClipboardWrite(str(a, 0))
	})
	def("clipboardRead", func(js *Ctx, _ Value, a []Value) (any, error) {
		if r.opts.ClipboardRead == nil {
			return nil, &JSError{Name: "NotAllowedError", Message: "clipboard unavailable"}
		}
		return r.opts.ClipboardRead()
	})
	if r.opts.PickFiles != nil {
		def("pickFiles", func(js *Ctx, _ Value, a []Value) (any, error) {
			accept, multiple := str(a, 0), len(a) > 1 && a[1].Bool()
			p, resolve, reject := js.NewPromise()
			go func() {
				files, err := r.opts.PickFiles(accept, multiple)
				if err != nil {
					reject(err)
					return
				}
				if files == nil {
					resolve(nil) // cancelled
					return
				}
				resolve(pickedFiles(files))
			}()
			return p, nil
		})
	}
	def("tz", func(js *Ctx, _ Value, a []Value) (any, error) {
		return tzInfo(str(a, 0), num(a, 1))
	})
	def("listen", func(js *Ctx, _ Value, a []Value) (any, error) {
		t, d := str(a, 0), int(num(a, 1))
		r.globalMu.Lock()
		if r.global == nil {
			r.global = map[string]int{}
		}
		r.global[t] += d
		if r.global[t] <= 0 {
			delete(r.global, t)
		}
		r.globalMu.Unlock()
		return nil, nil
	})
	def("gc", func(js *Ctx, _ Value, a []Value) (any, error) {
		r.eng.call("jr_gc")
		return nil, nil
	})
	lang := r.opts.Language
	if lang == "" {
		lang = "en-US"
	}
	plat := r.opts.Platform
	if plat == "" {
		switch runtime.GOOS {
		case "darwin":
			plat = "MacIntel"
		case "windows":
			plat = "Win32"
		default:
			plat = "Linux x86_64"
		}
	}
	_ = h.Set("language", lang)
	_ = h.Set("platform", plat)
	_ = h.Set("localZone", time.Local.String())
}

// ---- storage ----

type memKV struct {
	mu   sync.Mutex
	m    map[string]string
	keys []string
}

func (k *memKV) Get(key string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, ok
}
func (k *memKV) Set(key, value string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[key]; !ok {
		k.keys = append(k.keys, key)
	}
	k.m[key] = value
}
func (k *memKV) Delete(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[key]; !ok {
		return
	}
	delete(k.m, key)
	for i, x := range k.keys {
		if x == key {
			k.keys = append(k.keys[:i], k.keys[i+1:]...)
			break
		}
	}
}
func (k *memKV) Keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.keys...)
}
func (k *memKV) Clear() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m = map[string]string{}
	k.keys = nil
}

// NewMemKV returns an in-memory KV (the default storage backing).
func NewMemKV() KV { return &memKV{m: map[string]string{}} }

func (r *Runtime) kv(ns string) KV {
	if r.kvs == nil {
		r.kvs = map[string]KV{}
	}
	if k, ok := r.kvs[ns]; ok {
		return k
	}
	var k KV
	if r.opts.Storage != nil {
		k = r.opts.Storage(ns)
	}
	if k == nil {
		k = NewMemKV()
	}
	r.kvs[ns] = k
	return k
}

// ---- fetch ----

type fetchResult struct {
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	URL        string      `json:"url"`
	Headers    [][2]string `json:"headers"`
	Redirected bool        `json:"redirected"`
}

func (r *Runtime) startFetch(id int, method, url string, headers [][2]string, body []byte) {
	ctx, cancel := context.WithCancel(r.ctx)
	r.fetches[id] = cancel
	fail := func(err error) {
		r.Post(func(js *Ctx) {
			delete(r.fetches, id)
			cancel()
			name := "TypeError"
			if errors.Is(err, context.Canceled) {
				name = "AbortError"
			}
			fn := js.Global().Get("__jr").Get("fetchDone")
			_, _ = fn.Call(id, nil, nil, name, err.Error())
		})
	}
	go func() {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		if method == "" {
			method = "GET"
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rd)
		if err != nil {
			fail(err)
			return
		}
		for _, h := range headers {
			req.Header.Add(h[0], h[1])
		}
		do := r.opts.Fetch
		if do == nil {
			do = func(ctx context.Context, req *http.Request) (*http.Response, error) {
				return http.DefaultClient.Do(req)
			}
		}
		resp, err := do(ctx, req)
		if err != nil {
			fail(err)
			return
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fail(err)
			return
		}
		res := fetchResult{Status: resp.StatusCode, StatusText: http.StatusText(resp.StatusCode), URL: url}
		if resp.Request != nil && resp.Request.URL != nil {
			res.URL = resp.Request.URL.String()
			res.Redirected = res.URL != url
		}
		for k, vs := range resp.Header {
			for _, v := range vs {
				res.Headers = append(res.Headers, [2]string{strings.ToLower(k), v})
			}
		}
		meta, _ := json.Marshal(res)
		r.Post(func(js *Ctx) {
			delete(r.fetches, id)
			cancel()
			fn := js.Global().Get("__jr").Get("fetchDone")
			_, _ = fn.Call(id, string(meta), data)
		})
	}()
}

// PickedFile is a file chosen in the native picker (Options.PickFiles).
type PickedFile struct {
	Name         string
	Type         string // MIME type, "" if unknown
	Data         []byte
	LastModified time.Time
}

// pickedFiles builds the JS list [{name, type, data: Uint8Array, lastModified}].
func pickedFiles(files []PickedFile) func(*Ctx) (Value, error) {
	return func(js *Ctx) (Value, error) {
		arr := js.NewArray()
		for i, f := range files {
			o := js.NewObject()
			_ = o.Set("name", f.Name)
			_ = o.Set("type", f.Type)
			_ = o.Set("data", f.Data)
			if !f.LastModified.IsZero() {
				_ = o.Set("lastModified", float64(f.LastModified.UnixMilli()))
			}
			if err := arr.SetIndex(i, o); err != nil {
				return Value{}, err
			}
		}
		return arr, nil
	}
}
