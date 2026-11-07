#!/bin/sh
# Builds ../jsrt.wasm from QuickJS-NG + glue.c with wasi-sdk.
# Usage: WASI_SDK=/opt/wasi-sdk QJS=/path/to/quickjs-ng ./build.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
CC="$WASI_SDK/bin/clang"
"$CC" --target=wasm32-wasip1 --sysroot="$WASI_SDK/share/wasi-sysroot" \
	-O2 -flto -D_GNU_SOURCE -DNDEBUG -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS \
	-I"$QJS" -mexec-model=reactor \
	-Wno-unused-parameter -Wno-sign-compare -Wno-implicit-fallthrough \
	"$QJS/dtoa.c" "$QJS/libregexp.c" "$QJS/libunicode.c" "$QJS/quickjs.c" "$here/glue.c" \
	-lwasi-emulated-signal -lwasi-emulated-process-clocks \
	-Wl,-z,stack-size=8388608 -Wl,--strip-all \
	-o "$here/../jsrt.wasm"
ls -l "$here/../jsrt.wasm"
