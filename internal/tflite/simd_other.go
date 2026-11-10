//go:build !amd64

package tflite

const useAVX2 = false

func gemvAVX2(dst, a, w, bias *float32, k, co, nblk int) { panic("no AVX2") }
func maddAVX2(acc, a, b *float32, n int)                 { panic("no AVX2") }
