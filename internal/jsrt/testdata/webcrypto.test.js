// webcrypto.test.js: Web Crypto's AES-GCM and ECDSA (web.js, webcrypto.go),
// as Chromium has them. Run by TestJSSuites. The vectors are NIST's (GCM
// test case 2) and OpenSSL's (node 24's crypto, 2026-11-24).
const hexOf = (buf) => Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, '0')).join('');
const unhex = (s) => new Uint8Array(s.match(/../g).map((x) => parseInt(x, 16)));
const enc = (s) => new TextEncoder().encode(s);
const subtle = crypto.subtle;

test('AES-GCM: NIST test case 2, and the ciphertext ends in its tag', async () => {
  const key = await subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', true, ['decrypt', 'encrypt']);
  assertEq(Object.prototype.toString.call(key), '[object CryptoKey]');
  assertEq([key.type, key.extractable, key.algorithm, key.usages], ['secret', true, { name: 'AES-GCM', length: 128 }, ['encrypt', 'decrypt']]);
  assert(key.algorithm === key.algorithm && key.usages === key.usages, 'the same objects on each read');
  const out = await subtle.encrypt({ name: 'AES-GCM', iv: new Uint8Array(12) }, key, new Uint8Array(16));
  assert(out instanceof ArrayBuffer);
  assertEq(hexOf(out), '0388dace60b6a392f328c2b971b2fe78' + 'ab6e47d42cec13bdf53a67b21257bddf');
  assertEq(hexOf(await subtle.decrypt({ name: 'aes-gcm', iv: new Uint8Array(12) }, key, out)), '00000000000000000000000000000000');
});
test('AES-GCM: additional data, other iv and tag lengths (OpenSSL vectors)', async () => {
  const key = await subtle.importKey('raw', unhex('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f'), { name: 'AES-GCM' }, false, ['encrypt', 'decrypt']);
  assertEq(key.algorithm.length, 256);
  const p = { name: 'AES-GCM', iv: unhex('cafebabefacedbaddecaf888'), additionalData: enc('aad') };
  const ct = await subtle.encrypt(p, key, enc('{"id":"hm_1234"}'));
  assertEq(hexOf(ct), 'f181c94288406d732b546cef4829ab4252ddde28f3935b02f04028e01fb62331');
  assertEq(new TextDecoder().decode(await subtle.decrypt(p, key, ct)), '{"id":"hm_1234"}');
  await assertThrows(() => subtle.decrypt({ ...p, additionalData: enc('other') }, key, ct), 'OperationError');
  const bad = new Uint8Array(ct);
  bad[bad.length - 1] ^= 1;
  await assertThrows(() => subtle.decrypt(p, key, bad), 'OperationError');
  const p64 = { name: 'AES-GCM', iv: new Uint8Array(16).fill(7), tagLength: 64 };
  const ct64 = await subtle.encrypt(p64, key, enc('sixteen byte msg'));
  assertEq(hexOf(ct64), 'bb01a3829ddbd3c34ccf9cb66c858c1e499fe0c13d7c118f');
  assertEq(new TextDecoder().decode(await subtle.decrypt(p64, key, ct64)), 'sixteen byte msg');
  const short = new Uint8Array(ct64);
  short[17] ^= 0x80;
  await assertThrows(() => subtle.decrypt(p64, key, short), 'OperationError');
  for (const tagLength of [32, 96, 104, 112, 120]) {
    const q = { name: 'AES-GCM', iv: new Uint8Array(12).fill(1), tagLength };
    const c = await subtle.encrypt(q, key, enc('x'));
    assertEq(c.byteLength, 1 + tagLength / 8, `tagLength ${tagLength}`);
    assertEq(new TextDecoder().decode(await subtle.decrypt(q, key, c)), 'x');
  }
  await assertThrows(() => subtle.encrypt({ name: 'AES-GCM', iv: new Uint8Array(12), tagLength: 100 }, key, enc('x')), 'OperationError');
  await assertThrows(() => subtle.decrypt({ name: 'AES-GCM', iv: new Uint8Array(12) }, key, new Uint8Array(4)), 'OperationError');
});
test('AES-GCM: HMojis group key and payload', async () => {
  // the plugin's importGroupKey, encryptHmojiId and decryptPayload
  const raw = crypto.getRandomValues(new Uint8Array(32));
  const key = await subtle.importKey('raw', Uint8Array.from(atob(btoa(String.fromCharCode(...raw))), (c) => c.charCodeAt(0)), 'AES-GCM', false, ['encrypt', 'decrypt']);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await subtle.encrypt({ name: 'AES-GCM', iv }, key, new TextEncoder().encode(JSON.stringify({ id: 'hm_42' })));
  const combined = new Uint8Array(iv.length + ciphertext.byteLength);
  combined.set(iv, 0);
  combined.set(new Uint8Array(ciphertext), iv.length);
  const plain = await subtle.decrypt({ name: 'AES-GCM', iv: combined.slice(0, 12) }, key, combined.slice(12));
  assertEq(JSON.parse(new TextDecoder().decode(plain)), { id: 'hm_42' });
  await assertThrows(() => subtle.exportKey('raw', key), 'InvalidAccessError');
});
test('AES-GCM: import, export, generate and their errors', async () => {
  const jwk = { kty: 'oct', k: 'AAAAAAAAAAAAAAAAAAAAAA', alg: 'A128GCM', ext: true, key_ops: ['encrypt', 'decrypt'] };
  const key = await subtle.importKey('jwk', jwk, 'AES-GCM', true, ['encrypt']);
  assertEq(key.usages, ['encrypt']);
  assertEq(await subtle.exportKey('jwk', key), { alg: 'A128GCM', ext: true, k: 'AAAAAAAAAAAAAAAAAAAAAA', key_ops: ['encrypt'], kty: 'oct' });
  assertEq(hexOf(await subtle.exportKey('raw', key)), '00000000000000000000000000000000');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, alg: 'A256GCM' }, 'AES-GCM', true, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, ext: false }, 'AES-GCM', true, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, key_ops: ['decrypt'] }, 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, kty: 'EC' }, 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, k: 'AA==' }, 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', new Uint8Array(16), 'AES-GCM', false, ['encrypt']), 'TypeError');
  // Chromium leaves out 192-bit AES on purpose (crbug.com/533699): OperationError, not DataError
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(24), 'AES-GCM', false, ['encrypt']), 'OperationError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(20), 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['sign']), 'SyntaxError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, []), 'SyntaxError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['bogus']), 'TypeError');
  await assertThrows(() => subtle.importKey('spki', new Uint8Array(16), 'AES-GCM', false, ['encrypt']), 'NotSupportedError');
  await assertThrows(() => subtle.importKey('bogus', new Uint8Array(16), 'AES-GCM', false, ['encrypt']), 'TypeError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 'AES-CBC', false, ['encrypt']), 'NotSupportedError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false), 'TypeError');
  await assertThrows(() => subtle.encrypt({ name: 'AES-GCM' }, key, enc('x')), 'TypeError');
  await assertThrows(() => subtle.encrypt({ name: 'AES-GCM', iv: new Uint8Array(12) }, {}, enc('x')), 'TypeError');
  await assertThrows(() => subtle.encrypt({ name: 'AES-GCM', iv: new Uint8Array(12) }, key, 'x'), 'TypeError');
  await assertThrows(() => subtle.decrypt({ name: 'AES-GCM', iv: new Uint8Array(12) }, key, new Uint8Array(32)), 'InvalidAccessError');
  const gen = await subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt', 'wrapKey']);
  assertEq([gen.type, gen.extractable, gen.algorithm, gen.usages], ['secret', false, { name: 'AES-GCM', length: 256 }, ['encrypt', 'decrypt', 'wrapKey']]);
  await assertThrows(() => subtle.generateKey({ name: 'AES-GCM', length: 192 }, false, ['encrypt']), 'OperationError');
  await assertThrows(() => subtle.generateKey({ name: 'AES-GCM', length: 100 }, false, ['encrypt']), 'OperationError');
  await assertThrows(() => subtle.generateKey({ name: 'AES-GCM' }, false, ['encrypt']), 'TypeError');
  await assertThrows(() => subtle.generateKey({ name: 'AES-GCM', length: 128 }, false, []), 'SyntaxError');
  let threw = false;
  try { subtle.generateKey().catch(() => {}); } catch { threw = true; }
  assert(!threw, 'a bad call rejects, never throws');
  await assertThrows(() => new CryptoKey(), 'TypeError');
});
test('AES-GCM: wrapKey and unwrapKey', async () => {
  const kek = await subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['wrapKey', 'unwrapKey']);
  const key = await subtle.importKey('raw', unhex('00112233445566778899aabbccddeeff'), 'AES-GCM', true, ['encrypt', 'decrypt']);
  for (const format of ['raw', 'jwk']) {
    const p = { name: 'AES-GCM', iv: crypto.getRandomValues(new Uint8Array(12)) };
    const wrapped = await subtle.wrapKey(format, key, kek, p);
    const back = await subtle.unwrapKey(format, wrapped, kek, p, 'AES-GCM', true, ['encrypt']);
    assertEq(hexOf(await subtle.exportKey('raw', back)), '00112233445566778899aabbccddeeff', format);
    assertEq(back.usages, ['encrypt']);
  }
  const locked = await subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['encrypt']);
  await assertThrows(() => subtle.wrapKey('raw', locked, kek, { name: 'AES-GCM', iv: new Uint8Array(12) }), 'InvalidAccessError');
  await assertThrows(() => subtle.wrapKey('raw', key, key, { name: 'AES-GCM', iv: new Uint8Array(12) }), 'InvalidAccessError');
});

test('ECDSA: HMojis device key: generate, export the public JWK, sign, verify', async () => {
  const pair = await subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']);
  assertEq([pair.publicKey.type, pair.publicKey.extractable, pair.publicKey.algorithm, pair.publicKey.usages], ['public', true, { name: 'ECDSA', namedCurve: 'P-256' }, ['verify']]);
  assertEq([pair.privateKey.type, pair.privateKey.extractable, pair.privateKey.usages], ['private', false, ['sign']]);
  const j = await subtle.exportKey('jwk', pair.publicKey);
  assertEq(Object.keys(j), ['crv', 'ext', 'key_ops', 'kty', 'x', 'y']);
  assertEq([j.kty, j.crv, j.ext, j.key_ops, j.x.length, j.y.length], ['EC', 'P-256', true, ['verify'], 43, 43]);
  await assertThrows(() => subtle.exportKey('jwk', pair.privateKey), 'InvalidAccessError');
  const signed = enc(['HM1', 'GET', '/slack/hmojis/api/whoami', 1764000000000, 'nonce', ''].join('\n'));
  const sig = await subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, pair.privateKey, signed);
  assertEq(sig.byteLength, 64);
  assert(await subtle.verify({ name: 'ECDSA', hash: { name: 'SHA-256' } }, pair.publicKey, sig, signed));
  assert(!(await subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, sig, enc('other'))));
  assert(!(await subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, sig.slice(0, 63), signed)));
  assert(!(await subtle.verify({ name: 'ECDSA', hash: 'SHA-384' }, pair.publicKey, sig, signed)));
  // the server's side: the JWK it was given verifies the signature
  const pub = await subtle.importKey('jwk', { kty: 'EC', crv: 'P-256', x: j.x, y: j.y }, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify']);
  assert(await subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pub, sig, signed));
  await assertThrows(() => subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, signed), 'InvalidAccessError');
  await assertThrows(() => subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.privateKey, sig, signed), 'InvalidAccessError');
  await assertThrows(() => subtle.sign({ name: 'ECDSA' }, pair.privateKey, signed), 'TypeError');
  await assertThrows(() => subtle.sign({ name: 'ECDSA', hash: 'SHA-3' }, pair.privateKey, signed), 'NotSupportedError');
});
test('ECDSA: a signature OpenSSL made verifies, by JWK, SPKI and raw point', async () => {
  const jwk = { kty: 'EC', crv: 'P-256', x: '3s3SOq4kEPwjqpnXfGclVH20ymgSqJcqPvLYQMkjhGo', y: 'x5Pqj2T_MNMuT4velSnDdw28OcgbhrChGK-28etsQJI' };
  const data = enc('HM1\nGET\n/slack/hmojis/api/whoami\n1764000000000\nnonce\n');
  const sig = unhex('530d70141bdc653740e90dd19f15843f12f1f327e9bd34739e3b146e024a86e130900838f29eef46aeec67624523bf89b5950d7ae3b964e7bff7cb44508e8402');
  const spki = unhex('3059301306072a8648ce3d020106082a8648ce3d03010703420004decdd23aae2410fc23aa99d77c6725547db4ca6812a8972a3ef2d840c923846ac793ea8f64ff30d32e4f8bde9529c3770dbc39c81b86b0a118afb6f1eb6c4092');
  const alg = { name: 'ECDSA', namedCurve: 'P-256' }, p = { name: 'ECDSA', hash: 'SHA-256' };
  const byJwk = await subtle.importKey('jwk', jwk, alg, true, ['verify']);
  assert(await subtle.verify(p, byJwk, sig, data), 'jwk');
  assertEq(hexOf(await subtle.exportKey('spki', byJwk)), hexOf(spki));
  const bySpki = await subtle.importKey('spki', spki, alg, true, ['verify']);
  assert(await subtle.verify(p, bySpki, sig, data), 'spki');
  const raw = new Uint8Array(await subtle.exportKey('raw', bySpki));
  assertEq([raw.length, raw[0]], [65, 4]);
  assert(await subtle.verify(p, await subtle.importKey('raw', raw, alg, false, ['verify']), sig, data), 'raw');
  const compressed = new Uint8Array(33);
  compressed[0] = 2 + (raw[64] & 1);
  compressed.set(raw.subarray(1, 33), 1);
  assert(await subtle.verify(p, await subtle.importKey('raw', compressed, alg, false, ['verify']), sig, data), 'compressed point');
  const bent = new Uint8Array(sig);
  bent[0] ^= 1;
  assert(!(await subtle.verify(p, byJwk, bent, data)));
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, crv: 'P-384' }, alg, false, ['verify']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, x: jwk.y }, alg, false, ['verify']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, alg: 'ES384' }, alg, false, ['verify']), 'DataError');
  await assertThrows(() => subtle.importKey('spki', spki, { name: 'ECDSA', namedCurve: 'P-384' }, false, ['verify']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', jwk, alg, false, ['sign']), 'SyntaxError');
  await assertThrows(() => subtle.importKey('raw', raw.subarray(0, 64), alg, false, ['verify']), 'DataError');
  await assertThrows(() => subtle.exportKey('pkcs8', byJwk), 'InvalidAccessError');
});
test('ECDSA: private keys by JWK and PKCS #8, P-384 and P-521', async () => {
  for (const [namedCurve, hash, n] of [['P-256', 'SHA-256', 32], ['P-384', 'SHA-384', 48], ['P-521', 'SHA-512', 66]]) {
    const pair = await subtle.generateKey({ name: 'ECDSA', namedCurve }, true, ['sign', 'verify']);
    const data = enc('signed on ' + namedCurve);
    const sig = await subtle.sign({ name: 'ECDSA', hash }, pair.privateKey, data);
    assertEq(sig.byteLength, 2 * n, namedCurve);
    assert(await subtle.verify({ name: 'ECDSA', hash }, pair.publicKey, sig, data), namedCurve);
    const jwk = await subtle.exportKey('jwk', pair.privateKey);
    assertEq([jwk.crv, jwk.key_ops, typeof jwk.d], [namedCurve, ['sign'], 'string']);
    const again = await subtle.importKey('jwk', jwk, { name: 'ECDSA', namedCurve }, false, ['sign']);
    assert(await subtle.verify({ name: 'ECDSA', hash }, pair.publicKey, await subtle.sign({ name: 'ECDSA', hash }, again, data), data), 'jwk ' + namedCurve);
    const pkcs8 = await subtle.exportKey('pkcs8', pair.privateKey);
    const fromDer = await subtle.importKey('pkcs8', pkcs8, { name: 'ECDSA', namedCurve }, false, ['sign']);
    assert(await subtle.verify({ name: 'ECDSA', hash }, pair.publicKey, await subtle.sign({ name: 'ECDSA', hash }, fromDer, data), data), 'pkcs8 ' + namedCurve);
    const other = await subtle.generateKey({ name: 'ECDSA', namedCurve }, true, ['sign']);
    const otherJwk = await subtle.exportKey('jwk', other.privateKey);
    await assertThrows(() => subtle.importKey('jwk', { ...jwk, d: otherJwk.d }, { name: 'ECDSA', namedCurve }, false, ['sign']), 'DataError');
  }
  await assertThrows(() => subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-999' }, false, ['sign']), 'NotSupportedError');
  await assertThrows(() => subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['encrypt']), 'SyntaxError');
  await assertThrows(() => subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify']), 'SyntaxError');
  await assertThrows(() => subtle.generateKey({ name: 'ECDSA' }, false, ['sign']), 'TypeError');
  await assertThrows(() => subtle.deriveBits({ name: 'ECDH' }, null, 128), 'NotSupportedError');
});
test('CryptoKey: structuredClone copies it whole, extractable or not', async () => {
  const pair = await subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']);
  const copy = structuredClone({ priv: pair.privateKey, keyId: 'abc' });
  assert(copy.priv instanceof CryptoKey && copy.priv !== pair.privateKey);
  assertEq([copy.priv.type, copy.priv.extractable, copy.priv.algorithm, copy.priv.usages, copy.keyId], ['private', false, { name: 'ECDSA', namedCurve: 'P-256' }, ['sign'], 'abc']);
  const data = enc('x');
  assert(await subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, pair.publicKey, await subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, copy.priv, data), data));
});
test('JWK: the dictionary read as WebIDL converts it, as Blink does', async () => {
  const jwk = { kty: 'oct', k: 'AAAAAAAAAAAAAAAAAAAAAA', alg: 'A128GCM' };
  // ext a boolean and key_ops a sequence of strings, converted; a name that is no usage ignored
  const ops = { *[Symbol.iterator]() { yield 'encrypt'; yield 'bogus'; } };
  const k1 = await subtle.importKey('jwk', { ...jwk, ext: 'yes', key_ops: ops }, 'AES-GCM', true, ['encrypt']);
  assertEq([k1.extractable, k1.usages], [true, ['encrypt']]);
  // a member inherited, or got by a getter
  const k2 = await subtle.importKey('jwk', Object.create(jwk, { ext: { get: () => true } }), 'AES-GCM', true, ['encrypt']);
  assertEq(hexOf(await subtle.exportKey('raw', k2)), '00000000000000000000000000000000');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, key_ops: ['encrypt', 'encrypt'] }, 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, key_ops: 'encrypt' }, 'AES-GCM', false, ['encrypt']), 'TypeError');
  await assertThrows(() => subtle.importKey('jwk', { ...jwk, ext: 0 }, 'AES-GCM', true, ['encrypt']), 'DataError');
  // null is an empty JsonWebKey (no kty), a string none at all
  await assertThrows(() => subtle.importKey('jwk', null, 'AES-GCM', false, ['encrypt']), 'DataError');
  await assertThrows(() => subtle.importKey('jwk', 'oct', 'AES-GCM', false, ['encrypt']), 'TypeError');
  // the other review minors: an inherited curve name isn't one; an algorithm that's
  // no object a name; a bad curve before bad usages; P-224 a DataError
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(65), { name: 'ECDSA', namedCurve: 'toString' }, false, ['verify']), 'NotSupportedError');
  await assertThrows(() => subtle.importKey('raw', new Uint8Array(16), 42, false, ['encrypt']), 'NotSupportedError');
  await assertThrows(() => subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-999' }, false, ['encrypt']), 'NotSupportedError');
  const p224 = unhex('304e301006072a8648ce3d020106052b81040021033a0004' + 'b70e0cbd6bb4bf7f321390b94a03c1d356c21122343280d6115c1d21' + 'bd376388b5f723fb4c22dfe6cd4375a05a07476444d5819985007e34');
  await assertThrows(() => subtle.importKey('spki', p224, { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify']), 'DataError');
  // encrypt with a this that isn't a SubtleCrypto rejects, never throws
  const { encrypt } = subtle;
  let threw = false;
  try { await encrypt.call({}, { name: 'AES-GCM', iv: new Uint8Array(12) }, k1, new Uint8Array(1)).catch(() => {}); } catch { threw = true; }
  assert(!threw, 'a foreign this rejects');
});

