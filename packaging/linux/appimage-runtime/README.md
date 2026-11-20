The runtime at the front of Clack's AppImages: AppImage's [type2-runtime](https://github.com/AppImage/type2-runtime),
the build of commit `fedfad51acc2860b2916a1eef32ba4a500ff31ef` (2026-10-19). That's the "continuous" release's
`runtime-x86_64` and `runtime-aarch64`, which every Clack AppImage up to this change shipped, since appimagetool
downloads "continuous" when not given `--runtime-file`. release.yml now passes these copies instead, so a release
no longer takes whatever "continuous" is that day. Their sha256 are GitHub's recorded digests for those assets.

```sha256
617aa77314d57c8b8e4fae9c3f0b35af7b30d5529cb69e98f64f92510ed98a46  runtime-x86_64
9a72a744d221576c9347ec074243acc5c3c7568896b12714e0e82e9f0bf47900  runtime-aarch64
```

The runtime is statically linked. Its own help text says it contains code from the runtime itself, musl libc,
libfuse, squashfuse, libzstd and zlib; its strings show mimalloc too. Each one's licence, at the version the
runtime names or its build pins (type2-runtime's `scripts/common/install-dependencies.sh` and Alpine 3.21):

| file | what | licence |
|---|---|---|
| `LICENSE.runtime` | type2-runtime | MIT |
| `LICENSE.musl` | musl 1.2.5 | MIT |
| `LICENSE.libfuse`, `LICENSE.libfuse-LGPL2.txt` | libfuse 3.15.0's library | LGPL-2.1 |
| `LICENSE.squashfuse` | squashfuse 0.5.2 | BSD-2-Clause |
| `LICENSE.zstd` | zstd 1.5.6 | BSD-3-Clause (its BSD option) |
| `LICENSE.zlib` | zlib 1.3.2 | zlib |
| `LICENSE.mimalloc` | mimalloc 2.1.7 | MIT |

release.yml appends them to the AppImage's `THIRD_PARTY_LICENSES.txt`. libfuse is LGPL-2.1 and statically linked,
so the AppImage's licence file names where both sources are: type2-runtime at the commit above, which builds the
runtime from libfuse 3.15.0's release tarball (https://github.com/libfuse/libfuse/releases/tag/fuse-3.15.0).

supply-chain checks the runtimes against the sha256 above, and diffs each licence text against its upstream file
(the `upstream` block in `third_party/licenses/README.md`).
