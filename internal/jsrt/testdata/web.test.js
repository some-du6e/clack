// web.test.js: Web platform APIs (web.js). Run by TestJSSuites against the
// fakeFetch endpoints in jsrt_test.go (http://test/…).
const parts = (u) => [u.href, u.origin, u.protocol, u.username, u.password, u.host, u.hostname, u.port, u.pathname, u.search, u.hash];
const bytesOf = (x) => Array.from(new Uint8Array(x));
const hex = (buf) => Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, '0')).join('');
const events = (target, types) => {
  const log = [];
  for (const t of types) target.addEventListener(t, (e) => log.push(t + (target.readyState !== undefined ? ':' + target.readyState : '')));
  return log;
};
const until = async (fn, ms = 2000) => {
  const t0 = performance.now();
  while (!fn()) {
    if (performance.now() - t0 > ms) throw new Error('timed out waiting');
    await sleep(5);
  }
};

// ---- URL ----
test('URL: Slack client URL components', () => {
  const u = new URL('https://app.slack.com/client/T0266FRGM/C0123?thread_ts=1.2&cdn_fallback=1#msg');
  assertEq(parts(u), ['https://app.slack.com/client/T0266FRGM/C0123?thread_ts=1.2&cdn_fallback=1#msg', 'https://app.slack.com', 'https:', '', '', 'app.slack.com', 'app.slack.com', '', '/client/T0266FRGM/C0123', '?thread_ts=1.2&cdn_fallback=1', '#msg']);
  assertEq(Object.prototype.toString.call(u), '[object URL]');
  assertEq(String(u), u.href);
  assertEq(JSON.stringify({ u }), JSON.stringify({ u: u.href }));
  assertEq(u.toJSON(), u.href);
});
test('URL: normalization (case, default port, dot segments)', () => {
  assertEq(new URL('HTTP://EXAMPLE.COM:80/a/../b/./c').href, 'http://example.com/b/c');
  assertEq(new URL('https://x.com:443/').port, '');
  assertEq(new URL('https://x.com:8443/').port, '8443');
  assertEq(new URL('http://x.com:00080/').href, 'http://x.com/');
  assertEq(new URL('ws://x.com:80/').host, 'x.com');
  assertEq(new URL('http://x.com/%2e%2E/a/%2e').pathname, '/a/');
  assertEq(new URL('http:\\\\x.com\\a\\b').href, 'http://x.com/a/b');
  assertEq(new URL('  http://x.com/\t\na  ').href, 'http://x.com/a');
  assertEq(new URL('http://EXAMPLE.com./').hostname, 'example.com.');
  assertEq(new URL('https://x.com').pathname, '/');
});
test('URL: relative resolution (RFC 3986 examples)', () => {
  const base = 'http://a/b/c/d;p?q';
  const cases = { g: 'http://a/b/c/g', './g': 'http://a/b/c/g', 'g/': 'http://a/b/c/g/', '/g': 'http://a/g', '//g': 'http://g/', '?y': 'http://a/b/c/d;p?y', 'g?y': 'http://a/b/c/g?y', '#s': 'http://a/b/c/d;p?q#s', 'g#s': 'http://a/b/c/g#s', ';x': 'http://a/b/c/;x', '': 'http://a/b/c/d;p?q', '.': 'http://a/b/c/', '..': 'http://a/b/', '../g': 'http://a/b/g', '../..': 'http://a/', '../../../g': 'http://a/g', 'g;x=1/../y': 'http://a/b/c/y' };
  for (const [rel, want] of Object.entries(cases)) assertEq(new URL(rel, base).href, want, rel);
  assertEq(new URL('/api/x', new URL('https://app.slack.com/client')).href, 'https://app.slack.com/api/x');
  assertEq(new URL('//cdn.example/x.png', 'https://app.slack.com/').href, 'https://cdn.example/x.png');
});
test('URL: hosts (IPv4, IPv6, IDNA, forbidden)', () => {
  assertEq(new URL('http://0x7f.1/').host, '127.0.0.1');
  assertEq(new URL('http://192.168.257/').host, '192.168.1.1');
  assertEq(new URL('http://4294967295/').host, '255.255.255.255');
  assertThrows(() => new URL('http://4294967296/'), 'TypeError');
  assertThrows(() => new URL('http://1.2.3.4.5/'), 'TypeError');
  assertThrows(() => new URL('http://foo.09/'), 'TypeError');
  assertEq(new URL('http://[2001:db8::0:1]:8080/').host, '[2001:db8::1]:8080');
  assertEq(new URL('http://[::ffff:192.168.0.1]/').hostname, '[::ffff:c0a8:1]');
  assertEq(new URL('http://[0:0:1:0:0:0:0:0]/').hostname, '[0:0:1::]');
  assertEq(new URL('http://[::]/').hostname, '[::]');
  assertThrows(() => new URL('http://[g::]/'), 'TypeError');
  assertEq(new URL('http://münchen.de/').hostname, 'xn--mnchen-3ya.de');
  assertEq(new URL('https://EXAMPLE.ＣＯＭ/').hostname, 'example.com');
  assertEq(new URL('http://ex%61mple.com/').hostname, 'example.com');
  assertThrows(() => new URL('http://a b/'), 'TypeError');
  assertThrows(() => new URL('http://a%20b/'), 'TypeError');
  assertThrows(() => new URL('http://a<b/'), 'TypeError');
  assertEq(new URL('sc://x/').host, 'x');
  assertThrows(() => new URL('sc://x y/'), 'TypeError');
});
test('URL: percent-encoding sets', () => {
  const u = new URL('http://user:pa ss@host.com:8080/p a t h^{}?q u e r y\'#f r a g`');
  assertEq(u.password, 'pa%20ss');
  assertEq(u.pathname, '/p%20a%20t%20h%5E%7B%7D');
  assertEq(u.search, "?q%20u%20e%20r%20y%27");
  assertEq(u.hash, '#f%20r%20a%20g%60');
  assertEq(new URL("sc://x/?a='b'").search, "?a='b'");
  assertEq(new URL('https://x.com/😀?é#ü').href, 'https://x.com/%F0%9F%98%80?%C3%A9#%C3%BC');
  assertEq(new URL('http://x/\ud800').pathname, '/%EF%BF%BD');
  assertEq(new URL('http://a@b@c/').username, 'a%40b');
});
test('URL: special, file and opaque schemes', () => {
  assertEq(new URL('file:///C:/x/../y').href, 'file:///C:/y');
  assertEq(new URL('file://localhost/etc/passwd').href, 'file:///etc/passwd');
  assertEq(new URL('file:c:\\foo\\bar').href, 'file:///c:/foo/bar');
  const m = new URL('mailto:someone@example.com?subject=hi');
  assertEq([m.protocol, m.pathname, m.search, m.host, m.origin], ['mailto:', 'someone@example.com', '?subject=hi', '', 'null']);
  assertEq(new URL('javascript:alert(1)').pathname, 'alert(1)');
  assertEq(new URL('non-spec://host/a/../b').href, 'non-spec://host/b');
  assertEq(new URL('non-spec:/.//p').href, 'non-spec:/.//p');
  assertEq(new URL('blob:https://a.com/uuid').origin, 'https://a.com');
  assertEq(new URL('data:text/plain,hello world').pathname, 'text/plain,hello world');
  assertEq(new URL('#x', 'mailto:a').href, 'mailto:a#x');
  assertThrows(() => new URL('x', 'mailto:a'), 'TypeError');
});
test('URL: invalid input', async () => {
  const e = await assertThrows(() => new URL('nope'), 'TypeError');
  assertEq(e.message, "Failed to construct 'URL': Invalid URL");
  await assertThrows(() => new URL('/x', 'also-bad'), 'TypeError');
  await assertThrows(() => new URL(), 'TypeError');
  await assertThrows(() => new URL('http://x.com:65536/'), 'TypeError');
  await assertThrows(() => new URL('http://'), 'TypeError');
  assertEq(URL.canParse('https://x'), true);
  assertEq(URL.canParse('x'), false);
  assertEq(URL.canParse('x', 'https://a/'), true);
  assertEq(URL.parse('nope'), null);
  assertEq(URL.parse('b', 'https://a/c').href, 'https://a/b');
  assert(URL.parse('https://x') instanceof URL);
});
test('URL: setters', async () => {
  const u = new URL('http://a.com/x?y#z');
  u.protocol = 'https';
  assertEq(u.href, 'https://a.com/x?y#z');
  u.protocol = 'foo';
  assertEq(u.protocol, 'https:');
  u.host = 'b.com:81';
  assertEq(u.host, 'b.com:81');
  u.hostname = 'c.com:99'; // a port in the hostname setter is a no-op
  assertEq(u.host, 'b.com:81');
  u.hostname = 'c.com';
  assertEq(u.host, 'c.com:81');
  u.port = '443';
  assertEq(u.port, '');
  u.port = '8080abc';
  assertEq(u.port, '8080');
  u.port = 'abc';
  assertEq(u.port, '8080');
  u.port = '';
  assertEq(u.host, 'c.com');
  u.pathname = '/a b/../c?#';
  assertEq(u.pathname, '/c%3F%23');
  u.search = '?a=b c#';
  assertEq(u.search, '?a=b%20c%23');
  u.hash = 'x y';
  assertEq(u.hash, '#x%20y');
  u.username = 'u@:';
  u.password = 'p@:';
  assertEq(u.href, 'https://u%40%3A:p%40%3A@c.com/c%3F%23?a=b%20c%23#x%20y');
  u.search = '';
  u.hash = '';
  assertEq(u.href, 'https://u%40%3A:p%40%3A@c.com/c%3F%23');
  u.href = 'http://other/';
  assertEq(u.host, 'other');
  const e = await assertThrows(() => { u.href = 'bad'; }, 'TypeError');
  assert(e.message.includes('Invalid URL'));
  const f = new URL('file:///x');
  f.username = 'u';
  f.port = '1';
  assertEq(f.href, 'file:///x');
  const m = new URL('mailto:a');
  m.pathname = 'b';
  assertEq(m.href, 'mailto:a');
});
test('URL: searchParams is live in both directions', () => {
  const u = new URL('https://x.com/p?a=1&b=2');
  const sp = u.searchParams;
  assert(sp === u.searchParams);
  assertEq(sp.get('a'), '1');
  sp.append('c', 'd e');
  assertEq(u.search, '?a=1&b=2&c=d+e');
  sp.delete('a');
  sp.delete('b');
  sp.delete('c');
  assertEq(u.href, 'https://x.com/p');
  u.search = '?z=9';
  assertEq(sp.get('z'), '9');
  u.href = 'https://x.com/?q=%20';
  assertEq(sp.get('q'), ' ');
  assertEq([...sp.keys()], ['q']);
});
test('URL.createObjectURL / revokeObjectURL / fetch(blob:)', async () => {
  const url = URL.createObjectURL(new Blob(['hi there'], { type: 'text/plain' }));
  assert(/^blob:clack\/[0-9a-f-]{36}$/.test(url), url);
  const r = await fetch(url);
  assertEq(r.status, 200);
  assertEq(r.headers.get('content-type'), 'text/plain');
  assertEq(await r.text(), 'hi there');
  URL.revokeObjectURL(url);
  await assertThrows(() => fetch(url), 'TypeError');
  await assertThrows(() => URL.createObjectURL('x'), 'TypeError');
});

// ---- URLSearchParams ----
test('URLSearchParams: constructors', async () => {
  assertEq([...new URLSearchParams('?a=1&b=2&a=3')], [['a', '1'], ['b', '2'], ['a', '3']]);
  assertEq([...new URLSearchParams({ x: 1, y: 'two' })], [['x', '1'], ['y', 'two']]);
  assertEq([...new URLSearchParams([['k', 'v'], ['k', 'w']])], [['k', 'v'], ['k', 'w']]);
  assertEq([...new URLSearchParams(new Map([['m', 'n']]))], [['m', 'n']]);
  const src = new URLSearchParams('a=b');
  const copy = new URLSearchParams(src);
  copy.append('c', 'd');
  assertEq(src.toString(), 'a=b');
  assertEq(new URLSearchParams().toString(), '');
  assertEq(new URLSearchParams(null).toString(), 'null=');
  await assertThrows(() => new URLSearchParams([['a']]), 'TypeError');
  await assertThrows(() => new URLSearchParams([1]), 'TypeError');
});
test('URLSearchParams: methods', () => {
  const p = new URLSearchParams('a=1&b=2&a=3');
  assertEq(p.size, 3);
  assertEq(p.get('a'), '1');
  assertEq(p.get('zz'), null);
  assertEq(p.getAll('a'), ['1', '3']);
  assert(p.has('a') && p.has('a', '3') && !p.has('a', '9'));
  p.delete('a', '1');
  assertEq(p.toString(), 'b=2&a=3');
  p.set('b', 'x');
  p.append('b', 'y');
  p.set('b', 'z');
  assertEq(p.toString(), 'b=z&a=3');
  const s = new URLSearchParams('z=1&a=2&z=0&a=1&é=3');
  s.sort();
  assertEq(s.toString(), 'a=2&a=1&z=1&z=0&%C3%A9=3');
  const seen = [];
  s.forEach((v, k, o) => { seen.push(k + v); assert(o === s); });
  assertEq(seen, ['a2', 'a1', 'z1', 'z0', 'é3']);
  assertEq([...s.values()].join(''), '21103');
  assertEq(Object.prototype.toString.call(s), '[object URLSearchParams]');
});
test('URLSearchParams: form-urlencoding', () => {
  const p = new URLSearchParams();
  p.append('q', 'a b&c=d+e/é~*-._!\'()');
  assertEq(p.toString(), 'q=a+b%26c%3Dd%2Be%2F%C3%A9%7E*-._%21%27%28%29');
  const d = new URLSearchParams('x=%20+y&z&=v&a=%zz&e=%E2%82%AC&bad=%FF');
  assertEq([...d], [['x', '  y'], ['z', ''], ['', 'v'], ['a', '%zz'], ['e', '€'], ['bad', '\ufffd']]);
});

// ---- TextEncoder / TextDecoder ----
test('TextEncoder: ascii, BMP, astral, lone surrogates', () => {
  const e = new TextEncoder();
  assertEq(e.encoding, 'utf-8');
  assertEq(bytesOf(e.encode('hi')), [104, 105]);
  assertEq(bytesOf(e.encode('é€😀')), [0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80]);
  assertEq(bytesOf(e.encode('\ud800x\udc00')), [0xef, 0xbf, 0xbd, 0x78, 0xef, 0xbf, 0xbd]);
  assertEq(e.encode().length, 0);
  assert(e.encode('x') instanceof Uint8Array);
});
test('TextEncoder.encodeInto', () => {
  const e = new TextEncoder();
  const buf = new Uint8Array(5);
  assertEq(e.encodeInto('a😀b', buf), { read: 3, written: 5 });
  assertEq(bytesOf(buf), [0x61, 0xf0, 0x9f, 0x98, 0x80]);
  const small = new Uint8Array(3);
  assertEq(e.encodeInto('a😀', small), { read: 1, written: 1 });
});
test('TextDecoder: round trips and replacement', () => {
  const d = new TextDecoder();
  const s = 'héllo wörld 😀 ' + 'x'.repeat(100) + '中文';
  assertEq(d.decode(new TextEncoder().encode(s)), s);
  const big = 'abc€😀'.repeat(20000);
  assertEq(d.decode(new TextEncoder().encode(big)), big);
  assertEq(d.decode(new Uint8Array([0xe2, 0x82])), '\ufffd');
  assertEq(d.decode(new Uint8Array([0xc0, 0x80])), '\ufffd\ufffd');
  assertEq(d.decode(new Uint8Array([0xed, 0xa0, 0x80])), '\ufffd\ufffd\ufffd');
  assertEq(d.decode(new Uint8Array([0xf0, 0x9f, 0x98, 0x41])), '\ufffdA');
  assertEq(d.decode(new Uint8Array([0xff, 0x61])), '\ufffda');
  assertEq(d.decode(new Uint8Array([0xef, 0xbb, 0xbf, 0x61])), 'a');
  assertEq(new TextDecoder('utf-8', { ignoreBOM: true }).decode(new Uint8Array([0xef, 0xbb, 0xbf, 0x61])), '\ufeffa');
  assertEq(d.decode(new Uint8Array([0x61, 0x62]).buffer), 'ab');
  assertEq(d.decode(new DataView(new Uint8Array([0x63]).buffer)), 'c');
  assertEq(d.decode(), '');
});
test('TextDecoder: fatal, stream, labels', async () => {
  const f = new TextDecoder('utf-8', { fatal: true });
  assertEq(f.fatal, true);
  await assertThrows(() => f.decode(new Uint8Array([0xff])), 'TypeError');
  assertEq(f.decode(new Uint8Array([0x6f, 0x6b])), 'ok');
  const d = new TextDecoder();
  const b = new TextEncoder().encode('a😀b');
  let out = '';
  for (const x of b) out += d.decode(new Uint8Array([x]), { stream: true });
  out += d.decode();
  assertEq(out, 'a😀b');
  assertEq(d.decode(new Uint8Array([0xf0, 0x9f]), { stream: true }), '');
  assertEq(d.decode(), '\ufffd');
  assertEq(new TextDecoder('UTF8').encoding, 'utf-8');
  assertEq(new TextDecoder(' latin1 ').encoding, 'windows-1252');
  assertEq(new TextDecoder('latin1').decode(new Uint8Array([0x63, 0x61, 0x66, 0xe9, 0x80])), 'café€');
  const u16 = new TextDecoder('utf-16le');
  assertEq(u16.encoding, 'utf-16le');
  assertEq(u16.decode(new Uint8Array([0xff, 0xfe, 0x68, 0, 0x3d, 0xd8, 0x00, 0xde])), 'h😀');
  assertEq(new TextDecoder('utf-16be').decode(new Uint8Array([0, 0x68, 0, 0x69])), 'hi');
  const e = await assertThrows(() => new TextDecoder('nope'), 'RangeError');
  assert(e.message.includes("'nope'"));
  await assertThrows(() => d.decode('str'), 'TypeError');
});

// ---- atob / btoa ----
test('atob / btoa', async () => {
  assertEq(btoa('hello'), 'aGVsbG8=');
  assertEq(btoa(''), '');
  assertEq(btoa('\xff\xfe'), '//4=');
  assertEq(atob('aGVsbG8='), 'hello');
  assertEq(atob(' aGVs bG8 '), 'hello');
  assertEq(atob('aGVsbG8'), 'hello');
  assertEq(atob('//4='), '\xff\xfe');
  let e = await assertThrows(() => btoa('✓'), 'InvalidCharacterError');
  assert(e instanceof DOMException && e.code === 5);
  e = await assertThrows(() => atob('a'), 'InvalidCharacterError');
  assert(e instanceof DOMException);
  await assertThrows(() => atob('ab$c'), 'InvalidCharacterError');
  await assertThrows(() => atob('a==='), 'InvalidCharacterError');
  const all = Array.from({ length: 256 }, (_, i) => String.fromCharCode(i)).join('');
  assertEq(atob(btoa(all)), all);
});

// ---- Blob / File / FileReader ----
test('Blob: parts, type, slice, readers', async () => {
  const b = new Blob(['ab', new Uint8Array([99, 100]), new Blob(['ef']), new Uint16Array([0x6867]).buffer], { type: 'Text/Plain' });
  assertEq(b.size, 8);
  assertEq(b.type, 'text/plain');
  assertEq(await b.text(), 'abcdefgh');
  assertEq(bytesOf(await b.arrayBuffer()).length, 8);
  assertEq(Array.from(await b.bytes()).slice(0, 2), [97, 98]);
  assertEq(await b.slice(2, 4).text(), 'cd');
  assertEq(await b.slice(-2).text(), 'gh');
  assertEq(await b.slice(1, -1, 'X/Y').text(), 'bcdefg');
  assertEq(b.slice(1, -1, 'X/Y').type, 'x/y');
  assertEq(b.slice(5, 2).size, 0);
  assertEq(new Blob().size, 0);
  assertEq(new Blob(['x'], { type: 'a\u00e9' }).type, '');
  assertEq(Object.prototype.toString.call(b), '[object Blob]');
  await assertThrows(() => new Blob('abc'), 'TypeError');
  const src = new Uint8Array([1, 2]);
  const c = new Blob([src]);
  src[0] = 9;
  assertEq(Array.from(await c.bytes()), [1, 2]);
  const r = b.stream().getReader();
  const { value } = await r.read();
  assertEq(new TextDecoder().decode(value), 'abcdefgh');
  assertEq((await r.read()).done, true);
});
test('File', async () => {
  const f = new File(['data'], 'a.txt', { type: 'text/plain', lastModified: 1234 });
  assert(f instanceof Blob && f instanceof File);
  assertEq([f.name, f.size, f.type, f.lastModified, f.webkitRelativePath], ['a.txt', 4, 'text/plain', 1234, '']);
  assert(Math.abs(new File([], 'x').lastModified - Date.now()) < 5000);
  assertEq(Object.prototype.toString.call(f), '[object File]');
  await assertThrows(() => new File(['x']), 'TypeError');
  const clone = structuredClone(f);
  assert(clone instanceof File);
  assertEq([clone.name, await clone.text()], ['a.txt', 'data']);
});
test('FileReader: text, data URL, array buffer, events', async () => {
  const blob = new Blob(['héllo'], { type: 'text/plain' });
  const fr = new FileReader();
  assertEq(fr.readyState, FileReader.EMPTY);
  const log = [];
  for (const t of ['loadstart', 'progress', 'load', 'loadend']) fr.addEventListener(t, () => log.push(t + fr.readyState));
  let onload = 0;
  fr.onload = () => onload++;
  fr.readAsText(blob);
  assertEq(fr.readyState, FileReader.LOADING);
  await assertThrows(() => fr.readAsText(blob), 'InvalidStateError');
  await until(() => log.length === 4);
  assertEq(log, ['loadstart1', 'progress1', 'load2', 'loadend2']);
  assertEq([fr.result, onload], ['héllo', 1]);
  const fr2 = new FileReader();
  fr2.readAsDataURL(blob);
  await new Promise((r) => { fr2.onloadend = r; });
  assertEq(fr2.result, 'data:text/plain;base64,aMOpbGxv');
  const fr3 = new FileReader();
  fr3.readAsArrayBuffer(new Blob([new Uint8Array([7, 8])]));
  await new Promise((r) => fr3.addEventListener('load', r));
  assertEq(bytesOf(fr3.result), [7, 8]);
  const fr4 = new FileReader();
  fr4.readAsBinaryString(new Blob([new Uint8Array([0xff, 0x41])]));
  await new Promise((r) => { fr4.onload = r; });
  assertEq(fr4.result, '\xffA');
  const fr5 = new FileReader();
  const l5 = [];
  fr5.onabort = () => l5.push('abort');
  fr5.onload = () => l5.push('load');
  fr5.readAsText(blob);
  fr5.abort();
  await sleep(20);
  assertEq([l5, fr5.readyState, fr5.result], [['abort'], 2, null]);
  await assertThrows(() => new FileReader().readAsText('x'), 'TypeError');
});

// ---- FormData ----
test('FormData: entries and File values', async () => {
  const fd = new FormData();
  fd.append('a', '1');
  fd.append('a', 2);
  fd.append('f', new Blob(['x'], { type: 'text/plain' }));
  fd.append('g', new Blob(['y']), 'g.bin');
  assertEq(fd.get('a'), '1');
  assertEq(fd.getAll('a'), ['1', '2']);
  assert(fd.get('f') instanceof File);
  assertEq([fd.get('f').name, fd.get('f').type, fd.get('g').name], ['blob', 'text/plain', 'g.bin']);
  assertEq(fd.get('nope'), null);
  fd.set('a', '3');
  assertEq(fd.getAll('a'), ['3']);
  assert(fd.has('g'));
  fd.delete('g');
  assert(!fd.has('g'));
  assertEq([...fd.keys()], ['a', 'f']);
  assertEq(Object.prototype.toString.call(fd), '[object FormData]');
  await assertThrows(() => fd.append('x', 'y', 'name'), 'TypeError');
});
test('FormData: multipart round trip through Response', async () => {
  const fd = new FormData();
  fd.append('text', 'héllo\nworld');
  fd.append('quote"name', 'v');
  fd.append('file', new File([new Uint8Array([0, 1, 255])], 'b.bin', { type: 'application/x-test' }));
  const r = new Response(fd);
  const ct = r.headers.get('content-type');
  assert(ct.startsWith('multipart/form-data; boundary='), ct);
  const back = await r.formData();
  assertEq(back.get('text'), 'héllo\r\nworld');
  assertEq(back.get('quote"name'), 'v');
  const f = back.get('file');
  assert(f instanceof File);
  assertEq([f.name, f.type, Array.from(await f.bytes())], ['b.bin', 'application/x-test', [0, 1, 255]]);
  const u = await new Response(new URLSearchParams('a=1&b=%20')).formData();
  assertEq([...u], [['a', '1'], ['b', ' ']]);
  await assertThrows(() => new Response('x').formData(), 'TypeError');
});

// ---- streams ----
test('ReadableStream: start/enqueue/close and reader', async () => {
  const rs = new ReadableStream({ start(c) { c.enqueue('a'); c.enqueue('b'); c.close(); } });
  assertEq(Object.prototype.toString.call(rs), '[object ReadableStream]');
  const r = rs.getReader();
  assert(rs.locked);
  await assertThrows(() => rs.getReader(), 'TypeError');
  assertEq(await r.read(), { value: 'a', done: false });
  assertEq(await r.read(), { value: 'b', done: false });
  assertEq((await r.read()).done, true);
  await r.closed;
  r.releaseLock();
  assert(!rs.locked);
});
test('ReadableStream: pull, backpressure and cancel', async () => {
  let pulls = 0, canceled;
  const rs = new ReadableStream({
    pull(c) { pulls++; c.enqueue(pulls); if (pulls === 5) c.close(); },
    cancel(reason) { canceled = reason; },
  }, { highWaterMark: 2 });
  await sleep(5);
  assertEq(pulls, 2);
  const r = rs.getReader();
  assertEq((await r.read()).value, 1);
  const out = [];
  for (;;) { const x = await r.read(); if (x.done) break; out.push(x.value); }
  assertEq(out, [2, 3, 4, 5]);
  const rs2 = new ReadableStream({ pull(c) { c.enqueue(1); }, cancel(reason) { canceled = reason; } });
  await rs2.cancel('why');
  assertEq(canceled, 'why');
  const errd = new ReadableStream({ start(c) { c.error(new RangeError('bad')); } });
  await assertThrows(() => errd.getReader().read(), 'RangeError');
});
test('ReadableStream: async iteration, from(), tee()', async () => {
  const got = [];
  for await (const x of ReadableStream.from([1, 2, 3])) got.push(x);
  assertEq(got, [1, 2, 3]);
  async function* gen() { yield 'x'; yield 'y'; }
  const got2 = [];
  for await (const x of ReadableStream.from(gen())) got2.push(x);
  assertEq(got2, ['x', 'y']);
  let canceled = false;
  const rs = new ReadableStream({ pull(c) { c.enqueue(1); }, cancel() { canceled = true; } });
  for await (const x of rs) { assertEq(x, 1); break; }
  assert(canceled && !rs.locked);
  const [a, b] = ReadableStream.from(['p', 'q']).tee();
  const all = async (s) => { const o = []; for await (const x of s) o.push(x); return o; };
  assertEq(await Promise.all([all(a), all(b)]), [['p', 'q'], ['p', 'q']]);
});
test('WritableStream: writer, backpressure, close, abort', async () => {
  const chunks = [];
  let closed = false;
  const ws = new WritableStream({
    write(c) { chunks.push(c); return sleep(1); },
    close() { closed = true; },
  }, new CountQueuingStrategy({ highWaterMark: 2 }));
  const w = ws.getWriter();
  assertEq(w.desiredSize, 2);
  w.write('a');
  w.write('b');
  assertEq(w.desiredSize, 0);
  await w.ready;
  await w.write('c');
  await w.close();
  await w.closed;
  assertEq([chunks, closed], [['a', 'b', 'c'], true]);
  let reason;
  const ws2 = new WritableStream({ abort(r) { reason = r; } });
  const w2 = ws2.getWriter();
  await w2.abort('stop');
  assertEq(reason, 'stop');
  await assertThrows(() => w2.write('x'));
  const ws3 = new WritableStream({ write() { throw new TypeError('sink failed'); } });
  const w3 = ws3.getWriter();
  await assertThrows(() => w3.write('x'), 'TypeError');
  await assertThrows(() => w3.closed, 'TypeError');
});
test('pipeTo / pipeThrough / TransformStream', async () => {
  const out = [];
  await ReadableStream.from(['a', 'b']).pipeTo(new WritableStream({ write(c) { out.push(c); } }));
  assertEq(out, ['a', 'b']);
  const upper = new TransformStream({ transform(chunk, c) { c.enqueue(String(chunk).toUpperCase()); }, flush(c) { c.enqueue('!'); } });
  const res = [];
  for await (const x of ReadableStream.from(['x', 'y']).pipeThrough(upper)) res.push(x);
  assertEq(res, ['X', 'Y', '!']);
  const text = await new Response(ReadableStream.from(['héllo ', 'wörld']).pipeThrough(new TextEncoderStream())).text();
  assertEq(text, 'héllo wörld');
  const dec = [];
  const enc = new TextEncoder().encode('😀ok');
  for await (const s of ReadableStream.from([enc.slice(0, 2), enc.slice(2)]).pipeThrough(new TextDecoderStream())) dec.push(s);
  assertEq(dec.join(''), '😀ok');
  let srcCanceled;
  const failing = new WritableStream({ write() { throw new Error('dest broke'); } });
  const src = new ReadableStream({ pull(c) { c.enqueue(1); }, cancel(r) { srcCanceled = r; } });
  await assertThrows(() => src.pipeTo(failing));
  assertEq(srcCanceled && srcCanceled.message, 'dest broke');
  const ac = new AbortController();
  const slow = new ReadableStream({ pull(c) { return sleep(5).then(() => c.enqueue(1)); } });
  const p = slow.pipeTo(new WritableStream(), { signal: ac.signal });
  ac.abort();
  await assertThrows(() => p, 'AbortError');
  const ts = new TransformStream();
  const w = ts.writable.getWriter();
  w.write(new Uint8Array([104, 105]));
  w.close();
  assertEq(await new Response(ts.readable).text(), 'hi');
});
test('byte streams and BYOB reader', async () => {
  const rs = new ReadableStream({ type: 'bytes', start(c) { c.enqueue(new Uint8Array([1, 2, 3, 4, 5])); c.close(); } });
  const r = rs.getReader({ mode: 'byob' });
  const a = await r.read(new Uint8Array(3));
  assertEq([Array.from(a.value), a.done], [[1, 2, 3], false]);
  const b = await r.read(new Uint8Array(8));
  assertEq(Array.from(b.value), [4, 5]);
  assertEq((await r.read(new Uint8Array(1))).done, true);
  await assertThrows(() => new ReadableStream().getReader({ mode: 'byob' }), 'TypeError');
});

// ---- Headers ----
test('Headers: case-insensitive, combined, sorted, set-cookie', async () => {
  const h = new Headers({ 'X-B': '1', 'content-type': 'text/plain' });
  h.append('x-b', ' 2 ');
  h.append('Set-Cookie', 'a=1');
  h.append('set-cookie', 'b=2');
  h.append('A-First', 'z');
  assertEq(h.get('X-b'), '1, 2');
  assertEq(h.get('Set-Cookie'), 'a=1, b=2');
  assertEq(h.getSetCookie(), ['a=1', 'b=2']);
  assertEq([...h], [['a-first', 'z'], ['content-type', 'text/plain'], ['set-cookie', 'a=1'], ['set-cookie', 'b=2'], ['x-b', '1, 2']]);
  assert(h.has('CONTENT-TYPE'));
  h.set('x-b', '3');
  assertEq(h.get('x-b'), '3');
  h.delete('X-B');
  assertEq(h.get('x-b'), null);
  assertEq([...new Headers([['k', 'v']]).keys()], ['k']);
  assertEq(new Headers(h).get('a-first'), 'z');
  const seen = [];
  new Headers({ b: '2', a: '1' }).forEach((v, k) => seen.push(k + v));
  assertEq(seen, ['a1', 'b2']);
  const e = await assertThrows(() => h.append('bad name', 'x'), 'TypeError');
  assert(e.message.includes('Invalid name'));
  await assertThrows(() => h.append('ok', 'a\nb'), 'TypeError');
  await assertThrows(() => new Headers([['a', 'b', 'c']]), 'TypeError');
  await assertThrows(() => new Headers('x'), 'TypeError');
  assertEq(Object.prototype.toString.call(h), '[object Headers]');
});

// ---- Request / Response ----
test('Request: URL resolution, method, defaults', async () => {
  const r = new Request('/api/conversations.history?x=1', { method: 'post', body: 'a=b', headers: { 'X-Test': '1' } });
  assertEq(r.url, 'https://app.slack.com/api/conversations.history?x=1');
  assertEq(r.method, 'POST');
  assertEq(new Request('https://x/', { method: 'patch' }).method, 'patch');
  assertEq(new Request('https://x/', { method: 'Delete' }).method, 'DELETE');
  assertEq(r.headers.get('content-type'), 'text/plain;charset=UTF-8');
  assertEq([r.mode, r.credentials, r.cache, r.redirect, r.referrer, r.destination, r.keepalive], ['cors', 'same-origin', 'default', 'follow', 'about:client', '', false]);
  assert(r.signal instanceof AbortSignal && !r.signal.aborted);
  assertEq(await r.clone().text(), 'a=b');
  assertEq(r.bodyUsed, false);
  assertEq(await r.text(), 'a=b');
  assertEq(r.bodyUsed, true);
  await assertThrows(() => r.text(), 'TypeError');
  await assertThrows(() => r.clone(), 'TypeError');
  await assertThrows(() => new Request('https://x/', { body: 'x' }), 'TypeError');
  await assertThrows(() => new Request('https://u:p@x/'), 'TypeError');
  await assertThrows(() => new Request('http://['), 'TypeError');
  await assertThrows(() => new Request('https://x/', { method: 'CONNECT' }), 'TypeError');
  await assertThrows(() => new Request('https://x/', { mode: 'bogus' }), 'TypeError');
  const ac = new AbortController();
  const r2 = new Request('https://x/', { signal: ac.signal });
  const r3 = new Request(r2, { headers: { a: 'b' } });
  ac.abort('gone');
  assert(r2.signal.aborted && r3.signal.aborted);
  assertEq(r3.signal.reason, 'gone');
  assertEq([r3.url, r3.headers.get('a')], ['https://x/', 'b']);
});
test('Response: constructor, statics, bodies', async () => {
  const r = new Response('hello', { status: 201, statusText: 'Created', headers: { 'X-A': '1' } });
  assertEq([r.status, r.statusText, r.ok, r.type, r.url, r.redirected, r.headers.get('x-a'), r.headers.get('content-type')], [201, 'Created', true, 'default', '', false, '1', 'text/plain;charset=UTF-8']);
  assert(r.body instanceof ReadableStream);
  const c = r.clone();
  assertEq(await r.text(), 'hello');
  assertEq(r.bodyUsed, true);
  assertEq(bytesOf(await c.arrayBuffer()), [104, 101, 108, 108, 111]);
  assertEq(new Response().body, null);
  assertEq(await new Response().text(), '');
  assertEq(await new Response(null, { status: 204 }).text(), '');
  await assertThrows(() => new Response('x', { status: 204 }), 'TypeError');
  const e = await assertThrows(() => new Response('', { status: 99 }), 'RangeError');
  assert(e.message.includes('[200, 599]'));
  const j = Response.json({ a: [1] }, { status: 202 });
  assertEq([j.status, j.headers.get('content-type'), await j.json()], [202, 'application/json', { a: [1] }]);
  const er = Response.error();
  assertEq([er.type, er.status, er.ok], ['error', 0, false]);
  await assertThrows(() => er.headers.set('a', 'b'), 'TypeError');
  const rd = Response.redirect('/elsewhere', 301);
  assertEq([rd.status, rd.headers.get('location')], [301, 'https://app.slack.com/elsewhere']);
  await assertThrows(() => Response.redirect('/x', 200), 'RangeError');
  const blob = await new Response(new Uint8Array([1, 2]), { headers: { 'content-type': 'Image/PNG' } }).blob();
  assertEq([blob.type, blob.size], ['image/png', 2]);
  assertEq(Array.from(await new Response('ab').bytes()), [97, 98]);
  assertEq(await new Response(new Blob(['blob body'])).text(), 'blob body');
  assertEq(await new Response(ReadableStream.from([new Uint8Array([111]), new Uint8Array([107])])).text(), 'ok');
  await assertThrows(() => new Response(ReadableStream.from(['str'])).text(), 'TypeError');
  const streamed = new Response('abc');
  const reader = streamed.body.getReader();
  assertEq(new TextDecoder().decode((await reader.read()).value), 'abc');
  assertEq(streamed.bodyUsed, true);
  await assertThrows(() => streamed.text(), 'TypeError');
  await assertThrows(() => Response.json(undefined), 'TypeError');
  assertEq(await new Response('\ufeff{"x":1}').json(), { x: 1 });
});

// ---- fetch ----
test('fetch: GET json', async () => {
  const r = await fetch('http://test/json?q=x');
  assertEq([r.status, r.ok, r.statusText, r.url, r.redirected, r.type], [200, true, 'OK', 'http://test/json?q=x', false, 'cors']);
  assertEq(r.headers.get('Content-Type'), 'application/json');
  await assertThrows(() => r.headers.set('x', 'y'), 'TypeError');
  assertEq(await r.json(), { ok: true, n: 42, q: 'x' });
  await assertThrows(() => r.json(), 'TypeError');
  assert(r instanceof Response);
});
test('fetch: POST bodies reach the host', async () => {
  let r = await fetch('http://test/echo', { method: 'POST', body: 'héllo', headers: { 'X-Custom': 'yes' } });
  assertEq([r.headers.get('x-method'), r.headers.get('x-custom'), r.headers.get('content-type'), await r.text()], ['POST', 'yes', 'text/plain;charset=UTF-8', 'héllo']);
  r = await fetch('http://test/echo', { method: 'PUT', body: new URLSearchParams({ a: '1 2' }) });
  assertEq([r.headers.get('x-method'), r.headers.get('content-type'), await r.text()], ['PUT', 'application/x-www-form-urlencoded;charset=UTF-8', 'a=1+2']);
  r = await fetch('http://test/echo', { method: 'POST', body: new Uint8Array([1, 2, 3]).buffer });
  assertEq(bytesOf(await r.arrayBuffer()), [1, 2, 3]);
  r = await fetch('http://test/echo', { method: 'POST', body: new Blob(['blob!'], { type: 'text/x-blob' }) });
  assertEq([r.headers.get('content-type'), await r.text()], ['text/x-blob', 'blob!']);
  r = await fetch('http://test/echo', { method: 'POST', body: ReadableStream.from([new Uint8Array([65, 66])]), duplex: 'half' });
  assertEq(await r.text(), 'AB');
  const fd = new FormData();
  fd.append('k', 'v');
  fd.append('file', new File(['contents'], 'c.txt', { type: 'text/plain' }));
  r = await fetch('http://test/echo', { method: 'POST', body: fd });
  const back = await r.formData();
  assertEq([back.get('k'), await back.get('file').text(), back.get('file').name], ['v', 'contents', 'c.txt']);
  const req = new Request('http://test/echo', { method: 'POST', body: 'from request' });
  r = await fetch(req);
  assertEq(await r.text(), 'from request');
  assert(req.bodyUsed);
});
test('fetch: status codes, bytes, HEAD', async () => {
  const r = await fetch('http://test/404');
  assertEq([r.status, r.ok, r.statusText, await r.text()], [404, false, 'Not Found', 'not found']);
  const b = await fetch('http://test/bytes');
  assertEq(bytesOf(await b.arrayBuffer()), [0, 1, 2, 255]);
  const h = await fetch('http://test/json', { method: 'HEAD' });
  assertEq([h.status, h.body], [200, null]);
  const t = await fetch(new URL('http://test/some/path#frag'));
  assertEq([t.url, await t.text()], ['http://test/some/path', 'hello /some/path']);
});
test('fetch: abort and timeout', async () => {
  let t0 = performance.now();
  let e = await assertThrows(() => fetch('http://test/slow', { signal: AbortSignal.timeout(50) }), 'TimeoutError');
  assert(e instanceof DOMException);
  assert(performance.now() - t0 < 1500, 'timeout took ' + (performance.now() - t0));
  const ac = new AbortController();
  t0 = performance.now();
  const p = fetch('http://test/slow', { signal: ac.signal });
  setTimeout(() => ac.abort(), 20);
  e = await assertThrows(() => p, 'AbortError');
  assertEq(e.message, 'signal is aborted without reason');
  assert(performance.now() - t0 < 1500);
  await assertThrows(() => fetch('http://test/json', { signal: AbortSignal.abort(new RangeError('pre')) }), 'RangeError');
  const any = AbortSignal.any([new AbortController().signal, AbortSignal.timeout(30)]);
  await assertThrows(() => fetch('http://test/slow', { signal: any }), 'TimeoutError');
  // the runtime still works after aborts
  assertEq((await fetch('http://test/json?q=after')).status, 200);
});
test('fetch: data: URLs and unsupported schemes', async () => {
  let r = await fetch('data:,hello%20world');
  assertEq([r.status, r.headers.get('content-type'), await r.text()], [200, 'text/plain;charset=US-ASCII', 'hello world']);
  r = await fetch('data:text/html;charset=utf-8;base64,PGI+aGk8L2I+');
  assertEq([r.headers.get('content-type'), await r.text()], ['text/html;charset=utf-8', '<b>hi</b>']);
  r = await fetch('data:application/octet-stream;base64,AAH/');
  assertEq(bytesOf(await r.arrayBuffer()), [0, 1, 255]);
  await assertThrows(() => fetch('data:text/plain;base64,%%%'), 'TypeError');
  const e = await assertThrows(() => fetch('ftp://x/'), 'TypeError');
  assertEq(e.message, 'Failed to fetch');
  await assertThrows(() => fetch('about:blank'), 'TypeError');
  await assertThrows(() => fetch('http://['), 'TypeError');
  assert(fetch('data:,x') instanceof Promise);
});

// ---- XMLHttpRequest ----
test('XMLHttpRequest: GET json with events', async () => {
  const x = new XMLHttpRequest();
  assertEq([XMLHttpRequest.DONE, x.DONE, x.readyState, x.status], [4, 4, 0, 0]);
  const states = [];
  x.onreadystatechange = () => states.push(x.readyState);
  const log = [];
  for (const t of ['loadstart', 'progress', 'load', 'loadend']) x.addEventListener(t, (e) => log.push(t + (e instanceof ProgressEvent ? '' : '?')));
  x.open('GET', 'http://test/json?q=xhr');
  x.responseType = 'json';
  x.setRequestHeader('X-Custom', 'a');
  x.setRequestHeader('x-custom', 'b');
  x.send();
  await new Promise((r) => { x.onloadend = r; });
  assertEq(states, [1, 2, 3, 4]);
  assertEq(log, ['loadstart', 'progress', 'load', 'loadend']);
  assertEq([x.status, x.statusText, x.responseURL], [200, 'OK', 'http://test/json?q=xhr']);
  assertEq(x.response, { ok: true, n: 42, q: 'xhr' });
  assertEq(x.getResponseHeader('content-type'), 'application/json');
  assert(x.getAllResponseHeaders().includes('content-type: application/json\r\n'));
  await assertThrows(() => x.responseText, 'InvalidStateError');
  await assertThrows(() => { x.responseType = 'text'; }, 'InvalidStateError');
});
test('XMLHttpRequest: POST text, arraybuffer, blob', async () => {
  const load = (x) => new Promise((r) => x.addEventListener('load', r));
  const a = new XMLHttpRequest();
  a.open('POST', 'http://test/echo');
  a.setRequestHeader('X-Custom', 'hdr');
  a.send('payload');
  await load(a);
  assertEq([a.responseText, a.response, a.getResponseHeader('X-Method'), a.getResponseHeader('x-custom')], ['payload', 'payload', 'POST', 'hdr']);
  const b = new XMLHttpRequest();
  b.open('GET', 'http://test/bytes');
  b.responseType = 'arraybuffer';
  b.send();
  await load(b);
  assert(b.response instanceof ArrayBuffer);
  assertEq(bytesOf(b.response), [0, 1, 2, 255]);
  const c = new XMLHttpRequest();
  c.open('GET', 'http://test/bytes');
  c.responseType = 'blob';
  c.send();
  await load(c);
  assertEq([c.response instanceof Blob, c.response.type, c.response.size], [true, 'application/octet-stream', 4]);
  const d = new XMLHttpRequest();
  d.open('GET', '/relative');
  assertEq(d.readyState, 1);
  await assertThrows(() => d.open('GET', 'http://['), 'SyntaxError');
  await assertThrows(() => new XMLHttpRequest().send(), 'InvalidStateError');
  await assertThrows(() => new XMLHttpRequest().setRequestHeader('a', 'b'), 'InvalidStateError');
});
test('XMLHttpRequest: abort, timeout, network error', async () => {
  const x = new XMLHttpRequest();
  const log = [];
  for (const t of ['readystatechange', 'abort', 'error', 'load', 'loadend']) x.addEventListener(t, () => log.push(t + x.readyState));
  x.open('GET', 'http://test/slow');
  x.send();
  x.abort();
  assertEq(x.readyState, 0);
  assertEq(log, ['readystatechange1', 'readystatechange4', 'abort4', 'loadend4']);
  const t = new XMLHttpRequest();
  t.open('GET', 'http://test/slow');
  t.timeout = 50;
  const t0 = performance.now();
  const ev = await new Promise((r) => { t.ontimeout = () => r('timeout'); t.onload = () => r('load'); t.send(); });
  assertEq([ev, t.readyState, t.status], ['timeout', 4, 0]);
  assert(performance.now() - t0 < 1500);
  const n = new XMLHttpRequest();
  n.open('GET', 'ftp://nowhere/');
  const nev = await new Promise((r) => { n.onerror = (e) => r(e.type); n.send(); });
  assertEq([nev, n.status, n.readyState], ['error', 0, 4]);
});
test('XMLHttpRequest: NoTrack-style prototype patching', async () => {
  const { open, send } = XMLHttpRequest.prototype;
  let blocked = 0;
  try {
    XMLHttpRequest.prototype.open = function (method, url, ...rest) {
      Reflect.deleteProperty(this, 'readyState');
      return Reflect.apply(open, this, [method, url, ...rest]);
    };
    XMLHttpRequest.prototype.send = function (body) {
      blocked++;
      setTimeout(() => {
        Object.defineProperty(this, 'readyState', { value: XMLHttpRequest.DONE, configurable: true });
        this.dispatchEvent(new Event('readystatechange'));
        this.dispatchEvent(new ProgressEvent('error'));
        this.dispatchEvent(new ProgressEvent('loadend'));
      });
    };
    const x = new XMLHttpRequest();
    x.open('POST', 'https://slack.com/beacon/track');
    const got = await new Promise((r) => { x.onerror = () => r(x.readyState); x.send('{}'); });
    assertEq([got, blocked], [4, 1]);
    x.open('GET', 'http://test/json');
    assertEq(x.readyState, 1);
  } finally {
    XMLHttpRequest.prototype.open = open;
    XMLHttpRequest.prototype.send = send;
  }
});

// ---- AbortController / AbortSignal ----
test('AbortController / AbortSignal', async () => {
  const ac = new AbortController();
  const s = ac.signal;
  assert(s instanceof EventTarget);
  assertEq([s.aborted, s.reason], [false, undefined]);
  let fired = 0, onabort = 0;
  s.addEventListener('abort', (e) => { fired++; assertEq(e.type, 'abort'); });
  s.onabort = () => onabort++;
  s.throwIfAborted();
  ac.abort();
  ac.abort();
  assertEq([s.aborted, fired, onabort], [true, 1, 1]);
  assert(s.reason instanceof DOMException);
  assertEq([s.reason.name, s.reason.message, s.reason.code], ['AbortError', 'signal is aborted without reason', 20]);
  await assertThrows(() => s.throwIfAborted(), 'AbortError');
  const c2 = new AbortController();
  c2.abort('custom');
  assertEq(c2.signal.reason, 'custom');
  assertEq(AbortSignal.abort().reason.name, 'AbortError');
  assertEq(AbortSignal.abort(42).reason, 42);
  await assertThrows(() => new AbortSignal(), 'TypeError');
  const t = AbortSignal.timeout(10);
  await new Promise((r) => t.addEventListener('abort', r));
  assertEq([t.reason.name, t.reason.message], ['TimeoutError', 'signal timed out']);
  assertEq(Object.prototype.toString.call(s), '[object AbortSignal]');
});
test('AbortSignal.any', async () => {
  const a = new AbortController(), b = new AbortController();
  const any = AbortSignal.any([a.signal, b.signal]);
  const nested = AbortSignal.any([any]);
  let n = 0;
  any.onabort = () => n++;
  b.abort('B');
  a.abort('A');
  assertEq([any.aborted, any.reason, nested.aborted, nested.reason, n], [true, 'B', true, 'B', 1]);
  const pre = AbortSignal.any([new AbortController().signal, AbortSignal.abort('pre')]);
  assertEq([pre.aborted, pre.reason], [true, 'pre']);
  const t = AbortSignal.any([AbortSignal.timeout(10)]);
  await new Promise((r) => t.addEventListener('abort', r));
  assertEq(t.reason.name, 'TimeoutError');
  await assertThrows(() => AbortSignal.any([{}]), 'TypeError');
  const ac = new AbortController();
  const el = new EventTarget();
  let hits = 0;
  el.addEventListener('x', () => hits++, { signal: ac.signal });
  el.dispatchEvent(new Event('x'));
  ac.abort();
  el.dispatchEvent(new Event('x'));
  assertEq(hits, 1);
});

// ---- crypto ----
test('crypto.getRandomValues / randomUUID', async () => {
  const a = new Uint32Array(16);
  assert(crypto.getRandomValues(a) === a);
  assert(a.some((x) => x !== 0));
  const b = new Uint8Array(65536);
  crypto.getRandomValues(b);
  let e = await assertThrows(() => crypto.getRandomValues(new Uint8Array(65537)), 'QuotaExceededError');
  assert(e instanceof DOMException);
  e = await assertThrows(() => crypto.getRandomValues(new Float32Array(4)), 'TypeMismatchError');
  assert(e instanceof DOMException);
  await assertThrows(() => crypto.getRandomValues([1, 2]), 'TypeError');
  crypto.getRandomValues(new BigUint64Array(2));
  const ids = new Set();
  for (let i = 0; i < 300; i++) {
    const id = crypto.randomUUID();
    assert(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(id), id);
    ids.add(id);
  }
  assertEq(ids.size, 300);
  assertEq(Object.prototype.toString.call(crypto), '[object Crypto]');
});
test('crypto.subtle.digest', async () => {
  const abc = new TextEncoder().encode('abc');
  assertEq(hex(await crypto.subtle.digest('SHA-256', abc)), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  assertEq(hex(await crypto.subtle.digest({ name: 'sha-1' }, abc.buffer)), 'a9993e364706816aba3e25717850c26c9cd0d89d');
  assertEq((await crypto.subtle.digest('SHA-512', new Uint8Array(0))).byteLength, 64);
  assert((await crypto.subtle.digest('SHA-384', abc)) instanceof ArrayBuffer);
  await assertThrows(() => crypto.subtle.digest('MD5', abc), 'NotSupportedError');
  await assertThrows(() => crypto.subtle.digest('SHA-256', 'str'), 'TypeError');
  await assertThrows(() => crypto.subtle.importKey('raw', abc, 'AES-GCM', false, ['encrypt']), 'NotSupportedError');
});

// ---- Storage ----
test('localStorage / sessionStorage', async () => {
  localStorage.clear();
  sessionStorage.clear();
  assert(localStorage instanceof Storage);
  assertEq(localStorage.length, 0);
  localStorage.setItem('a', 1);
  localStorage.setItem('b', { toString: () => 'obj' });
  assertEq([localStorage.getItem('a'), localStorage.getItem('b'), localStorage.getItem('zz')], ['1', 'obj', null]);
  assertEq(localStorage.length, 2);
  assertEq([localStorage.key(0), localStorage.key(1), localStorage.key(5)], ['a', 'b', null]);
  localStorage.c = 'prop';
  assertEq([localStorage.c, localStorage.getItem('c'), localStorage.nope], ['prop', 'prop', undefined]);
  assert('c' in localStorage && !('nope' in localStorage));
  assertEq(Object.keys(localStorage), ['a', 'b', 'c']);
  assertEq(JSON.stringify(localStorage), '{"a":"1","b":"obj","c":"prop"}');
  delete localStorage.c;
  assertEq(localStorage.getItem('c'), null);
  localStorage.setItem('getItem', 'shadowed');
  assertEq(typeof localStorage.getItem, 'function');
  assertEq(localStorage.getItem('getItem'), 'shadowed');
  localStorage.removeItem('getItem');
  localStorage.removeItem('a');
  assertEq(Object.keys(localStorage), ['b']);
  sessionStorage.setItem('s', 'x');
  assertEq([sessionStorage.getItem('s'), localStorage.getItem('s'), sessionStorage.length], ['x', null, 1]);
  localStorage.clear();
  assertEq(localStorage.length, 0);
  await assertThrows(() => localStorage.setItem('x'), 'TypeError');
  await assertThrows(() => new Storage(), 'TypeError');
  assertEq(Object.prototype.toString.call(localStorage), '[object Storage]');
});

// ---- BroadcastChannel / MessageChannel ----
test('BroadcastChannel', async () => {
  const a = new BroadcastChannel('taut:test'), b = new BroadcastChannel('taut:test'), c = new BroadcastChannel('other');
  const got = { a: [], b: [], c: [] };
  a.onmessage = (e) => got.a.push(e.data);
  b.addEventListener('message', (e) => { got.b.push(e.data); assert(e instanceof MessageEvent); });
  c.onmessage = (e) => got.c.push(e.data);
  const msg = { k: [1, 2], d: new Date(5) };
  a.postMessage(msg);
  msg.k.push(3);
  assertEq(got.b.length, 0);
  await until(() => got.b.length === 1);
  assertEq([got.a.length, got.c.length, got.b[0].k, got.b[0].d instanceof Date], [0, 0, [1, 2], true]);
  b.close();
  a.postMessage('again');
  await sleep(10);
  assertEq(got.b.length, 1);
  assertEq(a.name, 'taut:test');
  b.close();
  await assertThrows(() => b.postMessage('x'), 'InvalidStateError');
  await assertThrows(() => a.postMessage(() => {}), 'DataCloneError');
  a.close();
  c.close();
});
test('MessageChannel / MessagePort', async () => {
  const { port1, port2 } = new MessageChannel();
  const got = [];
  port1.onmessage = (e) => got.push(e.data);
  port2.postMessage('x');
  port2.postMessage({ y: 1 });
  await until(() => got.length === 2);
  assertEq(got, ['x', { y: 1 }]);
  const ch = new MessageChannel();
  const got2 = [];
  ch.port2.addEventListener('message', (e) => got2.push(e.data));
  ch.port1.postMessage('queued');
  await sleep(10);
  assertEq(got2, []);
  ch.port2.start();
  await until(() => got2.length === 1);
  ch.port1.close();
  ch.port1.postMessage('dropped');
  await sleep(10);
  assertEq(got2, ['queued']);
});

// ---- navigator ----
test('navigator basics', async () => {
  assert(navigator.userAgent.includes('Chrome/') && navigator.userAgent.startsWith('Mozilla/5.0 ('), navigator.userAgent);
  assertEq([navigator.vendor, navigator.onLine, navigator.cookieEnabled, navigator.hardwareConcurrency, navigator.maxTouchPoints], ['Google Inc.', true, true, 4, 0]);
  assertEq(navigator.language, 'en-US');
  assertEq(navigator.languages, ['en-US', 'en']);
  assert(typeof navigator.platform === 'string' && navigator.platform.length > 0);
  assert(navigator.userAgentData.brands.some((b) => b.brand === 'Google Chrome'));
  assertEq(navigator.userAgentData.mobile, false);
  assertEq((await navigator.userAgentData.getHighEntropyValues(['architecture'])).architecture, 'x86');
  assertEq(navigator.vibrate(100), false);
  assertEq(navigator.getBattery, undefined);
  assertEq(navigator.serviceWorker, undefined);
  assertEq((await navigator.permissions.query({ name: 'notifications' })).state, 'prompt');
  assert((await navigator.storage.estimate()).quota > 0);
  assertEq(navigator.sendBeacon('http://test/echo', 'beacon'), true);
  const sb = navigator.sendBeacon.bind(navigator);
  assertEq(sb('/relative'), true);
  assertEq(Object.prototype.toString.call(navigator), '[object Navigator]');
  assertEq(typeof indexedDB, 'undefined');
  assertEq(typeof CompressionStream, 'undefined');
});
test('navigator.clipboard (no host clipboard in tests)', async () => {
  const e = await assertThrows(() => navigator.clipboard.writeText('x'), 'NotAllowedError');
  assert(e instanceof DOMException);
  await assertThrows(() => navigator.clipboard.readText(), 'NotAllowedError');
  const item = new ClipboardItem({ 'text/plain': 'hi' });
  assertEq(item.types, ['text/plain']);
  assertEq(await (await item.getType('text/plain')).text(), 'hi');
});
test('navigator.mediaDevices can be wrapped (StreamerMode)', async () => {
  const media = navigator.mediaDevices;
  const { getDisplayMedia, getUserMedia } = media;
  await assertThrows(() => getUserMedia.call(media, { audio: true }), 'NotAllowedError');
  await assertThrows(() => media.getDisplayMedia(), 'NotAllowedError');
  assertEq(await media.enumerateDevices(), []);
  let wrapped = 0;
  media.getDisplayMedia = async (...args) => { wrapped++; return getDisplayMedia.apply(media, args); };
  await assertThrows(() => navigator.mediaDevices.getDisplayMedia(), 'NotAllowedError');
  assertEq(wrapped, 1);
  media.getDisplayMedia = getDisplayMedia;
});
test('navigator.locks: exclusive queueing and results', async () => {
  const order = [];
  const p1 = navigator.locks.request('L', async (lock) => {
    assertEq([lock.name, lock.mode], ['L', 'exclusive']);
    order.push('1 start');
    await sleep(20);
    order.push('1 end');
    return 'one';
  });
  const p2 = navigator.locks.request('L', async () => { order.push('2'); return 'two'; });
  const q = await navigator.locks.query();
  assertEq([q.held.length, q.pending.length], [1, 1]);
  assertEq(await Promise.all([p1, p2]), ['one', 'two']);
  assertEq(order, ['1 start', '1 end', '2']);
  await assertThrows(() => navigator.locks.request('L', async () => { throw new RangeError('cb failed'); }), 'RangeError');
  assertEq(await navigator.locks.request('L', () => 5), 5);
  assertEq((await navigator.locks.query()).held.length, 0);
});
test('navigator.locks: shared, ifAvailable, steal, signal', async () => {
  let concurrent = 0, max = 0;
  const shared = () => navigator.locks.request('S', { mode: 'shared' }, async () => { concurrent++; max = Math.max(max, concurrent); await sleep(10); concurrent--; });
  await Promise.all([shared(), shared(), shared()]);
  assertEq(max, 3);
  let release;
  const held = navigator.locks.request('A', () => new Promise((r) => { release = r; }));
  await sleep(1);
  assertEq(await navigator.locks.request('A', { ifAvailable: true }, (lock) => lock), null);
  const ac = new AbortController();
  const waiting = navigator.locks.request('A', { signal: ac.signal }, () => 'never');
  ac.abort();
  await assertThrows(() => waiting, 'AbortError');
  const stolen = navigator.locks.request('A', { steal: true }, (lock) => lock.name + ' stolen');
  await assertThrows(() => held, 'AbortError');
  assertEq(await stolen, 'A stolen');
  release();
  await assertThrows(() => navigator.locks.request('-x', () => {}), 'NotSupportedError');
  await assertThrows(() => navigator.locks.request('x', { steal: true, ifAvailable: true }, () => {}), 'NotSupportedError');
  await assertThrows(() => navigator.locks.request('x', { mode: 'shared', steal: true }, () => {}), 'NotSupportedError');
  await assertThrows(() => navigator.locks.request('x', { signal: AbortSignal.abort('r') }, () => {}));
  assertEq(await navigator.locks.request('A', () => 'free again'), 'free again');
});

// ---- location / history ----
test('location and history', async () => {
  assertEq(location.href, 'https://app.slack.com/client/T0266FRGM');
  assertEq([location.origin, location.protocol, location.host, location.hostname, location.port, location.pathname, location.search, location.hash], ['https://app.slack.com', 'https:', 'app.slack.com', 'app.slack.com', '', '/client/T0266FRGM', '', '']);
  assertEq(String(location), location.href);
  assertEq(window.origin, 'https://app.slack.com');
  const len = history.length;
  history.pushState({ n: 1 }, '', '/client/T0266FRGM/C42?x=1');
  assertEq([location.pathname, location.search, history.length, history.state], ['/client/T0266FRGM/C42', '?x=1', len + 1, { n: 1 }]);
  history.replaceState(null, '', '#h');
  assertEq([location.hash, history.length, history.state], ['#h', len + 1, null]);
  await assertThrows(() => history.pushState(null, '', 'https://evil.example/'), 'SecurityError');
  location.assign('/client/T0266FRGM/D1');
  assertEq(location.pathname, '/client/T0266FRGM/D1');
  location.hash = 'frag';
  assertEq(location.href, 'https://app.slack.com/client/T0266FRGM/D1#frag');
  location.assign('https://example.com/elsewhere');
  assertEq(location.hostname, 'app.slack.com');
  await assertThrows(() => location.assign('http://['), 'SyntaxError');
  location.reload();
  assertEq(new Request('rel').url, 'https://app.slack.com/client/T0266FRGM/rel');
  history.replaceState(null, '', '/client/T0266FRGM');
  assertEq(location.href, 'https://app.slack.com/client/T0266FRGM');
  assertEq(Object.prototype.toString.call(location), '[object Location]');
});

// ---- misc ----
test('Notification stub', async () => {
  assertEq(Notification.permission, 'default');
  const n = new Notification('title', { body: 'b', tag: 't', data: { x: 1 } });
  assertEq([n.title, n.body, n.tag, n.data], ['title', 'b', 't', { x: 1 }]);
  await new Promise((r) => { n.onerror = r; });
  assertEq(await Notification.requestPermission(), 'denied');
  assertEq(Notification.permission, 'denied');
  n.close();
});
test('WebSocket / EventSource stubs fail asynchronously', async () => {
  const ws = new WebSocket('wss://wss-primary.slack.com/?token=x');
  assertEq([ws.readyState, WebSocket.CONNECTING, ws.url, ws.binaryType], [0, 0, 'wss://wss-primary.slack.com/?token=x', 'blob']);
  await assertThrows(() => ws.send('x'), 'InvalidStateError');
  const log = [];
  ws.onerror = () => log.push('error');
  const close = await new Promise((r) => { ws.onclose = r; });
  assertEq([log, ws.readyState, close.code, close.wasClean, close instanceof CloseEvent], [['error'], 3, 1006, false, true]);
  assertEq(new WebSocket('https://x/').url, 'wss://x/');
  await assertThrows(() => new WebSocket('ftp://x/'), 'SyntaxError');
  await assertThrows(() => new WebSocket('ws://x/#f'), 'SyntaxError');
  const es = new EventSource('/events');
  assertEq([es.readyState, es.url], [0, 'https://app.slack.com/events']);
  await new Promise((r) => { es.onerror = r; });
  assertEq(es.readyState, EventSource.CLOSED);
});
test('globals are non-enumerable and replaceable', () => {
  for (const k of ['fetch', 'URL', 'Headers', 'localStorage', 'navigator', 'crypto', 'TextEncoder', 'XMLHttpRequest', 'AbortSignal']) {
    const d = Object.getOwnPropertyDescriptor(globalThis, k);
    assert(d && !d.enumerable && d.configurable, k);
  }
  assert(!Object.keys(globalThis).includes('fetch'));
  const orig = window.fetch;
  window.fetch = () => 'patched';
  assertEq(fetch(), 'patched');
  window.fetch = orig;
  assertEq(typeof self.atob, 'function');
});
