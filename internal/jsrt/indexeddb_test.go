package jsrt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// idbCall runs body, an async function's body with open() for the
// 'hmojis-device' database (store 'keys'), in r and returns its result.
func idbCall(t *testing.T, r *Runtime, body string) json.RawMessage {
	t.Helper()
	name := fmt.Sprintf("__idb%d", time.Now().UnixNano())
	src := `globalThis.` + name + ` = async () => {
		const open = (db = 'hmojis-device', store = 'keys') => new Promise((resolve, reject) => {
			const r = indexedDB.open(db, 1)
			r.onupgradeneeded = () => r.result.createObjectStore(store)
			r.onsuccess = () => resolve(r.result)
			r.onerror = () => reject(r.error)
		})
		const put = (db, store, v, k) => new Promise((resolve) => {
			const tx = db.transaction(store, 'readwrite')
			tx.objectStore(store).put(v, k)
			tx.oncomplete = () => resolve('complete')
			tx.onabort = () => resolve(tx.error ? tx.error.name : 'abort')
		})
		const get = (db, store, k) => new Promise((resolve) => {
			const q = db.transaction(store).objectStore(store).get(k)
			q.onsuccess = () => resolve(q.result === undefined ? null : q.result)
			q.onerror = () => resolve({ error: q.error.name })
		})
		` + body + `
	}`
	if err := r.Do(func(js *Ctx) error { _, err := js.Eval(name+".js", src); return err }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := r.Call(ctx, name)
	if err != nil {
		t.Fatalf("js: %v", err)
	}
	return out
}

// textForms are the ways b can show in stored text: hex, and base64 (and
// base64url) in each of the three alignments it can sit at inside a longer
// encoded run.
func textForms(b []byte) []string {
	out := []string{hex.EncodeToString(b), string(b)}
	for s := 0; s < 3; s++ {
		chunk := b[s : s+3*((len(b)-s)/3)]
		out = append(out, base64.StdEncoding.EncodeToString(chunk), base64.RawURLEncoding.EncodeToString(chunk))
	}
	return out
}

// IndexedDB keeps its databases in the runtime's "indexedDB" storage
// (Options.Storage), a record a key: a second runtime over the same
// storage, with the same KeyWrap secret, reads what the first one wrote, a
// non-extractable CryptoKey included, which still signs; none of it shows
// in localStorage; the stored text holds none of the key's bytes (sealed
// with KeyWrap's secret, as no script can read); and a runtime with
// another secret, or none, can't open it.
func TestIndexedDBPersists(t *testing.T) {
	kvs := map[string]KV{}
	storage := func(ns string) KV {
		if kvs[ns] == nil {
			kvs[ns] = NewMemKV()
		}
		return kvs[ns]
	}
	wrap := bytes.Repeat([]byte{7}, 32)
	run := func(secret []byte, body string) json.RawMessage {
		t.Helper()
		opts := []func(*Options){func(o *Options) { o.Storage = storage }}
		if secret != nil {
			opts = append(opts, withKeyWrap(secret))
		}
		r := newTest(t, opts...)
		defer r.Close()
		return idbCall(t, r, body)
	}
	raw := run(wrap, `
		const db = await open()
		const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify'])
		const jwk = await crypto.subtle.exportKey('jwk', pair.publicKey)
		const d = (await crypto.subtle.exportKey('jwk', pair.privateKey)).d
		// the device key as HMojis keeps it: not extractable
		const priv = await crypto.subtle.importKey('jwk', await crypto.subtle.exportKey('jwk', pair.privateKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign'])
		const done = await put(db, 'keys', { priv, jwk, note: 'lone \ud800 surrogate' }, 'tag')
		return { done, d }`)
	var first struct {
		Done string `json:"done"`
		D    string `json:"d"`
	}
	if err := json.Unmarshal(raw, &first); err != nil || first.Done != "complete" {
		t.Fatalf("the first runtime stored %s (%v)", raw, err)
	}
	kv := kvs["indexedDB"]
	if kv == nil {
		t.Fatal("nothing went to the indexedDB storage namespace")
	}
	keys := kv.Keys()
	slices.Sort(keys)
	if want := []string{`m["hmojis-device"]`, `r["hmojis-device","keys"]["s","tag"]`}; !slices.Equal(keys, want) {
		t.Errorf("the indexedDB namespace holds %q; want the database's description and its one record, %q", keys, want)
	}
	d, err := base64.RawURLEncoding.DecodeString(first.D)
	if err != nil || len(d) != 32 {
		t.Fatalf("d: %v (%d bytes)", err, len(d))
	}
	for _, k := range keys {
		v, _ := kv.Get(k)
		for _, form := range textForms(d) {
			if strings.Contains(v, form) {
				t.Errorf("the stored %s holds the private key's d (as %q)", k, form)
			}
		}
	}

	var got struct {
		Verified    bool     `json:"verified"`
		Extractable bool     `json:"extractable"`
		Usages      []string `json:"usages"`
		Note        string   `json:"note"`
		LS          int      `json:"ls"`
	}
	raw = run(wrap, `
		const db = await open()
		const v = await get(db, 'keys', 'tag')
		if (!v || v.error) return { verified: false, got: v }
		const pub = await crypto.subtle.importKey('jwk', v.jwk, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
		const data = new TextEncoder().encode('signed in the second runtime')
		const sig = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, v.priv, data)
		return { verified: await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pub, sig, data), extractable: v.priv.extractable, usages: v.priv.usages, note: v.note === 'lone \ud800 surrogate' ? 'same' : JSON.stringify(v.note), ls: localStorage.length }`)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if !got.Verified || got.Extractable || strings.Join(got.Usages, ",") != "sign" {
		t.Errorf("the second runtime read the device key back as %s; want a non-extractable signing key whose signature verifies", raw)
	}
	if got.Note != "same" {
		t.Errorf("a string with a lone surrogate came back as %s", got.Note)
	}
	if got.LS != 0 {
		t.Errorf("localStorage holds %d items: IndexedDB's records show there", got.LS)
	}

	// another secret, or none: the record's key can't be opened
	for _, secret := range [][]byte{bytes.Repeat([]byte{8}, 32), nil} {
		raw = run(secret, `
			const db = await open()
			return await get(db, 'keys', 'tag')`)
		if !strings.Contains(string(raw), `"error":"DataError"`) {
			t.Errorf("with another KeyWrap secret (%x…) the record read %s; want a DataError", secret[:min(len(secret), 2)], raw)
		}
	}
}

// IndexedDB's quota (Options.IndexedDBQuota), as an origin's in Chromium:
// a commit that would take the stored bytes past it fails whole with
// QuotaExceededError, the transaction aborts and writes nothing, and the
// database goes on (the next transaction commits); estimate() reports
// usage and quota.
func TestIndexedDBQuota(t *testing.T) {
	kv := NewMemKV()
	r := newTest(t, withIDBQuota(4096), func(o *Options) {
		o.Storage = func(ns string) KV {
			if ns == "indexedDB" {
				return kv
			}
			return nil
		}
	})
	raw := idbCall(t, r, `
		const db = await open('quota', 's')
		const big = await put(db, 's', 'x'.repeat(5000), 'big')
		const small = await put(db, 's', 'fits', 'small')
		const e = await navigator.storage.estimate()
		return { big, small, bigBack: await get(db, 's', 'big'), smallBack: await get(db, 's', 'small'), usage: e.usage, quota: e.quota }`)
	var got struct {
		Big, Small, BigBack, SmallBack any
		Usage, Quota                   int
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if got.Big != "QuotaExceededError" || got.BigBack != nil {
		t.Errorf("a 5 KB record past a 4 KB quota: %v, read back %v; want QuotaExceededError and nothing written", got.Big, got.BigBack)
	}
	if got.Small != "complete" || got.SmallBack != "fits" {
		t.Errorf("after the failed commit, a small one: %v, read back %v; want it complete and read back", got.Small, got.SmallBack)
	}
	if got.Quota != 4096 || got.Usage <= 0 || got.Usage > 4096 {
		t.Errorf("estimate(): usage %d, quota %d; want the stored bytes (0 < usage ≤ 4096) and 4096", got.Usage, got.Quota)
	}
	for _, k := range kv.Keys() {
		if strings.Contains(k, "big") {
			t.Errorf("the failed commit stored %s", k)
		}
	}
}

// countKV counts what a commit writes.
type countKV struct {
	KV
	mu      sync.Mutex
	sets    int
	written int // bytes of the keys and values set
	dels    int
}

func (c *countKV) Set(k, v string) {
	c.mu.Lock()
	c.sets++
	c.written += len(k) + len(v)
	c.mu.Unlock()
	c.KV.Set(k, v)
}

func (c *countKV) Delete(k string) {
	c.mu.Lock()
	c.dels++
	c.mu.Unlock()
	c.KV.Delete(k)
}

func (c *countKV) take() (sets, written, dels int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sets, written, dels = c.sets, c.written, c.dels
	c.sets, c.written, c.dels = 0, 0, 0
	return
}

// A commit writes the records its transaction changed, not its stores
// whole (as LevelDB writes Chromium's): one record put in a store of 50 is
// one record's write, one deleted one delete.
func TestIndexedDBCommitsWhatChanged(t *testing.T) {
	kv := &countKV{KV: NewMemKV()}
	r := newTest(t, func(o *Options) {
		o.Storage = func(ns string) KV {
			if ns == "indexedDB" {
				return kv
			}
			return nil
		}
	})
	idbCall(t, r, `
		const db = await open('many', 's')
		const tx = db.transaction('s', 'readwrite')
		for (let i = 0; i < 50; i++) tx.objectStore('s').put('record ' + i + ' ' + 'x'.repeat(200), i)
		await new Promise((resolve) => { tx.oncomplete = resolve })
		globalThis.__manyDB = db
		return true`)
	if sets, _, _ := kv.take(); sets < 50 {
		t.Fatalf("50 records put: %d keys set", sets)
	}
	idbCall(t, r, `return await put(__manyDB, 's', 'changed ' + 'y'.repeat(200), 7)`)
	if sets, written, dels := kv.take(); sets != 1 || written > 400 || dels != 0 {
		t.Errorf("one record put in a store of 50: %d keys set (%d bytes), %d deleted; want the one record's (~230 bytes)", sets, written, dels)
	}
	idbCall(t, r, `
		const tx = __manyDB.transaction('s', 'readwrite')
		tx.objectStore('s').delete(8)
		await new Promise((resolve) => { tx.oncomplete = resolve })
		return true`)
	if sets, _, dels := kv.take(); sets != 0 || dels != 1 {
		t.Errorf("one record deleted: %d keys set, %d deleted; want one delete", sets, dels)
	}
}
