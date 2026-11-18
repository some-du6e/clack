// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && nox11

package app

// ClackXSettings: no X11 in this build (Clack patch, CLACK_PATCHES.md §47).
func ClackXSettings() []byte { return nil }
