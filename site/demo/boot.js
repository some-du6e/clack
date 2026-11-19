// Starts the web demo: reads build.json (the site workflow writes it with
// the program, tools/demo-pack.mjs), streams the program's parts
// (main.0.wasm, main.1.wasm, …) into one module with a progress bar, runs
// it with wasm_exec.js, and hides the card once Clack draws its first
// frame. clackDemo is how the program talks to the page (cmd/clack-web):
// status, ready, failed and ended; ?probe asks it for the CI test's hooks.
// On a phone, or when the browser asks to save data, it waits for a tap
// before the big download.
(() => {
  const $ = (id) => document.getElementById(id);
  const card = $('card'), bar = $('bar'), go = $('go'), again = $('again');
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

  async function run({ wasm: size, parts }) {
    go.hidden = true;
    show('Downloading Clack');
    // the parts one after another as one stream, counted (decompressed, as
    // they arrive) against the size build.json gave; as a percentage,
    // since what crosses the network depends on the host's compression
    let got = 0, i = 0, reader = null;
    const counted = new ReadableStream({
      async pull(c) {
        for (;;) {
          if (!reader) {
            if (i === parts.length) { c.close(); return; }
            const res = await fetch(parts[i++].file);
            if (!res.ok || !res.body) throw new Error(`${parts[i - 1].file}: HTTP ${res.status}`);
            reader = res.body.getReader();
          }
          const { done, value } = await reader.read();
          if (done) { reader = null; continue; }
          got += value.byteLength;
          bar.value = Math.min(got / size, 1);
          show(`Downloading Clack · ${Math.floor((100 * got) / size)}%`);
          c.enqueue(value);
          return;
        }
      },
    });
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
    if (typeof WebAssembly !== 'object' || typeof WebAssembly.instantiateStreaming !== 'function' || typeof ReadableStream !== 'function') {
      stop('This browser can’t run the demo: it needs WebAssembly.');
      return;
    }
    const info = await fetch('build.json', { cache: 'no-cache' });
    if (!info.ok) throw new Error('this copy of the site was built without it');
    const build = await info.json();
    const small = matchMedia('(max-width: 767px)').matches;
    const save = navigator.connection && navigator.connection.saveData;
    if (!small && !save) return run(build);
    show(small ? 'Clack is a desktop app, so the demo wants a wide window.' : 'Your browser asked to save data.');
    go.hidden = false;
    go.addEventListener('click', () => run(build).catch(fail), { once: true });
  }
  const fail = (e) => stop(`The demo couldn’t start: ${e.message || e}`);
  init().catch(fail);
})();
