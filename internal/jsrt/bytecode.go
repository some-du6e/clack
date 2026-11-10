package jsrt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
)

// Boot scripts (env, React, …) are compiled to QuickJS bytecode once and
// cached in memory and, with Options.CacheDir, on disk, so later starts skip
// parsing ~600 KB of JS. Keys hash the engine wasm with the script, so a
// different engine build never reads stale bytecode. Files carry a SHA-256
// trailer and are ignored when it doesn't match.

var (
	bcMu    sync.Mutex
	bcMem   = map[[32]byte][]byte{}
	wasmSum = sync.OnceValue(func() [32]byte { return sha256.Sum256(engineWasm()) })
)

func bcKey(name, src string) [32]byte {
	h := sha256.New()
	w := wasmSum()
	h.Write(w[:])
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(src))
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

func bcPath(dir string, k [32]byte) string {
	return filepath.Join(dir, "jsrt-bc", hex.EncodeToString(k[:16])+".qbc")
}

func loadBC(dir string, k [32]byte) []byte {
	bcMu.Lock()
	b := bcMem[k]
	bcMu.Unlock()
	if b != nil || dir == "" {
		return b
	}
	data, err := os.ReadFile(bcPath(dir, k))
	if err != nil || len(data) < 32 {
		return nil
	}
	body, sum := data[:len(data)-32], data[len(data)-32:]
	if s := sha256.Sum256(body); !bytes.Equal(s[:], sum) {
		return nil
	}
	bcMu.Lock()
	bcMem[k] = body
	bcMu.Unlock()
	return body
}

func storeBC(dir string, k [32]byte, body []byte) {
	bcMu.Lock()
	bcMem[k] = body
	bcMu.Unlock()
	if dir == "" {
		return
	}
	p := bcPath(dir, k)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	s := sha256.Sum256(body)
	// a fresh O_EXCL temp file, renamed over p: never writes through a
	// symlink someone left at p or p.tmp
	f, err := os.CreateTemp(filepath.Dir(p), ".bc-*.tmp")
	if err != nil {
		return
	}
	_, err = f.Write(append(append([]byte(nil), body...), s[:]...))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
}

// evalBoot evaluates a trusted boot script through the bytecode cache
// (plain eval when the engine lacks bytecode support).
func (e *engine) evalBoot(name, src string) error {
	h, err := e.evalCached(name, src, true)
	if err == nil {
		e.release(h)
	}
	return err
}

// evalCached evaluates a classic script through the bytecode cache and
// returns its completion value's handle (plain eval without bytecode
// support). mem keeps the bytecode in memory too (boot scripts: every
// runtime evaluates them); plugins only use the disk copy.
func (e *engine) evalCached(name, src string, mem bool) (uint32, error) {
	if e.fns["jr_compile"] == nil || e.fns["jr_eval_bytecode"] == nil {
		return e.eval(name, src, false)
	}
	dir := e.r.opts.CacheDir
	k := bcKey(name, src)
	bc := loadBC(dir, k)
	if bc == nil {
		sp, sfree := e.tmpBytes([]byte(src))
		np, nfree := e.tmpBytes([]byte(name))
		pl := e.call("jr_compile", uint64(sp), uint64(len(src)), uint64(np), uint64(len(name)))
		sfree()
		nfree()
		if pl == 0 {
			x := uint32(e.call("jr_exception")) &^ excBit
			return 0, e.takeError(x)
		}
		p, n := unpack(pl)
		bc = e.read(p, n)
		e.call("jr_free_js", uint64(p))
		storeBC(dir, k, bc)
	}
	if !mem {
		bcMu.Lock()
		delete(bcMem, k)
		bcMu.Unlock()
	}
	bp, bfree := e.tmpBytes(bc)
	defer bfree()
	return e.res(uint32(e.call("jr_eval_bytecode", uint64(bp), uint64(len(bc)))))
}

// EvalCached is Eval through the bytecode cache (Options.CacheDir): the
// script is compiled once and later runtimes, in this process or the next,
// evaluate the cached QuickJS bytecode instead of parsing the source. The
// key is the engine build plus name and source, so a changed script or
// engine compiles again. Same semantics as Eval (a classic sloppy-mode
// script and its completion value). JS thread only.
func (c *Ctx) EvalCached(name, src string) (Value, error) {
	h, err := c.r.eng.evalCached(name, src, false)
	return c.r.wrap(h), err
}

// EvalCached is Ctx.EvalCached from any goroutine; the Value is remote
// (usable from any goroutine, see Value).
func (r *Runtime) EvalCached(name, src string) (Value, error) {
	var v Value
	err := r.Do(func(js *Ctx) (err error) {
		v, err = js.EvalCached(name, src)
		v = v.asRemote()
		return err
	})
	return v, err
}
