package tflite

import "os"

// noSIMD (CLACK_TFLITE_NOSIMD=1) keeps the pure-Go loops, for tests that
// compare both.
var noSIMD = os.Getenv("CLACK_TFLITE_NOSIMD") != ""
