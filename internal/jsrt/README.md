# internal/jsrt

Clack's JavaScript runtime for Taut plugins. The API contract is in `docs/PLUGINS.md`; this file is for maintainers.

## Layout

| path | what |
|---|---|
| `jsrt.go`, `loop.go`, `value.go`, `react.go` | the Go API: `Runtime` (one JS goroutine, task queue, timer heap), `Value`/`Ctx`, React roots, events |
| `engine.go`, `bytecode.go` | wazero host for the engine wasm, the handle-based ABI, the boot bytecode cache |
| `boot.go` | boot sequence, private host functions (`__jrh`: timers, fetch, storage, digest, clipboard, commit…) |
| `csrc/glue.c`, `csrc/build.sh` | the C side of the ABI around QuickJS-NG, built to `jsrt.wasm` |
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

JS values cross as `u32` handles into a table in wasm memory (0 = undefined; bit 31 on a returned handle means "exception, the rest is the thrown value"). Go owns every handle it receives, wrapped in a GC-tracked `Value` (`runtime.AddCleanup` queues the release on the JS thread). Host functions are `JS_NewCFunctionData` with the Go id in `magic`, plus a `HostRef` object whose finalizer calls `host_free` so Go drops the closure when JS collects the function. Imports: `host_call`, `host_interrupt` (deadline per task), `host_module` (ES module source by name), `host_rejection`, `host_free`.

## Rebuilding

- Engine: edit `csrc/`, push; the `jsrt` workflow's `wasm` job builds `jsrt.wasm` (wasi-sdk 27, QuickJS-NG `QJS_VERSION`) and uploads the `jsrt-wasm` artifact. Download it (`gh run download <id> -n jsrt-wasm`) and commit it. New exports should be optional in `engine.go` so Go keeps working with the previous wasm.
- React: download the npm tarballs, copy the same `cjs/*.production.js` files and licenses into `vendor/`, bump `vendor/README.md`.
- Never build or test locally on the shared box; the `jsrt` workflow runs the tests (`test`), loads all 40 Taut plugins built with taut's own `build:user-plugin` (`taut-plugins`), and on `workflow_dispatch` with `bench=true` runs the benchmarks, the render profile and the goja comparison (`bench`).
- `CLACK_JSRT_WASM=/path/to/jsrt.wasm` swaps the engine binary (for benchmarking candidate builds).

## Numbers (GitHub Actions ubuntu-latest, 4 vCPU)

| | |
|---|---|
| first `New` in a process, cold compile cache | 350–650 ms (wazero compiles 1 MB of wasm; cached on disk with `CacheDir`) |
| `New` with compiled code + bytecode cached | 20–45 ms (instantiate 9–22 ms, env+web+intl+dom 10–15 ms, React 4–8 ms) |
| JS heap after boot | 3.8 MiB |
| Go→JS call (`Value.Call`) | 1.2–2.3 µs |
| JS→Go host call | 1.1–1.5 µs |
| `Runtime.Do` round trip from another goroutine | 9–13 µs |
| React: update 1 row of a 100-row list (event → render → commit → host tree) | 2.2–3.2 ms |
| React: render+commit+serialize 100 `<li>` (minimal host config, same in both engines) | QuickJS-NG 2.7–4.0 ms, goja 2.9–4.0 ms |
| CPU loop + 200k-object sort | QuickJS-NG 0.64–0.87 s, goja 0.92–1.26 s |

goja lacks async generators, WeakRef, FinalizationRegistry, RegExp `/d` and `/v`, `Array.fromAsync` and ES modules/dynamic `import()`; Taut plugins are esbuild output with no downleveling, so QuickJS-NG (ES2024+; `Intl` comes from `js/intl.js`; 31/31 on the feature list in `enginecmp/`) is the engine. `wasm-opt -O3` on the clang `-O2 -flto` build measured no gain.
