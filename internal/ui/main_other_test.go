//go:build !darwin

package ui

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	endOfRunReport()
	os.Exit(code)
}
