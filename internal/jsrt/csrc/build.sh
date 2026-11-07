#!/bin/sh
# Builds ../jsrt.wasm from QuickJS-NG + glue.c with wasi-sdk.
# Usage: WASI_SDK=/opt/wasi-sdk QJS=/path/to/quickjs-ng ./build.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
CC="$WASI_SDK/bin/clang"
# quickjs.c with csrc/quickjs-interrupt.patch applied (a copy; $QJS stays
# untouched): generic array methods poll the interrupt handler, so a plugin
# looping over {length: 2**53 - 1} is stopped by the task deadline.
patched="$here/.build"
rm -rf "$patched" && mkdir -p "$patched"
cp "$QJS/quickjs.c" "$patched/quickjs.c"
patch -s -p1 -d "$patched" < "$here/quickjs-interrupt.patch"
"$CC" --target=wasm32-wasip1 --sysroot="$WASI_SDK/share/wasi-sysroot" \
	-O2 -flto -D_GNU_SOURCE -DNDEBUG -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS \
	-I"$QJS" -mexec-model=reactor \
	-Wno-unused-parameter -Wno-sign-compare -Wno-implicit-fallthrough \
	"$QJS/dtoa.c" "$QJS/libregexp.c" "$QJS/libunicode.c" "$patched/quickjs.c" "$here/glue.c" \
	-lwasi-emulated-signal -lwasi-emulated-process-clocks \
	-Wl,-z,stack-size=8388608 -Wl,--strip-all \
	-o "$here/../jsrt.wasm"
rm -rf "$patched"
ls -l "$here/../jsrt.wasm"
