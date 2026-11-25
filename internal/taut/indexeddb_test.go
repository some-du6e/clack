package taut

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hmojisKeyDB is HMojis' (izie's Taut plugin) device-key storage, its
// openKeyDb, idbGet and idbPut as the plugin has them.
const hmojisKeyDB = `
	const KEY_DB = 'hmojis-device', KEY_STORE = 'keys'
	function openKeyDb() {
		return new Promise((resolve, reject) => {
			const req = indexedDB.open(KEY_DB, 1)
			req.onupgradeneeded = () => req.result.createObjectStore(KEY_STORE)
			req.onsuccess = () => resolve(req.result)
			req.onerror = () => reject(req.error)
		})
	}
	async function idbGet(tag) {
		const db = await openKeyDb()
		return new Promise((resolve, reject) => {
			const req = db.transaction(KEY_STORE).objectStore(KEY_STORE).get(tag)
			req.onsuccess = () => resolve(req.result)
			req.onerror = () => reject(req.error)
		})
	}
	async function idbPut(tag, key) {
		const db = await openKeyDb()
		return new Promise((resolve, reject) => {
			const tx = db.transaction(KEY_STORE, 'readwrite')
			tx.objectStore(KEY_STORE).put(key, tag)
			tx.oncomplete = () => resolve()
			tx.onerror = () => reject(tx.error)
		})
	}`

// HMojis in Clack: it decrypts hidden emoji with its AES-GCM group key,
// and signs its requests with a device key, a non-extractable ECDSA
// CryptoKey it keeps in IndexedDB. Web Crypto had only digest and there
// was no indexedDB, so it could do neither. Here the group key opens what
// it sealed, and the device key it kept signs after Clack restarts (a new
// plugin system over the same folder), its records in their own file, not
// in localStorage. The file holds no key's bytes: a kept key is sealed
// with a secret of secrets.json's (jsrt's KeyWrap), which no plugin reads,
// and a plugin can't write IndexedDB's records through taut_kv.
func TestHMojisCryptoKeptAcrossRestart(t *testing.T) {
	h := newHarness(t)
	var first struct {
		Opened  string `json:"opened"`
		KeyID   string `json:"keyId"`
		Before  bool   `json:"before"`
		D       string `json:"d"`
		KVWrite string `json:"kvWrite"`
	}
	h.jsInto(hmojisKeyDB+`
		// the group key: importGroupKey, encryptHmojiId, decryptPayload
		const groupKey = await crypto.subtle.importKey('raw', crypto.getRandomValues(new Uint8Array(32)), 'AES-GCM', false, ['encrypt', 'decrypt'])
		const iv = crypto.getRandomValues(new Uint8Array(12))
		const sealed = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, groupKey, new TextEncoder().encode(JSON.stringify({ id: 'hm_secret' })))
		const opened = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, groupKey, sealed))).id
		// the device key: generateDeviceKey, then kept under its tag
		const before = (await idbGet('tag1')) !== undefined
		const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify'])
		const j = await crypto.subtle.exportKey('jwk', pair.publicKey)
		const jwk = { kty: 'EC', crv: 'P-256', x: j.x, y: j.y }
		const keyId = btoa(String.fromCharCode(...new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(jwk.x + '.' + jwk.y))))).slice(0, 22)
		await idbPut('tag1', { priv: pair.privateKey, jwk, keyId })
		// a key whose bytes the test knows, kept the same way
		const probe = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify'])
		const probeJwk = await crypto.subtle.exportKey('jwk', probe.privateKey)
		await idbPut('probe', await crypto.subtle.importKey('jwk', probeJwk, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign']))
		let kvWrite = 'written'
		try { __clack.taut_kv('write', 'indexedDB', 'm["forged"]', '{}') } catch (e) { kvWrite = 'refused' }
		return { opened, keyId, before, d: probeJwk.d, kvWrite }`, &first)
	if first.Opened != "hm_secret" || first.Before || first.KeyID == "" {
		t.Fatalf("the first start: %+v", first)
	}
	if first.KVWrite != "refused" {
		t.Errorf("taut_kv wrote into IndexedDB's namespace (%s); want it refused", first.KVWrite)
	}
	if err := h.tt.Close(); err != nil {
		t.Fatal(err)
	}

	// Clack again: a plugin system of its own over the same folder
	tt2, err := New(Options{App: h.app, Dir: h.dir, CredsPath: h.dir + "/creds.json", UI: h.ui.hooks(), NoAutoStart: true, CacheDir: t.TempDir(), Fetch: h.fetch})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt2.Close() })
	r2, err := tt2.ensureRuntime()
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("__hmojis%d", time.Now().UnixNano())
	if _, err := r2.rt.Eval(name+".js", "globalThis."+name+" = async () => {"+hmojisKeyDB+`
		const v = await idbGet('tag1')
		if (!v) return { found: false, ls: localStorage.length }
		// signRequest: HM1, method, path, ts, nonce, body hash, signed with the kept key
		const signed = new TextEncoder().encode(['HM1', 'GET', '/slack/hmojis/api/whoami', Date.now(), 'nonce', ''].join('\n'))
		const sig = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, v.priv, signed)
		const pub = await crypto.subtle.importKey('jwk', v.jwk, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
		return { found: true, keyId: v.keyId, extractable: v.priv.extractable, verified: await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pub, sig, signed), ls: localStorage.length }
	}`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := r2.rt.Call(ctx, name)
	if err != nil {
		t.Fatalf("js: %v", err)
	}
	var second struct {
		Found       bool   `json:"found"`
		KeyID       string `json:"keyId"`
		Extractable bool   `json:"extractable"`
		Verified    bool   `json:"verified"`
		LS          int    `json:"ls"`
	}
	if err := json.Unmarshal(raw, &second); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if !second.Found || second.KeyID != first.KeyID || second.Extractable || !second.Verified {
		t.Errorf("after the restart the device key is %s; want the one kept (%s), still non-extractable, signing so its public key verifies", raw, first.KeyID)
	}
	if second.LS != 0 {
		t.Errorf("localStorage holds %d items: IndexedDB's records show there", second.LS)
	}
	b, err := os.ReadFile(filepath.Join(h.dir, "storage", "indexedDB.json"))
	if err != nil || !strings.Contains(string(b), "hmojis-device") {
		t.Errorf("storage/indexedDB.json: %v, %d bytes", err, len(b))
	}
	if strings.Contains(string(b), "forged") {
		t.Errorf("storage/indexedDB.json holds the record taut_kv was asked to write")
	}
	d, err := base64.RawURLEncoding.DecodeString(first.D)
	if err != nil || len(d) != 32 {
		t.Fatalf("d: %v (%d bytes)", err, len(d))
	}
	for _, form := range keyTextForms(d) {
		if strings.Contains(string(b), form) {
			t.Errorf("storage/indexedDB.json holds a kept key's d (as %q)", form)
		}
	}
	var secrets map[string]string
	if sb, err := os.ReadFile(filepath.Join(h.dir, "secrets.json")); err != nil || json.Unmarshal(sb, &secrets) != nil || len(secrets["jsrt:keywrap"]) == 0 {
		t.Errorf("secrets.json has no jsrt:keywrap: %v", err)
	} else if strings.Contains(string(b), secrets["jsrt:keywrap"]) {
		t.Errorf("storage/indexedDB.json holds the KeyWrap secret")
	}
	if ls, err := os.ReadFile(filepath.Join(h.dir, "storage", "localStorage.json")); err == nil && strings.Contains(string(ls), "hmojis") {
		t.Errorf("localStorage.json holds the IndexedDB records: %s", ls)
	}
}

// keyTextForms are the ways b can show in stored text: hex, and base64 (and
// base64url) in each of the three alignments it can sit at inside a longer
// encoded run.
func keyTextForms(b []byte) []string {
	out := []string{hex.EncodeToString(b), string(b)}
	for s := 0; s < 3; s++ {
		chunk := b[s : s+3*((len(b)-s)/3)]
		out = append(out, base64.StdEncoding.EncodeToString(chunk), base64.RawURLEncoding.EncodeToString(chunk))
	}
	return out
}
