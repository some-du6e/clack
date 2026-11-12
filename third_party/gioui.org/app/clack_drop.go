// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"net/url"
	"os"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/key"
)

// Clack patch (CLACK_PATCHES.md §11): files dragged onto the window from
// the OS (Explorer, Finder, a file manager).
//
// A drag that carries files sends one FileDragEnter, FileDragOver as the
// pointer moves (and, on Windows and macOS, periodically while it rests),
// then either FileDragLeave (the pointer left the window, or the user
// pressed Esc) or FileDrop. Window.Event returns them in that order, before
// the next FrameEvent. A drag without files (text, a link) sends nothing.
//
// The window always answers the OS with a copy. Gio doesn't redraw for
// these events: an app that draws something for a drag asks for a frame.
type FileDropEvent struct {
	Kind FileDropKind
	// Position is the pointer, in the same coordinates as pointer.Event
	// positions in the FrameEvent's Ops (pixels, decorations excluded).
	Position f32.Point
	// Paths are the dragged files' absolute paths, on FileDragEnter and
	// FileDrop. X11 only learns them on the drop, so its FileDragEnter
	// has none; FileDragOver and FileDragLeave never carry them.
	Paths []string
	// Modifiers are the keys held (Shift drops share at once in Slack).
	Modifiers key.Modifiers
}

// FileDropKind is the stage of an OS file drag.
type FileDropKind uint8

const (
	FileDragEnter FileDropKind = iota
	FileDragOver
	FileDragLeave
	FileDrop
)

func (k FileDropKind) String() string {
	switch k {
	case FileDragEnter:
		return "Enter"
	case FileDragOver:
		return "Over"
	case FileDragLeave:
		return "Leave"
	case FileDrop:
		return "Drop"
	}
	return "FileDropKind(?)"
}

func (FileDropEvent) ImplementsEvent() {}

// queueFileDrop adds e to the events waiting for Window.Event, folding a
// FileDragOver into the FileDragOver before it so a busy drag doesn't
// queue up stale positions.
func queueFileDrop(q []FileDropEvent, e FileDropEvent) []FileDropEvent {
	if n := len(q); n > 0 && e.Kind == FileDragOver && q[n-1].Kind == FileDragOver {
		q[n-1] = e
		return q
	}
	return append(q, e)
}

// parseURIList reads a text/uri-list (RFC 2483), as X11's XDND and
// Wayland's wl_data_offer carry dragged files: one URI per line (CRLF),
// '#' lines are comments. Only file: URIs on this host become paths.
func parseURIList(s string) []string {
	var paths []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" {
			continue
		}
		if u.Host != "" && u.Host != "localhost" {
			if h, err := os.Hostname(); err != nil || !strings.EqualFold(u.Host, h) {
				continue
			}
		}
		if u.Path != "" {
			paths = append(paths, u.Path)
		}
	}
	return paths
}
