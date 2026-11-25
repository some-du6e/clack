package core

import "testing"

// Red-tree shims: the new APIs the tests use don't exist on the old code.

var connMaxAttempts = 10

func scaleConnWaits(t *testing.T, f float64) {}
