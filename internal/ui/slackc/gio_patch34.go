//go:build !stockgio

package slackc

import (
	"gioui.org/io/event"
	"gioui.org/io/input"
)

// The focus answers of Gio patch 34 (third_party/gioui.org/CLACK_PATCHES.md):
// the focused tag, whether a tag can keep the focus (as of the last frame,
// and asked in this one), and whether one was drawn inside another
// (layers.go).

func focusTag(s input.Source) event.Tag               { return s.FocusTag() }
func canFocus(s input.Source, tag event.Tag) bool     { return s.CanFocus(tag) }
func askedFocus(s input.Source, tag event.Tag) bool   { return s.AskedFocus(tag) }
func within(s input.Source, tag, area event.Tag) bool { return s.Within(tag, area) }
