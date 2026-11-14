// SPDX-License-Identifier: Unlicense OR MIT

//go:build !windows && (!darwin || ios)

package app

// displays is Displays where windows can't be placed (Clack patch,
// CLACK_PATCHES.md §25): X11 and Wayland windows go where the window
// manager or the compositor puts them, and the mobile platforms and the
// browser have one screen.
func displays() []Display { return nil }
