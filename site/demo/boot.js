// Starts the web demo: reads build.json (the site workflow writes it next
// to main.wasm, with its size), fetches main.wasm with a progress bar,
// runs it with wasm_exec.js, and hides the card once Clack draws its first
// frame. clackDemo is how the program talks to the page (cmd/clack-web):
// status, ready, failed and ended; ?probe asks it for the CI test's hooks.
// On a phone, or when the browser asks to save data, it waits for a tap
// before the big download.
(() => {
  const $ = (id) => document.getElementById(id);
  const card = $('card'), bar = $('bar'), go = $('go'), again = $('again');
  const mb = (n) => (n / 1048576).toFixed(1);
  let stopped = false;

  const show = (text) => { $('state').textContent = text; };
  const stop = (text) => {
    if (stopped) return;
    stopped = true;
    document.documentElement.classList.remove('ready');
    card.classList.add('stopped');
    go.hidden = true;
    again.hidden = false;
    show(text);
  };
  const demo = globalThis.clackDemo = {
    probe: new URLSearchParams(location.search).has('probe'),
    status: show,
    ready() { document.documentElement.classList.add('ready'); },
    failed: (e) => stop(`The demo stopped: ${e}`),
    ended: () => stop('The demo has ended.'),
  };
  again.addEventListener('click', () => location.reload());

  async function run(size) {
    go.hidden = true;
    show(`Downloading Clack · ${mb(size)} MB`);
    const res = await fetch('main.wasm');
    if (!res.ok || !res.body) throw new Error(`main.wasm: HTTP ${res.status}`);
    // what arrives, decompressed, against the size build.json gave
    let got = 0;
    const counted = res.body.pipeThrough(new TransformStream({
      transform(chunk, c) {
        got += chunk.byteLength;
        bar.value = Math.min(got / size, 1);
        show(`Downloading Clack · ${mb(got)} of ${mb(size)} MB`);
        c.enqueue(chunk);
      },
    }));
    const runtime = new Go();
    runtime.argv = ['clack-web'];
    runtime.env = { HOME: '/home/demo', USER: 'demo' };
    const { instance } = await WebAssembly.instantiateStreaming(
      new Response(counted, { headers: { 'Content-Type': 'application/wasm' } }), runtime.importObject);
    bar.removeAttribute('value'); // indeterminate while it starts
    show('Starting Clack');
    await runtime.run(instance);
    demo.ended();
  }

  async function init() {
    if (typeof WebAssembly !== 'object' || typeof WebAssembly.instantiateStreaming !== 'function' || typeof TransformStream !== 'function') {
      stop('This browser can’t run the demo: it needs WebAssembly.');
      return;
    }
    const info = await fetch('build.json', { cache: 'no-cache' });
    if (!info.ok) throw new Error('this copy of the site was built without it');
    const { wasm: size } = await info.json();
    const small = matchMedia('(max-width: 767px)').matches;
    const save = navigator.connection && navigator.connection.saveData;
    if (!small && !save) return run(size);
    show(small ? 'Clack is a desktop app, so the demo wants a wide window.' : 'Your browser asked to save data.');
    go.textContent = `Load the demo · ${mb(size)} MB`;
    go.hidden = false;
    go.addEventListener('click', () => run(size).catch(fail), { once: true });
  }
  const fail = (e) => stop(`The demo couldn’t start: ${e.message || e}`);
  init().catch(fail);
})();
