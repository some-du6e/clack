Licence texts that third-party code Clack links needs but whose module doesn't carry as a file:
code a module copied from another project, ports and transpilations. `tools/licenses.py` adds
each to the builds whose compiled files call for it (its `EXTRA` table) in `THIRD_PARTY_LICENSES.txt`.

| file | upstream | covers |
|---|---|---|
| `quic-go.LICENSE` | quic-go/quic-go `LICENSE` (MIT) | uTLS's `internal/quicvarint` and `protocol`, copied from quic-go ("governed by a MIT-style license that can be found in the LICENSE file of the quic-go repository") |
| `FiniteStateEntropy.LICENSE` | Cyan4973/FiniteStateEntropy `LICENSE` (BSD-2-Clause) | klauspost/compress's `fse`, `huff0` and `zstd`, "based on work Copyright (c) 2013, Yann Collet" |
| `tdewolff-canvas.LICENSE.md` | tdewolff/canvas `LICENSE.md` (MIT) | Gio's `internal/stroke`, whose algorithms were "extracted, adapted from" tdewolff/canvas |
| `libopus.COPYING` | xiph/opus `COPYING` at v1.6.1 (BSD-3-Clause) | opus-go, which is libopus 1.6.1 transpiled to Go |

Notices written out in full in a source file's own header (fontconfig's in typesetting's
`fontscan/langset_gen.go`, wayland-protocols' in Gio's `app/wayland_*.c`, Psiphon's in uTLS's
`u_prng.go`) aren't copied here: licenses.py takes them from that header, so they can't drift.

The `supply-chain` workflow fetches each file at the commit below (`path=copy`, copies relative
to this directory) and diffs it against its copy:

```upstream
quic-go/quic-go 4cfc2ea94ee053d181091466a1b5a93d8e1fe060 LICENSE=quic-go.LICENSE
Cyan4973/FiniteStateEntropy 9f30e0918f87bd835fa040d922a208d7b219e50b LICENSE=FiniteStateEntropy.LICENSE
tdewolff/canvas 891200a8cf2be9ef1245cdd063fbfe17d68e8aec LICENSE.md=tdewolff-canvas.LICENSE.md
xiph/opus 22244de5a79bd1d6d623c32e72bf1954b56235be COPYING=libopus.COPYING
iamcal/emoji-data e77759c296ed37beb0dbd63908b43d8619509c6d LICENSE=../../internal/emoji/LICENSE-emoji-data.txt
```
