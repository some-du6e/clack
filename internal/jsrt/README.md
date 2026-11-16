# internal/jsrt

Clack's JavaScript runtime for Taut plugins. The API contract is in `docs/PLUGINS.md`; this file is for maintainers.

## Layout

| path | what |
|---|---|
| `jsrt.go`, `loop.go`, `value.go`, `react.go` | the Go API: `Runtime` (one JS goroutine, task queue, timer heap), `Value`/`Ctx`, React roots, events |
| `engine.go`, `bytecode.go` | wazero host for the engine wasm, the handle-based ABI, the boot bytecode cache |
| `boot.go` | boot sequence, private host functions (`__jrh`: timers, fetch, storage, digest, clipboard, commit…) |
| `csrc/glue.c`, `csrc/build.sh` | the C side of the ABI around QuickJS-NG, built to `jsrt.wasm` |
| `csrc/quickjs-*.patch` | Clack's changes to QuickJS-NG: interrupt polls in the generic array methods, yields in its native loops |
| `diag.go` | `Snapshots` (what each runtime is doing, for freeze reports) and the collector's timing |
| `jsrt.wasm` | QuickJS-NG v0.18 + glue, wasm32-wasip1 reactor (committed; rebuilt by CI) |
| `js/env.js` | event loop APIs, console, Event/EventTarget, DOMException, structuredClone, small ES extras |
| `js/web.js` | URL, encoding, Blob/File/FormData, streams, fetch/XHR, Abort*, crypto, Storage, BroadcastChannel, locks, navigator, location |
| `js/intl.js` | `Intl` (QuickJS-NG has none) and the `toLocale*` methods |
| `js/dom.js` | the DOM subset, events with capture/bubble, observers, selectors, innerHTML, the host-tree serializer |
| `js/react-host.js` | react-reconciler host config, `__clack` roots, native components, ReactDOM |
| `vendor/` | unmodified React 19.3.1 / react-reconciler 0.34.0 / scheduler 0.28.0 production builds (MIT) |
| `enginecmp/` | separate module comparing QuickJS-NG/wazero with goja (CI only) |
| `testdata/*.test.js` | JS test suites run by `TestJSSuites` |

Boot order: env → web → intl → dom → React (CommonJS via `__jr.require`) → react-host. Each boot script is compiled to QuickJS bytecode once and cached in memory and under `Options.CacheDir/jsrt-bc` (keyed by the engine hash + script, SHA-256 checked).

## ABI in one paragraph

JS values cross as `u32` handles into a table in wasm memory (0 = undefined; bit 31 on a returned handle means "exception, the rest is the thrown value"). Go owns every handle it receives, wrapped in a GC-tracked `Value` (`runtime.AddCleanup` queues the release on the JS thread). Host functions are `JS_NewCFunctionData` with the Go id in `magic`, plus a `HostRef` object whose finalizer calls `host_free` so Go drops the closure when JS collects the function. Imports: `host_call`, `host_interrupt` (deadline per task), `host_module` (ES module source by name), `host_rejection`, `host_free`, `host_tz_offset`, `host_yield` (a no-op; see Threads) and `host_gc` (the cycle collector starts and ends).

## Threads

The engine is one wasm instance: its stack pointer, malloc and the handle table are process-wide state of that instance, so it must never be entered from two goroutines at once (that corrupts it and traps later, anywhere: "out of bounds memory access" in whatever call comes next). Everything runs on the JS thread: Do, Post, host functions, Root callbacks, GC cleanups (queued as tasks). A `Value` made there is used there, with no check on the hot path. The Values `Runtime.Eval`, `EvalCached` and `Import` return to the caller's goroutine are *remote*: they and the Values their methods return can be used from any goroutine, and off the JS thread each method runs through `Do` (`on` in value.go). `fmt.Sprint(v)` calls `v.String()`, so a JS-thread Value printed off the thread is the same bug. `go test -race` flags it (`engine.call`'s stack pool).

Go can't stop the JS thread while it runs wasm: wazero's compiled code has no safe points, so the goroutine stops only when it calls the host. Every Go stop-the-world (each GC cycle, `runtime.ReadMemStats`, `runtime.Stack`) waits for that, with every other goroutine, the UI's included, paused. Bytecode calls `host_interrupt` every 10k steps. QuickJS's own C loops call nothing, so `csrc/quickjs-yield.patch` makes them call `host_yield` every ~10k steps (`js_yield_tick`): the cycle collector's passes (per object, array element, Map record and property), every allocation, JSON parse and stringify (per value), string quoting, `join` and comparator-less `sort`. That keeps the wait in milliseconds instead of the whole operation: `TestStopTheWorldBounded` runs each with the yields off and on, and on the runners the longest stop-the-world went from 57 ms to 0.12 ms for a collection over 1M live objects, 71 ms to 0.12 ms freeing 1M, 497 ms to 1.2 ms for `JSON.stringify` of 200k records, 326 ms to 4.5 ms for `JSON.parse` of them and 172 ms to 9.4 ms sorting 300k strings (jsrt run 43793413362). React renders cost the same (`BenchmarkReactUpdate` 3.22 ms against 3.33 ms on the previous engine, same runner). The yields don't throw, so they change nothing a task sees; `jr_set_yield` changes their spacing (0 turns them off, for measuring). Clack's own code avoids stop-the-world calls where it can (cmd/clack/trim.go reads runtime/metrics, not ReadMemStats).

## Limits (plugins are untrusted code)

- CPU: every task (a `Do`, a timer, an event, and the microtasks after it) has `Options.Timeout` (5 s), on the monotonic clock (time asleep doesn't count, so a task running when the lid closed isn't over its deadline on wake); QuickJS's interrupt handler (`host_interrupt`) throws an uncatchable error past it. QuickJS polls it in bytecode, regexps and (with `csrc/quickjs-interrupt.patch`) the generic array methods, so `Array.prototype.reverse.call({length: 2**53-1})` stops too. For anything that still never polls, a watchdog gives the Runtime up once a task is `max(Timeout, 1 s)` past its deadline: it closes, `Done` fires, callers get `ErrClosed`, and Taut starts a fresh one (after 3 crashes in 5 minutes it stops the plugins instead). The stuck goroutine can't be stopped from outside the wasm. wazero's `WithCloseOnContextDone` could, but made React renders 2–3× slower (a goroutine per call and a check per loop iteration).
- Memory: QuickJS's heap limit (`Options.MemoryLimit`, 512 MiB) makes allocations throw; the wasm memory is capped at the limit + 512 MiB (`WithMemoryLimitPages`). `fetch` bodies are capped at 128 MiB.
- No WASI filesystem, env, args or stdio (wazero's defaults: nothing preopened, output discarded); only clocks and crypto random. `fetch` goes to http(s) only, through `Options.Fetch` (the default, and Taut's, add no cookies or tokens).
- A Go panic in a host function becomes a JS exception; an engine trap closes the Runtime instead of the process.

## Rebuilding

- Engine: edit `csrc/`, push, and dispatch the `jsrt` workflow (`gh workflow run jsrt.yml --ref <branch>`); its `wasm` job builds `jsrt.wasm` (wasi-sdk 27, QuickJS-NG `QJS_VERSION`, `csrc/quickjs-*.patch` applied in `build.sh`'s order) and uploads the `jsrt-wasm` artifact. Download it (`gh run download <id> -n jsrt-wasm`), commit it, and rebuild `assets.pack` (the engine is embedded from it). New exports should be optional in `engine.go` so Go keeps working with the previous wasm; new imports need their host function in `engine.go` (an older wasm just doesn't import it).
- React: download the npm tarballs, copy the same `cjs/*.production.js` files and licenses into `vendor/`, bump `vendor/README.md`.
- Never build or test locally on the shared box; the `jsrt` workflow runs the tests (`test`), loads all 40 Taut plugins built with taut's own `build:user-plugin` (`taut-plugins`), and on `workflow_dispatch` with `bench=true` runs the benchmarks, the render profile and the goja comparison (`bench`).
- `CLACK_JSRT_WASM=/path/to/jsrt.wasm` swaps the engine binary (for benchmarking candidate builds).

## Numbers (GitHub Actions ubuntu-latest, 4 vCPU)

| | |
|---|---|
| first `New` in a process, cold compile cache | 350–650 ms (wazero compiles 1 MB of wasm; cached on disk with `CacheDir`) |
| `New` with compiled code + bytecode cached | 20–30 ms (instantiate 10–15 ms, environment 6–8 ms, React 4 ms) |
| JS heap after boot | 3 MiB |
| Go→JS call (`Value.Call`) | 1.8–2.3 µs |
| JS→Go host call | 1.1–1.5 µs |
| `Runtime.Do` round trip from another goroutine | 9–13 µs |
| React: update 1 row of a 100-row list (event → render → commit → host tree) | 2.3–3.2 ms |
| React: render+commit+serialize 100 `<li>` (minimal host config, same in both engines) | QuickJS-NG 2.7–4.0 ms, goja 2.9–4.0 ms |
| CPU loop + 200k-object sort | QuickJS-NG 0.64–0.87 s, goja 0.92–1.26 s |

goja lacks async generators, WeakRef, FinalizationRegistry, RegExp `/d` and `/v`, `Array.fromAsync` and ES modules/dynamic `import()`; Taut plugins are esbuild output with no downleveling, so QuickJS-NG (ES2024+, only `Intl` missing, which `js/intl.js` provides) is the engine. `wasm-opt -O3` on the clang `-O2 -flto` build measured no gain.
