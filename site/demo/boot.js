// Starts the web demo: checks that the browser has what Clack needs
// (WebAssembly and WebGL) before anything big downloads, reads the
// manifest tools/demo-pack.mjs put in this page (#build: the program's
// parts under p/<hash>/ and their sizes, so the page and its parts are
// always one build), streams the parts into one module with a progress
// bar, runs it with wasm_exec.js, and hides the card once Clack draws its
// first frame.
// clackDemo is how the program talks to the page (cmd/clack-web): status,
// ready, failed and ended; ?probe asks it for the CI test's hooks. In a
// narrow window or on a touch-only device, or when the browser asks to
// save data, it says how big the download is and waits for a tap.
(() => {
  const $ = (id) => document.getElementById(id);
  const card = $('card'), bar = $('bar'), go = $('go'), again = $('again');
  const mb = (n) => Math.round(n / 1048576);
  let stopped = false;

  // #state is what's on screen; #say is the live region, told less often
  // (each quarter of the download, not every chunk)
  const show = (text, say = true) => {
    $('state').textContent = text;
    if (say) $('say').textContent = text;
  };
  const stop = (text, retry = true) => {
    if (stopped) return;
    stopped = true;
    document.documentElement.classList.remove('ready');
    card.classList.add('stopped');
    go.hidden = true;
    again.hidden = !retry;
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

  // what Clack needs: Go's js/wasm port is plain WebAssembly (MVP), and
  // Gio draws with WebGL 2 or 1
  const missing = () => {
    if (typeof WebAssembly !== 'object' || typeof WebAssembly.instantiateStreaming !== 'function' || typeof ReadableStream !== 'function') return 'WebAssembly';
    const c = document.createElement('canvas');
    if (!(c.getContext('webgl2') || c.getContext('webgl'))) return 'WebGL';
    return '';
  };

  // a part that's gone or isn't the size the manifest says: another deploy
  // landed while this one loaded
  class Updated extends Error {}

  async function run({ wasm: size, brotli, parts }) {
    go.hidden = true;
    show(`Downloading Clack · about ${mb(brotli)} MB`);
    // the parts one after another as one stream, counted (decompressed, as
    // they arrive) against their sizes in the manifest
    let got = 0, i = 0, reader = null, inPart = 0, said = 0;
    const counted = new ReadableStream({
      async pull(c) {
        for (;;) {
          if (!reader) {
            if (i === parts.length) { c.close(); return; }
            const res = await fetch(parts[i].file);
            if (res.status === 404) throw new Updated();
            if (!res.ok || !res.body) throw new Error(`${parts[i].file}: HTTP ${res.status}`);
            reader = res.body.getReader();
            inPart = 0;
          }
          const { done, value } = await reader.read();
          if (done) {
            if (inPart !== parts[i].size) throw new Updated();
            reader = null;
            i++;
            continue;
          }
          got += value.byteLength;
          inPart += value.byteLength;
          if (inPart > parts[i].size) throw new Updated();
          const pct = Math.floor((100 * got) / size);
          bar.value = Math.min(got / size, 1);
          bar.setAttribute('aria-valuetext', `${pct}%`);
          const quarter = Math.floor(pct / 25) * 25;
          show(`Downloading Clack · ${pct}% of about ${mb(brotli)} MB`, quarter > said);
          if (quarter > said) said = quarter;
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
    if (demo.probe) demo.memory = () => instance.exports.mem.buffer.byteLength;
    bar.removeAttribute('value'); // indeterminate while it starts
    bar.removeAttribute('aria-valuetext');
    show('Starting Clack');
    await runtime.run(instance);
    demo.ended();
  }

  async function init() {
    const need = missing();
    if (need) {
      stop(need === 'WebGL'
        ? 'This browser can’t run the demo: it needs WebGL, which is turned off or blocked here.'
        : 'This browser can’t run the demo: it needs WebAssembly.', false);
      return;
    }
    const inline = $('build');
    if (!inline) {
      stop('The demo isn’t in this copy of the site.', false);
      return;
    }
    const build = JSON.parse(inline.textContent);
    // a narrow window, or a touch-only device (phones either way up,
    // tablets); not the screen's size, which a scaled 1080p desktop shares
    // with a tablet
    const small = matchMedia('(max-width: 767px), (pointer: coarse) and (hover: none)').matches;
    const save = navigator.connection && navigator.connection.saveData;
    if (!small && !save) return run(build);
    // memory: the CI browser test's measure, about 136 MB of wasm memory
    // after a message is sent, and 16 MB of JavaScript heap
    show(`${small ? 'Clack is a desktop app, so the demo wants a wide window.' : 'Your browser asked to save data.'} Loading it downloads about ${mb(build.brotli)} MB (${mb(build.wasm)} MB unpacked) and uses about 150 MB of memory.`);
    go.hidden = false;
    go.addEventListener('click', () => run(build).catch(fail), { once: true });
  }
  const fail = (e) => e instanceof Updated
    ? stop('The demo was updated while it loaded. Start again to get the new one.')
    : stop(`The demo couldn’t start: ${e.message || e}`);
  init().catch(fail);
})();
