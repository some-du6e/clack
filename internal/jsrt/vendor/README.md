Unmodified production builds from npm (MIT, Meta Platforms, Inc.; see the LICENSE.* files):

| file | package | sha256 |
|---|---|---|
| react.production.js | react@19.3.1 `cjs/` | `0b5529981841afa1b58c76ba85c29bb681ff5f07cd8cde6a10b967d3915b8e1c` |
| react-jsx-runtime.production.js | react@19.3.1 `cjs/` | `1e46f15002696985e80c61d47aaa30dabb03c954270a690f5f7dfbbacfa9002b` |
| react-reconciler.production.js | react-reconciler@0.34.0 `cjs/` | `41abaf7360ca299162ee0366112e431d365e35a1cee2b2488b9dc382593d1faa` |
| react-reconciler-constants.production.js | react-reconciler@0.34.0 `cjs/` | `413c03d7cb39e4a1474d1dad2faa605bc0e861311bff9fc87a4e43d7dda3e09e` |
| scheduler.production.js | scheduler@0.28.0 `cjs/` | `679adff761d31e9426604f80c0e99be44a3e6f4c6834b0218ecf0312a67c171f` |

They are CommonJS; `vendor.go` wraps each in a factory that env.js's small `require` resolves.

The `supply-chain` workflow checks the files against the hashes below, then downloads each
tarball from registry.npmjs.org, checks its sha512 against the registry's `dist.integrity`
recorded below, and diffs every vendored file (and LICENSE) against the tarball's copy.
Last verified by hand 2026-11-07: all identical.

To update: bump the versions and integrity (`npm view <pkg>@<ver> dist.integrity`) in the `npm`
block, copy the same files out of the new tarballs, and regenerate the `sha256` block with
`sha256sum *.js LICENSE.react* LICENSE.scheduler`.

```npm
react 19.3.1 sha512-QNhWdt7nrgVWWQG58QuGsAR3jv3Hbe4mb4vz7w2csnHavciZJgda1CPNaldF6eVxv90ud3jUXpKlHL69gO38yw== cjs/react.production.js=react.production.js cjs/react-jsx-runtime.production.js=react-jsx-runtime.production.js LICENSE=LICENSE.react
react-reconciler 0.34.0 sha512-eVibvB1fc9j5Qf1KRkiM3GzRPBdjg2P6G6QV2f3P6Eb3OGMxRBhdg2rrUwjbV5JrQaJj8nOa8uajmmJA5tJWjQ== cjs/react-reconciler.production.js=react-reconciler.production.js cjs/react-reconciler-constants.production.js=react-reconciler-constants.production.js LICENSE=LICENSE.react-reconciler
scheduler 0.28.0 sha512-juorfCmIkIw8tT+p5BXSm6PJjQF/ycEYmKyzURCIt/RaZIhL+PulbQ9Yu2z1HdOJDdqDTlxA1+xKBmHXJsczAw== cjs/scheduler.production.js=scheduler.production.js LICENSE=LICENSE.scheduler
```

```sha256
1e46f15002696985e80c61d47aaa30dabb03c954270a690f5f7dfbbacfa9002b  react-jsx-runtime.production.js
0b5529981841afa1b58c76ba85c29bb681ff5f07cd8cde6a10b967d3915b8e1c  react.production.js
413c03d7cb39e4a1474d1dad2faa605bc0e861311bff9fc87a4e43d7dda3e09e  react-reconciler-constants.production.js
41abaf7360ca299162ee0366112e431d365e35a1cee2b2488b9dc382593d1faa  react-reconciler.production.js
679adff761d31e9426604f80c0e99be44a3e6f4c6834b0218ecf0312a67c171f  scheduler.production.js
da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93  LICENSE.react
da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93  LICENSE.react-reconciler
da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93  LICENSE.scheduler
```

## Other third-party code in jsrt

| what | where | license |
|---|---|---|
| QuickJS-NG v0.18.0 (commit `d7215b0ad3fc2c562e8317fd9bca058fd4863804`) | compiled into `../jsrt.wasm` | MIT, `LICENSE.quickjs-ng` |
| wasi-libc from wasi-sdk 27 (tarball sha256 `b7d4d944…23bc2`) | statically linked into `../jsrt.wasm` | MIT / Apache-2.0 WITH LLVM-exception (musl-derived parts MIT) |
| CLDR data via ICU 78 | the generated block in `../js/intl.js` | Unicode License v3, `LICENSE.unicode` |

`../jsrt.wasm` is reproducible: the `jsrt` workflow's `wasm` job rebuilds it from `../csrc/` with
the pinned QuickJS-NG commit and the checksummed wasi-sdk tarball, and fails if the result differs
from the committed file.
