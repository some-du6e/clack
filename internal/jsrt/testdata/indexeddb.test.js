// indexeddb.test.js: IndexedDB (web.js), as the browser has it. Run by
// TestJSSuites; TestIndexedDBPersists (jsrt_test.go) checks a second
// runtime reads what the first one wrote.
const req = (r) => new Promise((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); });
const done = (tx) => new Promise((resolve, reject) => { tx.oncomplete = () => resolve('complete'); tx.onabort = () => reject(tx.error || new DOMException('aborted', 'AbortError')); });
let dbN = 0;
// a fresh database: upgrade(db, tx) builds it at version 1
const fresh = (upgrade) => new Promise((resolve, reject) => {
  const r = indexedDB.open('test-' + ++dbN, 1);
  r.onupgradeneeded = (e) => upgrade(r.result, r.transaction, e);
  r.onsuccess = () => resolve(r.result);
  r.onerror = () => reject(r.error);
});

test('HMojis: the device key kept and read back, a CryptoKey that still signs', async () => {
  // the plugin's openKeyDb, idbPut and idbGet, unchanged
  const KEY_DB = 'hmojis-device', KEY_STORE = 'keys';
  function openKeyDb() {
    return new Promise((resolve, reject) => {
      const req = indexedDB.open(KEY_DB, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(KEY_STORE);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }
  async function idbGet(tag) {
    const db = await openKeyDb();
    return new Promise((resolve, reject) => {
      const req = db.transaction(KEY_STORE).objectStore(KEY_STORE).get(tag);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }
  async function idbPut(tag, key) {
    const db = await openKeyDb();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(KEY_STORE, 'readwrite');
      tx.objectStore(KEY_STORE).put(key, tag);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }
  assertEq(await idbGet('tag1'), undefined);
  const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']);
  const j = await crypto.subtle.exportKey('jwk', pair.publicKey);
  await idbPut('tag1', { priv: pair.privateKey, jwk: { kty: 'EC', crv: 'P-256', x: j.x, y: j.y }, keyId: 'k1' });
  const back = await idbGet('tag1');
  assert(back.priv instanceof CryptoKey && back.priv !== pair.privateKey);
  assertEq([back.priv.type, back.priv.extractable, back.priv.usages, back.keyId, back.jwk.x], ['private', false, ['sign'], 'k1', j.x]);
  const data = new TextEncoder().encode('HM1\nGET\n/x\n1\nn\n');
  const sig = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, back.priv, data);
  assert(await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, sig, data));
  assertEq((await indexedDB.databases()).filter((d) => d.name === KEY_DB), [{ name: KEY_DB, version: 1 }]);
});
test('open: upgradeneeded, then success; versions; VersionError', async () => {
  const log = [];
  const name = 'versions-' + ++dbN;
  const db = await new Promise((resolve, reject) => {
    const r = indexedDB.open(name, 2);
    assert(r instanceof IDBOpenDBRequest && r.readyState === 'pending');
    r.onupgradeneeded = (e) => {
      log.push(['upgradeneeded', e.oldVersion, e.newVersion, r.transaction.mode, e instanceof IDBVersionChangeEvent]);
      r.result.createObjectStore('b');
      r.result.createObjectStore('a', { keyPath: 'id' });
    };
    r.onsuccess = () => { log.push(['success', r.transaction]); resolve(r.result); };
    r.onerror = () => reject(r.error);
  });
  assertEq(log, [['upgradeneeded', 0, 2, 'versionchange', true], ['success', null]]);
  assertEq([db.name, db.version, [...db.objectStoreNames], db.objectStoreNames.contains('a'), db.objectStoreNames.item(1)], [name, 2, ['a', 'b'], true, 'b']);
  const same = await req(indexedDB.open(name));
  assertEq(same.version, 2);
  const err = await req(indexedDB.open(name, 1)).then(() => null, (e) => e);
  assertEq(err && err.name, 'VersionError');
  await assertThrows(() => indexedDB.open(name, 0), 'TypeError');
  db.close();
  same.close();
});
test('records: put and add, in-line and out-of-line keys, autoIncrement', async () => {
  const db = await fresh((db) => {
    db.createObjectStore('people', { keyPath: 'id' });
    db.createObjectStore('notes');
    db.createObjectStore('log', { keyPath: 'n', autoIncrement: true });
    db.createObjectStore('queue', { autoIncrement: true });
  });
  const tx = db.transaction(['people', 'notes', 'log', 'queue'], 'readwrite');
  assertEq([tx.mode, [...tx.objectStoreNames]], ['readwrite', ['log', 'notes', 'people', 'queue']]);
  const people = tx.objectStore('people');
  assert(people === tx.objectStore('people'), 'the same store object within a transaction');
  assertEq([people.name, people.keyPath, people.autoIncrement], ['people', 'id', false]);
  assertEq(await req(people.put({ id: 2, name: 'Bob' })), 2);
  assertEq(await req(people.add({ id: 1, name: 'Ana' })), 1);
  assertEq(await req(tx.objectStore('notes').put('hello', 'k1')), 'k1');
  assertEq(await req(tx.objectStore('log').add({ text: 'a' })), 1);
  assertEq(await req(tx.objectStore('log').add({ text: 'b', n: 10 })), 10);
  assertEq(await req(tx.objectStore('log').add({ text: 'c' })), 11);
  assertEq(await req(tx.objectStore('queue').add('x')), 1);
  assertEq(await req(tx.objectStore('queue').add('y', 5.5)), 5.5);
  assertEq(await req(tx.objectStore('queue').add('z')), 6);
  assertEq(await done(tx), 'complete');
  const r = db.transaction(['people', 'log', 'queue']);
  assertEq(await req(r.objectStore('people').getAll()), [{ id: 1, name: 'Ana' }, { id: 2, name: 'Bob' }]);
  assertEq(await req(r.objectStore('log').get(11)), { text: 'c', n: 11 });
  assertEq(await req(r.objectStore('queue').getAllKeys()), [1, 5.5, 6]);
  assertEq(await req(r.objectStore('people').count()), 2);
  assertEq(await req(r.objectStore('people').getKey(IDBKeyRange.lowerBound(2))), 2);
  assertEq(await req(r.objectStore('people').get(3)), undefined);
  // in-line and out-of-line key rules, thrown at once
  const w = db.transaction(['people', 'notes'], 'readwrite');
  await assertThrows(() => w.objectStore('people').put({ id: 3 }, 3), 'DataError');
  await assertThrows(() => w.objectStore('people').put({ name: 'no id' }), 'DataError');
  await assertThrows(() => w.objectStore('notes').put('no key'), 'DataError');
  await assertThrows(() => w.objectStore('notes').put('bad key', {}), 'DataError');
  await assertThrows(() => w.objectStore('notes').put(() => 1, 'f'), 'DataCloneError');
  await assertThrows(() => w.objectStore('nope'), 'NotFoundError');
  await done(w);
  await assertThrows(() => db.transaction('people').objectStore('people').put({ id: 9 }), 'ReadOnlyError');
  db.close();
});
test('values: structured clones come back equal, a new copy each time', async () => {
  const db = await fresh((db) => db.createObjectStore('v'));
  const cyc = { name: 'cycle' };
  cyc.self = cyc;
  const values = {
    num: -0, nan: NaN, inf: -Infinity, big: 12345678901234567890n, undef: undefined, nul: null,
    date: new Date(1700000000000), re: /a+b/gi, map: new Map([[1, 'one'], ['two', { x: 2 }]]), set: new Set(['a', 3]),
    bytes: new Uint8Array([1, 2, 3]), f64: new Float64Array([1.5]), ab: new Uint8Array([9, 8]).buffer, nested: { a: [1, [2, { b: 3 }]] },
    sparse: [1, , 3], boxed: new String('s'), err: new RangeError('nope'), blob: new Blob(['blob bytes'], { type: 'text/plain' }), cyc,
  };
  const w = db.transaction('v', 'readwrite');
  for (const [k, v] of Object.entries(values)) w.objectStore('v').put(v, k);
  await done(w);
  const s = db.transaction('v').objectStore('v');
  const get = (k) => req(s.get(k));
  assert(Object.is(await get('num'), -0));
  assert(Number.isNaN(await get('nan')));
  assertEq(await get('inf'), -Infinity);
  assertEq(await get('big'), 12345678901234567890n);
  assertEq(await get('undef'), undefined);
  assertEq(await get('nul'), null);
  const d = await get('date');
  assert(d instanceof Date && d.getTime() === 1700000000000);
  const re = await get('re');
  assertEq([re instanceof RegExp, re.source, re.flags], [true, 'a+b', 'gi']);
  const m = await get('map');
  assertEq([m instanceof Map, m.get(1), m.get('two')], [true, 'one', { x: 2 }]);
  assertEq([...(await get('set'))], ['a', 3]);
  const b = await get('bytes');
  assertEq([b instanceof Uint8Array, [...b]], [true, [1, 2, 3]]);
  assertEq([...(await get('f64'))], [1.5]);
  assertEq([...new Uint8Array(await get('ab'))], [9, 8]);
  assertEq(await get('nested'), { a: [1, [2, { b: 3 }]] });
  const sp = await get('sparse');
  assertEq([sp.length, 1 in sp, sp[2]], [3, false, 3]);
  assertEq(String(await get('boxed')), 's');
  const e = await get('err');
  assertEq([e instanceof RangeError, e.message], [true, 'nope']);
  const blob = await get('blob');
  assertEq([blob instanceof Blob, blob.type, await blob.text()], [true, 'text/plain', 'blob bytes']);
  const c = await get('cyc');
  assert(c.self === c && c.name === 'cycle');
  const a1 = await get('nested'), a2 = await get('nested');
  assert(a1 !== a2, 'a new copy each time');
  db.close();
});
test('keys: their order, key ranges, cmp', async () => {
  const keys = [[1], new Uint8Array([2]), 'b', 'a', new Date(5), 10, -1, ['a', 1], Infinity];
  assertEq(indexedDB.cmp(1, 2), -1);
  assertEq(indexedDB.cmp('a', 1), 1);
  assertEq(indexedDB.cmp([1, 2], [1]), 1);
  assertEq(indexedDB.cmp(new Date(1), 99999), 1);
  await assertThrows(() => indexedDB.cmp({}, 1), 'DataError');
  await assertThrows(() => indexedDB.cmp(NaN, 1), 'DataError');
  const db = await fresh((db) => db.createObjectStore('k'));
  const w = db.transaction('k', 'readwrite');
  keys.forEach((k, i) => w.objectStore('k').put(i, k));
  await done(w);
  const s = db.transaction('k').objectStore('k');
  const all = await req(s.getAllKeys());
  assertEq(all.map((k) => (k instanceof Date ? 'date' : k instanceof ArrayBuffer ? 'binary' : k)), [-1, 10, Infinity, 'date', 'a', 'b', 'binary', [1], ['a', 1]]);
  assertEq(await req(s.getAll(IDBKeyRange.bound(0, 'a'))), [5, 8, 4, 3]);
  assertEq(await req(s.getAll(IDBKeyRange.bound(-1, 10, true, false))), [5]);
  assertEq(await req(s.getAll(IDBKeyRange.upperBound(10, true))), [6]);
  assertEq(await req(s.getAll(null, 2)), [6, 5]);
  assertEq(await req(s.count(IDBKeyRange.lowerBound('a'))), 5);
  const r = IDBKeyRange.bound(1, 5, true);
  assertEq([r.lower, r.upper, r.lowerOpen, r.upperOpen, r.includes(1), r.includes(5), r.includes(3)], [1, 5, true, false, false, true, true]);
  await assertThrows(() => IDBKeyRange.bound(5, 1), 'DataError');
  await assertThrows(() => IDBKeyRange.only({}), 'DataError');
  db.close();
});
test('cursors: directions, continue, advance, update, delete', async () => {
  const db = await fresh((db) => {
    const s = db.createObjectStore('s', { keyPath: 'id' });
    s.createIndex('by_tag', 'tag');
  });
  const w = db.transaction('s', 'readwrite');
  for (const [id, tag] of [[1, 'x'], [2, 'y'], [3, 'x'], [4, 'z'], [5, 'y']]) w.objectStore('s').put({ id, tag });
  await done(w);
  const walk = (source, query, direction, f) => new Promise((resolve, reject) => {
    const seen = [];
    const r = source.openCursor(query, direction);
    r.onsuccess = () => {
      const c = r.result;
      if (!c) { resolve(seen); return; }
      seen.push(c.primaryKey);
      if (f) f(c, seen); else c.continue();
    };
    r.onerror = () => reject(r.error);
  });
  const t = db.transaction('s');
  const s = t.objectStore('s');
  assertEq(await walk(s), [1, 2, 3, 4, 5]);
  assertEq(await walk(s, null, 'prev'), [5, 4, 3, 2, 1]);
  assertEq(await walk(s, IDBKeyRange.bound(2, 4)), [2, 3, 4]);
  assertEq(await walk(s, null, 'next', (c) => c.advance(2)), [1, 3, 5]);
  assertEq(await walk(s, null, 'next', (c) => (c.primaryKey === 1 ? c.continue(4) : c.continue())), [1, 4, 5]);
  const ix = s.index('by_tag');
  assertEq([ix.name, ix.keyPath, ix.unique, ix.multiEntry, ix.objectStore === s], ['by_tag', 'tag', false, false, true]);
  assertEq(await walk(ix), [1, 3, 2, 5, 4]);
  assertEq(await walk(ix, null, 'nextunique'), [1, 2, 4]);
  assertEq(await walk(ix, null, 'prevunique'), [4, 2, 1]);
  assertEq(await walk(ix, 'y'), [2, 5]);
  assertEq(await req(ix.get('z')), { id: 4, tag: 'z' });
  assertEq(await req(ix.getAllKeys('x')), [1, 3]);
  assertEq(await req(ix.count('y')), 2);
  const keyCursor = await req(s.openKeyCursor());
  assertEq([keyCursor instanceof IDBCursorWithValue, keyCursor.key, 'value' in keyCursor], [false, 1, false]);
  await done(t);
  const u = db.transaction('s', 'readwrite');
  await walk(u.objectStore('s'), null, 'next', (c) => {
    if (c.value.tag === 'x') c.update({ ...c.value, tag: 'X' });
    if (c.value.tag === 'z') c.delete();
    c.continue();
  });
  await done(u);
  assertEq(await req(db.transaction('s').objectStore('s').getAll()), [{ id: 1, tag: 'X' }, { id: 2, tag: 'y' }, { id: 3, tag: 'X' }, { id: 5, tag: 'y' }]);
  db.close();
});
test('transactions: an error event aborts and rolls back, unless cancelled; abort(); inactive', async () => {
  const db = await fresh((db) => db.createObjectStore('s'));
  const w = db.transaction('s', 'readwrite');
  w.objectStore('s').put('kept', 1);
  const events = [];
  const dup = w.objectStore('s').add('dup', 1);
  dup.onerror = (e) => { events.push(['request error', dup.error.name, e.bubbles, e.cancelable]); };
  w.onerror = (e) => events.push(['transaction error', e.target === dup]);
  db.onerror = () => events.push(['database error']);
  const later = w.objectStore('s').put('never', 2);
  later.onerror = () => events.push(['later', later.error.name]);
  const aborted = await done(w).then(() => null, (e) => e);
  assertEq(aborted && aborted.name, 'ConstraintError');
  assertEq(w.error.name, 'ConstraintError');
  // the abort's error at each pending request bubbles too
  assertEq(events, [['request error', 'ConstraintError', true, true], ['transaction error', true], ['database error'], ['later', 'AbortError'], ['transaction error', false], ['database error']]);
  assertEq(await req(db.transaction('s').objectStore('s').getAll()), [], 'rolled back');
  db.onerror = null;

  const k = db.transaction('s', 'readwrite');
  k.objectStore('s').put('a', 1);
  const d2 = k.objectStore('s').add('b', 1);
  d2.onerror = (e) => e.preventDefault();
  k.objectStore('s').put('c', 3);
  assertEq(await done(k), 'complete');
  assertEq(await req(db.transaction('s').objectStore('s').getAll()), ['a', 'c']);

  const a = db.transaction('s', 'readwrite');
  a.objectStore('s').put('gone', 9);
  a.abort();
  assertEq(await done(a).then(() => 'complete', (e) => e.name), 'AbortError');
  await assertThrows(() => a.abort(), 'InvalidStateError');
  assertEq(await req(db.transaction('s').objectStore('s').get(9)), undefined);

  // a store object of a transaction whose task has ended (committed by
  // now, with nothing asked of it): TransactionInactiveError; asking the
  // finished transaction for a store is InvalidStateError
  const t = db.transaction('s', 'readwrite');
  const ts = t.objectStore('s');
  await new Promise((r) => setTimeout(r, 0));
  await assertThrows(() => ts.put('late', 4), 'TransactionInactiveError');
  await assertThrows(() => t.objectStore('s'), 'InvalidStateError');

  const thrown = db.transaction('s', 'readwrite');
  const r = thrown.objectStore('s').put('x', 7);
  r.onsuccess = () => { throw new Error('a callback that throws'); };
  assertEq(await done(thrown).then(() => 'complete', (e) => e.name), 'AbortError');
  assertEq(await req(db.transaction('s').objectStore('s').get(7)), undefined);
  db.close();
});
test('versionchange: an open connection is told, and the upgrade waits for it to close', async () => {
  const name = 'vc-' + ++dbN;
  const first = await req(indexedDB.open(name, 1));
  const log = [];
  first.onversionchange = (e) => { log.push(['versionchange', e.oldVersion, e.newVersion]); first.close(); };
  const second = await new Promise((resolve, reject) => {
    const r = indexedDB.open(name, 2);
    r.onblocked = () => log.push(['blocked']);
    r.onupgradeneeded = (e) => log.push(['upgrade', e.oldVersion, e.newVersion]);
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
  assertEq(log, [['versionchange', 1, 2], ['upgrade', 1, 2]]);
  await assertThrows(() => first.transaction([]), 'InvalidStateError');
  second.close();
  const del = await new Promise((resolve) => { const r = indexedDB.deleteDatabase(name); r.onsuccess = (e) => resolve([e.oldVersion, e.newVersion, r.result]); });
  assertEq(del, [2, null, undefined]);
  assert(!(await indexedDB.databases()).some((d) => d.name === name));
});
test('upgrade: stores and indexes, a unique index, an aborted upgrade', async () => {
  const name = 'up-' + ++dbN;
  const db = await new Promise((resolve, reject) => {
    const r = indexedDB.open(name, 1);
    r.onupgradeneeded = () => {
      const s = r.result.createObjectStore('users', { keyPath: 'id' });
      s.createIndex('email', 'email', { unique: true });
      s.createIndex('tags', 'tags', { multiEntry: true });
      assertEq([...s.indexNames], ['email', 'tags']);
      s.put({ id: 1, email: 'a@x', tags: ['a', 'b'] });
    };
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
  const t = db.transaction('users', 'readwrite');
  const dup = t.objectStore('users').put({ id: 2, email: 'a@x', tags: [] });
  assertEq(await done(t).then(() => 'complete', (e) => e.name), 'ConstraintError');
  assertEq(dup.error.name, 'ConstraintError');
  assertEq(await req(db.transaction('users').objectStore('users').index('tags').getAllKeys('b')), [1]);
  db.close();
  const err = await new Promise((resolve) => {
    const r = indexedDB.open(name, 2);
    r.onupgradeneeded = () => { r.result.createObjectStore('extra'); r.transaction.abort(); };
    r.onsuccess = () => resolve(null);
    r.onerror = () => resolve(r.error);
  });
  assertEq(err && err.name, 'AbortError');
  const again = await req(indexedDB.open(name));
  assertEq([again.version, [...again.objectStoreNames]], [1, ['users']]);
  again.close();
});

// the review's findings (2026-11-25): a kept key's bytes, the host
// functions a script reaches, close(), the quota, Web Crypto's timing
const allStored = () => (__jr.host.storage('indexedDB', 'keys') || []).map((k) => [k, __jr.host.storage('indexedDB', 'get', k)]);
const b64Of = (u8) => btoa(String.fromCharCode(...u8));
const b64urlOf = (u8) => b64Of(u8).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const hexBytes = (u8) => Array.from(u8, (b) => b.toString(16).padStart(2, '0')).join('');
// the forms bytes take in text: hex, raw, and base64 (and base64url) in each
// of the three alignments it can sit at inside a longer run
const textForms = (u8) => {
  const out = [hexBytes(u8), String.fromCharCode(...u8)];
  for (let s = 0; s < 3; s++) {
    const chunk = u8.subarray(s, s + 3 * Math.floor((u8.length - s) / 3));
    out.push(b64Of(chunk), b64urlOf(chunk));
  }
  return out;
};
const fromB64url = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0));
const openAt = (name, version, upgrade) => new Promise((resolve, reject) => {
  const r = indexedDB.open(name, version);
  r.onupgradeneeded = (e) => upgrade && upgrade(r.result, e);
  r.onsuccess = () => resolve(r.result);
  r.onerror = () => reject(r.error);
});

test('a CryptoKey is kept sealed: what storage returns holds none of its bytes', async () => {
  const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']);
  const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const priv = await crypto.subtle.importKey('jwk', jwk, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign']);
  const aesRaw = crypto.getRandomValues(new Uint8Array(32));
  const aes = await crypto.subtle.importKey('raw', aesRaw, 'AES-GCM', false, ['encrypt']);
  const db = await fresh((db) => db.createObjectStore('keys'));
  const tx = db.transaction('keys', 'readwrite');
  tx.objectStore('keys').put({ priv, aes }, 'device');
  await done(tx);
  const text = JSON.stringify(allStored());
  assert(text.includes('device'), 'the record is stored');
  for (const [what, bytes] of [['d', fromB64url(jwk.d)], ['the AES key', aesRaw]]) {
    for (const form of textForms(bytes)) assert(!text.includes(form), what + ' shows in the stored records');
  }
  // read back: it still signs, and still won't export
  const back = await req(db.transaction('keys').objectStore('keys').get('device'));
  const data = new TextEncoder().encode('x');
  assert(await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, back.priv, data), data));
  await assertThrows(() => crypto.subtle.exportKey('jwk', back.priv), 'InvalidAccessError');
  await assertThrows(() => crypto.subtle.exportKey('raw', back.aes), 'InvalidAccessError');
  db.close();
});
test('the host functions a script reaches: no key store or commit, no IndexedDB writes, and one replaced sees nothing', async () => {
  const H = __jr.host;
  assertEq([typeof H.webcrypto, typeof H.idbCommit], ['undefined', 'undefined']);
  await assertThrows(() => H.storage('indexedDB', 'set', 'm["forged"]', '{}'), 'NotAllowedError');
  await assertThrows(() => H.storage('indexedDB', 'del', 'm["forged"]'), 'NotAllowedError');
  await assertThrows(() => H.storage('indexedDB', 'clear'), 'NotAllowedError');
  const saved = { storage: H.storage, random: H.random, digest: H.digest, storageEstimate: H.storageEstimate };
  const seen = [];
  for (const k of Object.keys(saved)) H[k] = (...a) => { seen.push(k); return saved[k](...a); };
  try {
    const db = await fresh((db) => db.createObjectStore('s'));
    const tx = db.transaction('s', 'readwrite');
    tx.objectStore('s').put('v', 'k');
    await done(tx);
    assertEq(await req(db.transaction('s').objectStore('s').get('k')), 'v');
    crypto.getRandomValues(new Uint8Array(16));
    crypto.randomUUID();
    await crypto.subtle.digest('SHA-256', new Uint8Array(1));
    await crypto.subtle.generateKey({ name: 'AES-GCM', length: 128 }, false, ['encrypt']);
    await navigator.storage.estimate();
    db.close();
  } finally {
    Object.assign(H, saved);
  }
  assertEq(seen, []);
});
test('close() waits for its transactions: a deleteDatabase or an upgrade runs after them', async () => {
  const log = [];
  let name = 'closing-' + ++dbN;
  let db = await openAt(name, 1, (db) => db.createObjectStore('s'));
  let tx = db.transaction('s', 'readwrite');
  tx.objectStore('s').put('written before the delete', 'k');
  tx.oncomplete = () => log.push('complete');
  db.close();
  await assertThrows(() => db.transaction('s'), 'InvalidStateError');
  await new Promise((resolve, reject) => {
    const del = indexedDB.deleteDatabase(name);
    del.onsuccess = () => { log.push('deleted'); resolve(); };
    del.onerror = () => reject(del.error);
  });
  assertEq(log, ['complete', 'deleted']);
  // deleted it stays: nothing stored under its name, and it opens new
  assertEq(allStored().filter(([k]) => k.includes(JSON.stringify(name))), []);
  assert(!(await indexedDB.databases()).some((d) => d.name === name), 'databases() lists it');
  db = await openAt(name, 1, (db, e) => log.push('new ' + e.oldVersion + ' ' + db.objectStoreNames.length));
  assertEq(log.at(-1), 'new 0 0');
  db.close();

  log.length = 0;
  name = 'closing-' + ++dbN;
  db = await openAt(name, 1, (db) => db.createObjectStore('s'));
  tx = db.transaction('s', 'readwrite');
  tx.objectStore('s').put('written before the upgrade', 'k');
  tx.oncomplete = () => log.push('complete');
  db.onversionchange = () => log.push('versionchange'); // not to a closing connection
  db.close();
  const up = await openAt(name, 2, (db, e) => { log.push('upgrade from ' + e.oldVersion); db.createObjectStore('t'); });
  assertEq(log, ['complete', 'upgrade from 1']);
  assertEq([up.version, [...up.objectStoreNames]], [2, ['s', 't']]);
  assertEq(await req(up.transaction('s').objectStore('s').get('k')), 'written before the upgrade');
  up.close();
});
test('stores renamed and made anew write what they hold, under their own names', async () => {
  const name = 'names-' + ++dbN;
  let db = await openAt(name, 1, (db) => { db.createObjectStore('a').put('in a', 1); db.createObjectStore('b').put('in b', 1); });
  db.close();
  // the two swap names; a third is dropped and made anew, empty
  db = await openAt(name, 2, (db, e) => {
    const t = e.target.transaction;
    t.objectStore('a').name = 'tmp';
    t.objectStore('b').name = 'a';
    t.objectStore('tmp').name = 'b';
    db.createObjectStore('c').put('old c', 1);
  });
  db.close();
  db = await openAt(name, 3, (db) => { db.deleteObjectStore('c'); db.createObjectStore('c'); });
  const get = (s) => req(db.transaction(s).objectStore(s).get(1));
  assertEq([await get('a'), await get('b'), await get('c')], ['in b', 'in a', undefined]);
  const stored = allStored().filter(([k]) => k.startsWith('r' + JSON.stringify([name]).slice(0, -1)));
  assertEq(stored.map(([k, v]) => [k.slice(k.indexOf(',') + 1), v]).sort(), [['"a"]["n",1]', '"in b"'], ['"b"]["n",1]', '"in a"']]);
  db.close();
});
test('navigator.storage.estimate(): what IndexedDB stores, and its quota', async () => {
  const before = await navigator.storage.estimate();
  assertEq(before.quota, 64 * 1024 * 1024);
  const db = await fresh((db) => db.createObjectStore('s'));
  const tx = db.transaction('s', 'readwrite');
  tx.objectStore('s').put('x'.repeat(10000), 'big');
  await done(tx);
  const after = await navigator.storage.estimate();
  assert(after.usage - before.usage >= 10000 && after.usage - before.usage < 10400, before.usage + ' then ' + after.usage);
  db.close();
});
test("Web Crypto settles in a later task, as Chromium's: a transaction is inactive after the await", async () => {
  const db = await fresh((db) => db.createObjectStore('s'));
  const store = db.transaction('s', 'readwrite').objectStore('s');
  await crypto.subtle.generateKey({ name: 'AES-GCM', length: 128 }, false, ['encrypt']);
  await assertThrows(() => store.put('late', 'k'), 'TransactionInactiveError');
  const order = [];
  const p = crypto.subtle.digest('SHA-256', new Uint8Array(1)).then(() => order.push('digest'));
  Promise.resolve().then(() => order.push('microtask'));
  await p;
  assertEq(order, ['microtask', 'digest']);
  db.close();
});

