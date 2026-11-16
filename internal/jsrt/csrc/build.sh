#!/bin/sh
# Builds ../jsrt.wasm from QuickJS-NG + glue.c with wasi-sdk.
# Usage: WASI_SDK=/opt/wasi-sdk QJS=/path/to/quickjs-ng ./build.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
CC="$WASI_SDK/bin/clang"
# quickjs.c with Clack's patches applied, in this order (a copy; $QJS stays
# untouched):
#   quickjs-interrupt.patch  generic array methods poll the interrupt
#                            handler, so a plugin looping over
#                            {length: 2**53 - 1} is stopped by the task deadline
#   quickjs-yield.patch      the cycle collector, the allocator, JSON and the
#                            array, typed-array, string and property-table
#                            loops call the host every ~10k steps, so a Go
#                            stop-the-world never waits long on the JS thread
patched="$here/.build"
rm -rf "$patched" && mkdir -p "$patched"
cp "$QJS/quickjs.c" "$patched/quickjs.c"
for p in quickjs-interrupt.patch quickjs-yield.patch; do
	patch -s -p1 -d "$patched" < "$here/$p"
done
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
