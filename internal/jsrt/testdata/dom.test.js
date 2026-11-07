// DOM subset (dom.js) and the event loop (env.js).
test('timers order and clear', async () => {
  const seen = [];
  setTimeout(() => seen.push('t10'), 10);
  const id = setTimeout(() => seen.push('never'), 5);
  clearTimeout(id);
  setTimeout(() => seen.push('t0'), 0);
  queueMicrotask(() => seen.push('micro'));
  Promise.resolve().then(() => seen.push('promise'));
  setImmediate(() => seen.push('immediate'));
  let n = 0;
  const iv = setInterval(() => { if (++n === 3) clearInterval(iv); }, 1);
  await sleep(40);
  assertEq(seen.slice(0, 2), ['micro', 'promise']);
  assert(seen.includes('t0') && seen.includes('t10') && seen.includes('immediate') && !seen.includes('never'), seen.join());
  assert(seen.indexOf('t0') < seen.indexOf('t10'));
  assertEq(n, 3);
});

test('requestAnimationFrame and idle callbacks', async () => {
  const t = await new Promise((r) => requestAnimationFrame(r));
  assert(typeof t === 'number' && t > 0);
  const d = await new Promise((r) => requestIdleCallback(r));
  assert(d.timeRemaining() >= 0 && d.didTimeout === false);
});

test('createElement, attributes, classList, dataset, style', () => {
  const d = document.createElement('div');
  d.id = 'x';
  d.className = 'a b';
  d.classList.add('c');
  d.classList.toggle('a');
  d.dataset.itemKey = 'k1';
  d.style.backgroundImage = 'url(x.png)';
  d.style.setProperty('--gap', '4px');
  d.style.width = '10px';
  assertEq(d.className, 'b c');
  assertEq(d.getAttribute('data-item-key'), 'k1');
  assertEq(d.style.getPropertyValue('--gap'), '4px');
  assertEq(d.style.width, '10px');
  assert(d.style.cssText.includes('background-image: url(x.png);'), d.style.cssText);
  d.style.removeProperty('width');
  assertEq(d.style.width, '');
  assert(d instanceof HTMLDivElement && d instanceof HTMLElement && d instanceof Element && d instanceof Node && d instanceof EventTarget);
  assertEq(d.tagName, 'DIV');
});

test('tree ops and selectors', () => {
  const root = document.createElement('section');
  root.innerHTML = '<ul class="list"><li data-item-key="a" class="it first">A</li><li data-item-key="b" class="it">B &amp; C</li><li class="it"><span>x</span></li></ul><p id="p1">para</p>';
  assertEq(root.querySelectorAll('li').length, 3);
  assertEq(root.querySelector('.it.first').textContent, 'A');
  assertEq(root.querySelector('[data-item-key="b"]').textContent, 'B & C');
  assertEq(root.querySelector('ul > li:nth-child(2)').dataset.itemKey, 'b');
  assertEq(root.querySelector('li:last-child span').textContent, 'x');
  assertEq(root.querySelectorAll('li:not(.first)').length, 2);
  assertEq(root.querySelector('ul + p').id, 'p1');
  assertEq(root.querySelector('li:has(span)').className, 'it');
  const span = root.querySelector('span');
  assertEq(span.closest('ul').className, 'list');
  assert(root.contains(span) && !span.contains(root));
  span.remove();
  assertEq(root.querySelectorAll('span').length, 0);
  const frag = document.createDocumentFragment();
  frag.append('t', document.createElement('b'));
  root.prepend(frag);
  assertEq(root.firstChild.textContent, 't');
  assertEq(root.children[0].tagName, 'B');
  assert(root.outerHTML.startsWith('<section>t<b></b><ul class="list">'), root.outerHTML);
  assertThrows(() => root.querySelector('a[[') , 'SyntaxError');
});

test('document structure and body overlays', () => {
  assert(document.body && document.head && document.documentElement.contains(document.body));
  const d = document.createElement('div');
  d.style.position = 'fixed';
  document.body.appendChild(d);
  assert(d.isConnected);
  assertEq(document.querySelector('body > div'), d);
  d.remove();
  assert(!d.isConnected);
  document.documentElement.classList.add('streamer');
  assert(document.documentElement.classList.contains('streamer'));
  document.title = 'Clack';
  assertEq(document.title, 'Clack');
});

test('events: capture, bubble, once, stopImmediatePropagation, signal', () => {
  const outer = document.createElement('div'), inner = document.createElement('span');
  outer.appendChild(inner);
  document.body.appendChild(outer);
  const log = [];
  outer.addEventListener('click', () => log.push('outer-capture'), true);
  outer.addEventListener('click', (e) => log.push('outer-bubble:' + (e.currentTarget === outer) + ':' + (e.target === inner)));
  inner.addEventListener('click', () => log.push('inner-once'), { once: true });
  const ac = new AbortController();
  inner.addEventListener('click', () => log.push('inner-signal'), { signal: ac.signal });
  window.addEventListener('click', () => log.push('window'), { once: true });
  inner.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  assertEq(log, ['outer-capture', 'inner-once', 'inner-signal', 'outer-bubble:true:true', 'window']);
  ac.abort();
  log.length = 0;
  inner.click();
  assertEq(log, ['outer-capture', 'outer-bubble:true:true']);
  const ev = new Event('x', { cancelable: true });
  inner.addEventListener('x', (e) => { e.preventDefault(); e.stopImmediatePropagation(); });
  inner.addEventListener('x', () => log.push('never'));
  assertEq(inner.dispatchEvent(ev), false);
  assert(!log.includes('never'));
  outer.remove();
});

test('MutationObserver', async () => {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const recs = [];
  const mo = new MutationObserver((r) => recs.push(...r));
  mo.observe(host, { childList: true, subtree: true, attributes: true, attributeOldValue: true });
  const c = document.createElement('p');
  host.appendChild(c);
  c.setAttribute('title', 'a');
  c.setAttribute('title', 'b');
  await Promise.resolve();
  await Promise.resolve();
  assertEq(recs.map((r) => r.type), ['childList', 'attributes', 'attributes']);
  assertEq(recs[2].oldValue, 'a');
  assertEq(recs[0].addedNodes[0], c);
  mo.disconnect();
  host.appendChild(document.createElement('i'));
  await Promise.resolve();
  assertEq(recs.length, 3);
  host.remove();
});

test('focus and activeElement', () => {
  const i = document.createElement('input');
  document.body.appendChild(i);
  const log = [];
  i.addEventListener('focus', () => log.push('focus'));
  document.body.addEventListener('focusin', () => log.push('focusin'), { once: true });
  i.focus();
  assertEq(document.activeElement, i);
  assertEq(log, ['focus', 'focusin']);
  i.blur();
  assertEq(document.activeElement, document.body);
  i.remove();
});

test('window, matchMedia, getComputedStyle, canvas', () => {
  assert(window.innerWidth > 0 && window.innerHeight > 0);
  assertEq(window, globalThis);
  const mq = matchMedia('(prefers-color-scheme: dark)');
  assertEq(typeof mq.matches, 'boolean');
  assert(matchMedia('(min-width: 100px)').matches);
  const el = document.createElement('div');
  el.style.fontSize = '13px';
  document.body.appendChild(el);
  const cs = getComputedStyle(el);
  assertEq(cs.fontSize, '13px');
  assert(cs.fontFamily.length > 0);
  const ctx = new OffscreenCanvas(0, 0).getContext('2d');
  ctx.font = '15px Lato';
  const w = ctx.measureText('hello').width;
  assert(w > 10 && w < 60, String(w));
  assert(document.fonts.check('15px Lato'));
  el.remove();
});

test('structuredClone and language extras', () => {
  const src = { a: [1, { b: new Date(0) }], m: new Map([[1, 'x']]), s: new Set([1]), u: new Uint8Array([1, 2]) };
  src.self = src;
  const c = structuredClone(src);
  assert(c !== src && c.self === c && c.m.get(1) === 'x' && c.s.has(1) && c.u[1] === 2 && c.a[1].b.getTime() === 0);
  assertThrows(() => structuredClone({ f() {} }), 'DataCloneError');
  assertEq(RegExp.escape('a+b'), '\\x61\\+b');
  assertEq([...Map.groupBy([1, 2, 3], (x) => x % 2).keys()], [1, 0]);
  const { promise, resolve } = Promise.withResolvers();
  resolve(5);
  return promise.then((v) => assertEq(v, 5));
});

test('console formatting does not throw', () => {
  const o = { a: 1 };
  o.o = o;
  console.log('fmt %s %d %o', 'x', 3, o, [1, 2], new Map([[1, 2]]), new Error('e'));
  console.group('g');
  console.info(document.createElement('div'));
  console.groupEnd();
  console.table([{ a: 1 }]);
});

test('async generators, private methods, error cause', async () => {
  class A { static #n = 0; #inc() { return ++A.#n; } run() { return this.#inc(); } }
  assertEq(new A().run(), 1);
  async function* gen() { yield 1; await sleep(1); yield 2; }
  const out = [];
  for await (const x of gen()) out.push(x);
  assertEq(out, [1, 2]);
  const e = new Error('outer', { cause: 'inner' });
  assertEq(e.cause, 'inner');
  const wr = new WeakRef({});
  assertEq(typeof wr.deref, 'function');
  assertEq(Array.fromAsync ? (await Array.fromAsync(gen())).length : 2, 2);
});
