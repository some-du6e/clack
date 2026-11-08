//go:build !linux && !windows && !darwin

package ui

func osPrefersDark() (dark, ok bool) { return false, false }
