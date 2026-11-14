// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"

	"gioui.org/io/system"
	"gioui.org/unit"
)

// Clack patch (CLACK_PATCHES.md §22): a window without its OS title bar
// that keeps the OS frame, for Slack's frameless main window on Windows.
// Slack's BrowserWindow is frame: false with titleBarStyle "hidden" and a
// titleBarOverlay: Electron (WinFrameView, WinCaptionButtonContainer) keeps
// the standard window styles, takes the caption into the client area with
// WM_NCCALCSIZE, and draws Windows' minimize, maximize and close buttons
// over the page, whose top nav is the title bar.

// HiddenTitleBar removes the title bar of a decorated window and keeps
// the rest of the OS frame: the resize borders, the drop shadow and the
// rounded corners, Snap, the system menu and the window animations. The
// client area runs to the window's top edge and the program draws the
// caption, declaring its parts with system.ActionInputOp:
//
//   - ActionMove: the caption (dragging moves the window, a double-click
//     maximizes it, a right-click opens the system menu);
//   - ActionMinimize, ActionMaximize or ActionUnmaximize, ActionClose: the
//     caption buttons. The OS knows them for what they are (Windows 11's
//     Snap Layouts flyout opens over the maximize button), and pointer
//     input over them still reaches the program's widgets, which perform
//     the action.
//
// The top edge of a resizable window that is neither maximized nor full
// screen resizes it, as the top of a standard frame does, except over the
// caption buttons.
//
// Only Windows implements it; elsewhere the window keeps its title bar.
// It has no effect on an undecorated window (Decorated(false)).
func HiddenTitleBar(hidden bool) Option {
	return func(_ unit.Metric, cnf *Config) {
		cnf.hiddenTitleBar = hidden
	}
}

// captionPart is what a point in the client area of a window with a hidden
// title bar is to the OS (Windows: a WM_NCHITTEST code).
type captionPart uint8

const (
	captionClient captionPart = iota
	captionMove
	captionMinimize
	captionMaximize // the maximize or restore button
	captionClose
	captionTop
	captionTopLeft
	captionTopRight
)

// captionHit is WinFrameView::NonClientHitTest for a point p of the
// client area (size, px): the caption buttons first, then the top resize
// band (topBand px tall, 0 when the window can't be resized from it,
// with corner px at each end resizing diagonally), then the caption,
// else the client. act and hasAct are the topmost system action area at p
// (ActionAt). A full-screen window is all client.
func captionHit(p, size image.Point, fullscreen bool, topBand, corner int, act system.Action, hasAct bool) captionPart {
	if fullscreen {
		return captionClient
	}
	if hasAct {
		switch {
		case act&system.ActionClose != 0:
			return captionClose
		case act&system.ActionMinimize != 0:
			return captionMinimize
		case act&(system.ActionMaximize|system.ActionUnmaximize) != 0:
			return captionMaximize
		}
	}
	if p.Y < topBand {
		switch {
		case p.X < corner:
			return captionTopLeft
		case p.X >= size.X-corner:
			return captionTopRight
		}
		return captionTop
	}
	if hasAct && act&system.ActionMove != 0 {
		return captionMove
	}
	return captionClient
}
