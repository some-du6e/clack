package jsrt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// IndexedDB keeps its databases in the runtime's "indexedDB" storage
// (Options.Storage): a second runtime over the same storage reads what the
// first one wrote, a non-extractable CryptoKey included, which still
// signs; and none of it shows in localStorage.
func TestIndexedDBPersists(t *testing.T) {
	kvs := map[string]KV{}
	storage := func(ns string) KV {
		if kvs[ns] == nil {
			kvs[ns] = NewMemKV()
		}
		return kvs[ns]
	}
	run := func(body string) json.RawMessage {
		t.Helper()
		r := newTest(t, func(o *Options) { o.Storage = storage })
		name := fmt.Sprintf("__idb%d", time.Now().UnixNano())
		src := `globalThis.` + name + ` = async () => {
			const open = () => new Promise((resolve, reject) => {
				const r = indexedDB.open('hmojis-device', 1)
				r.onupgradeneeded = () => r.result.createObjectStore('keys')
				r.onsuccess = () => resolve(r.result)
				r.onerror = () => reject(r.error)
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
		r.Close()
		return out
	}
	run(`
		const db = await open()
		const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify'])
		const jwk = await crypto.subtle.exportKey('jwk', pair.publicKey)
		await new Promise((resolve, reject) => {
			const tx = db.transaction('keys', 'readwrite')
			tx.objectStore('keys').put({ priv: pair.privateKey, jwk }, 'tag')
			tx.oncomplete = resolve
			tx.onerror = () => reject(tx.error)
		})
		return true`)
	var got struct {
		Verified    bool     `json:"verified"`
		Extractable bool     `json:"extractable"`
		Usages      []string `json:"usages"`
		LS          int      `json:"ls"`
	}
	raw := run(`
		const db = await open()
		const v = await new Promise((resolve, reject) => {
			const r = db.transaction('keys').objectStore('keys').get('tag')
			r.onsuccess = () => resolve(r.result)
			r.onerror = () => reject(r.error)
		})
		if (!v) return { verified: false }
		const pub = await crypto.subtle.importKey('jwk', v.jwk, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
		const data = new TextEncoder().encode('signed in the second runtime')
		const sig = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, v.priv, data)
		return { verified: await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pub, sig, data), extractable: v.priv.extractable, usages: v.priv.usages, ls: localStorage.length }`)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if !got.Verified || got.Extractable || strings.Join(got.Usages, ",") != "sign" {
		t.Errorf("the second runtime read the device key back as %s; want a non-extractable signing key whose signature verifies", raw)
	}
	if got.LS != 0 {
		t.Errorf("localStorage holds %d items: IndexedDB's records show there", got.LS)
	}
	kv := kvs["indexedDB"]
	if kv == nil {
		t.Fatal("nothing went to the indexedDB storage namespace")
	}
	var keys []string
	for _, k := range kv.Keys() {
		keys = append(keys, strings.ReplaceAll(k, "\x00", "/"))
	}
	if strings.Join(keys, " ") != "db/hmojis-device store/hmojis-device/keys" {
		t.Errorf("the indexedDB namespace holds %q", keys)
	}
}
