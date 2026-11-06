//go:build !linux

package ui

func newNotifier() notifier { return nopNotifier{} }
