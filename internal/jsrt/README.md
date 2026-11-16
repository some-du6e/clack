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

Go can't stop the JS thread while it runs wasm: wazero's compiled code has no safe points, so the goroutine stops only when it calls the host. Every Go stop-the-world (each GC cycle, `runtime.ReadMemStats`, `runtime.Stack`) waits for that, with every other goroutine, the UI's included, paused. Bytecode calls `host_interrupt` every 10k steps. QuickJS's own C loops call nothing, so `csrc/quickjs-yield.patch` makes them call `host_yield` every ~10k ticks (`js_yield_tick`). A tick is one object visited, one allocation or one array element; a loop over raw memory ticks once per 16 elements or 64 bytes, so 10k ticks is a millisecond or less. What ticks:

- the cycle collector's passes (per object, array element, Map record and property) and every allocation
- JSON: parse and stringify per value, string quoting per 16 characters (counted apart from the index, which steps two over a surrogate pair), one long string value per 16 bytes, and the UTF-8 copy of the input that `JSON.parse` (and every string Go reads) starts with
- arrays: the C side's element accessors (`JS_GetPropertyInt64` and the like: every builtin's generic loop, such as `sort`'s collect and write-back, `concat`, `from`, `flat`, `fill`, `splice`), fast-array appends, and the fast arrays' own loops (`indexOf`, `lastIndexOf`, `includes`, `reverse`, `slice`, `with`, `toReversed`, `toSpliced`, `toSorted`, spread, `shift`, `copyWithin`, truncating `length`, releasing the elements)
- typed arrays and ArrayBuffers: `sort` with and without a comparator, `fill`, `indexOf`/`lastIndexOf`/`includes`, `reverse`, `slice`, `set`, `copyWithin`, `with`, and their copies, zeroing and searches in 64 KiB pieces (a new ArrayBuffer too, which calloc zeroed in one go)
- strings: `indexOf`, `lastIndexOf`, `includes`, `startsWith`, `endsWith`, `split` and `replace` with a string, `repeat`, `padStart`/`padEnd`, `toLowerCase`/`toUpperCase`, appending one long string to a StringBuffer (`JSON.stringify`'s output, `join`), and flattening a rope
- one big object's property table: `Object.keys`/`values`/`entries`, `for-in`, `Object.assign` and `{...o}`, freeing, rehashing and compacting it

`TestStopTheWorldBounded` runs each case with the yields off and on while other goroutines stop the world every 2 ms; the longest stop-the-world on the runners (the jsrt workflow's stw step: freeze-stw's engine in run 43809504063, this one in run 43811552844):

| case | yields off | freeze-stw's yields | now |
|---|---|---|---|
| QuickJS GC over 1M live objects in cycles | 73 ms | 0.20 ms | 0.28 ms |
| QuickJS GC freeing 1M objects in cycles | 92 ms | 0.24 ms | 0.15 ms |
| QuickJS GC over a 1M-entry Map | 72 ms | 0.22 ms | 0.23 ms |
| `JSON.stringify` of 200k records | 569 ms | 1.6 ms | 0.42 ms |
| `JSON.parse` of 200k records | 362 ms | 10 ms | 0.60 ms |
| `sort` of 300k strings | 222 ms | 16 ms | 0.47 ms |
| `JSON.stringify` of 15M emoji after one ASCII character | 322 ms | 354 ms | 3.7 ms |
| `sort` of 3M strings | 3070 ms | 205 ms | 0.81 ms |
| `TypedArray.prototype.sort` of 2M doubles | 519 ms | 601 ms | 1.1 ms |
| `indexOf`, `lastIndexOf`, `includes`, `reverse`, `with` over 10M elements | 229 ms | 543 ms | 0.73 ms |
| typed-array `fill`, `indexOf`, `lastIndexOf`, `includes`, `reverse`, `slice` over 10M doubles, 4 times | 505 ms | 274 ms | 0.72 ms |
| string `indexOf`, `lastIndexOf`, `includes`, `repeat` over 20M characters | 1050 ms | 1213 ms | 1.0 ms |
| `Object.keys` twice and `for-in` over 2M keys | 1299 ms | 1156 ms | 5.2 ms |

What still runs as one stretch: a copy inside the allocator (realloc growing a big array, string buffer or property table; the 3.7 ms and 5.2 ms above are those), turning a host string into a JS string (cutils.h's UTF-8 decoding), libunicode's `normalize` and `localeCompare`, BigInt arithmetic on huge values, compiling a regexp, and wazero growing the wasm memory (a Go memmove of all of it; the test notes when that happens). Running regexps poll `host_interrupt`. React renders cost about the same: `BenchmarkReactUpdate` 2.39 ms against 2.29 ms on the engine before any yields and `BenchmarkReactRender100` 6.65 ms against 6.38 ms (run 43811552844, same runner; +1.7% in run 43810548894, and freeze-stw's engine measured 3% faster than the old one, so this is within the runners' noise). The yields don't throw, so they change nothing a task sees; `jr_set_yield` changes their spacing (0 turns them off, for measuring). Clack's own code avoids stop-the-world calls where it can (cmd/clack/trim.go reads runtime/metrics, not ReadMemStats).

## Limits (plugins are untrusted code)

- CPU: every task (a `Do`, a timer, an event, and the microtasks after it) has `Options.Timeout` (5 s), on the awake clock (`internal/awake`): time asleep or hibernating doesn't count, so a task running when the lid closed isn't over its deadline on wake. On Linux (CLOCK_MONOTONIC) and macOS (mach_absolute_time) that is Go's monotonic clock, which stops during sleep; on Windows Go's monotonic clock reads InterruptTime, which goes on through sleep and hibernation, so the clock there is QueryUnbiasedInterruptTime (the same counter without them). The boot (the environment and React, before the first task) has no deadline; freeze reports name it while it runs. QuickJS's interrupt handler (`host_interrupt`) throws an uncatchable error past it. QuickJS polls it in bytecode, regexps and (with `csrc/quickjs-interrupt.patch`) the generic array methods, so `Array.prototype.reverse.call({length: 2**53-1})` stops too. For anything that still never polls, a watchdog gives the Runtime up once a task is `max(Timeout, 1 s)` past its deadline: it closes, `Done` fires, callers get `ErrClosed`, and Taut starts a fresh one (after 3 crashes in 5 minutes it stops the plugins instead). The stuck goroutine can't be stopped from outside the wasm. wazero's `WithCloseOnContextDone` could, but made React renders 2–3× slower (a goroutine per call and a check per loop iteration).
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
