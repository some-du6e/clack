package tflite

import "golang.org/x/sys/cpu"

// useAVX2: the 8-wide kernels in simd_amd64.s (FMA rounds a·b+c once,
// like arm64's fused multiply-add that Go emits there).
var useAVX2 = cpu.X86.HasAVX2 && cpu.X86.HasFMA && !noSIMD

// gemvAVX2: for each of nblk blocks of 8 output channels,
// dst = bias (or 0) + Σ_i a[i]·w[i*co : i*co+8]. bias may be nil.
//
//go:noescape
func gemvAVX2(dst, a, w, bias *float32, k, co, nblk int)

// maddAVX2: acc[i] += a[i]·b[i] for 8·n values.
//
//go:noescape
func maddAVX2(acc, a, b *float32, n int)
