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
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(append([]byte(nil), body...), s[:]...), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// evalBoot evaluates a trusted boot script through the bytecode cache
// (plain eval when the engine lacks bytecode support).
func (e *engine) evalBoot(name, src string) error {
	if e.fns["jr_compile"] == nil || e.fns["jr_eval_bytecode"] == nil {
		_, err := e.evalChecked(name, src)
		return err
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
			return e.takeError(x)
		}
		p, n := unpack(pl)
		bc = e.read(p, n)
		e.call("jr_free_js", uint64(p))
		storeBC(dir, k, bc)
	}
	bp, bfree := e.tmpBytes(bc)
	defer bfree()
	h, err := e.res(uint32(e.call("jr_eval_bytecode", uint64(bp), uint64(len(bc)))))
	e.release(h)
	return err
}
