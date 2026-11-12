// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"reflect"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
)

// Clack patch (CLACK_PATCHES.md §11).

func TestFileDropEventsReachWindowEvent(t *testing.T) {
	w := new(Window)
	// decorations drawn by Gio above the content: positions come out in
	// the FrameEvent's coordinates
	w.lastFrame.off = image.Pt(0, 30)
	in := []FileDropEvent{
		{Kind: FileDragEnter, Position: f32.Pt(10, 40), Paths: []string{"/a.png"}},
		{Kind: FileDragOver, Position: f32.Pt(11, 41)},
		{Kind: FileDragOver, Position: f32.Pt(12, 42), Modifiers: key.ModShift},
		{Kind: FileDrop, Position: f32.Pt(13, 43), Paths: []string{"/a.png"}, Modifiers: key.ModShift},
		{Kind: FileDragEnter, Position: f32.Pt(1, 31), Paths: []string{"/b"}},
		{Kind: FileDragLeave},
	}
	for _, e := range in {
		if !w.processEvent(e) {
			t.Fatalf("processEvent(%v) = false", e.Kind)
		}
	}
	want := []FileDropEvent{
		{Kind: FileDragEnter, Position: f32.Pt(10, 10), Paths: []string{"/a.png"}},
		// the two Overs fold into the later one
		{Kind: FileDragOver, Position: f32.Pt(12, 12), Modifiers: key.ModShift},
		{Kind: FileDrop, Position: f32.Pt(13, 13), Paths: []string{"/a.png"}, Modifiers: key.ModShift},
		{Kind: FileDragEnter, Position: f32.Pt(1, 1), Paths: []string{"/b"}},
		{Kind: FileDragLeave},
	}
	var got []FileDropEvent
	for {
		e, ok := w.nextEvent()
		if !ok {
			break
		}
		fe, isDrop := e.(FileDropEvent)
		if !isDrop {
			t.Fatalf("nextEvent: %T, want only FileDropEvents", e)
		}
		got = append(got, fe)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %+v\nwant %+v", got, want)
	}
}

func TestFileDropsComeBeforeTheFrame(t *testing.T) {
	w := new(Window)
	cfg := ConfigEvent{}
	w.coalesced.cfg = &cfg
	w.processEvent(FileDropEvent{Kind: FileDragEnter})
	e, _ := w.nextEvent()
	if _, ok := e.(FileDropEvent); !ok {
		t.Fatalf("first event %T, want the FileDropEvent", e)
	}
	if e, _ := w.nextEvent(); reflect.TypeOf(e) != reflect.TypeOf(ConfigEvent{}) {
		t.Fatalf("second event %T, want the ConfigEvent", e)
	}
}

func TestQueueFileDropFoldsOnlyAdjacentOvers(t *testing.T) {
	var q []FileDropEvent
	for _, k := range []FileDropKind{FileDragEnter, FileDragOver, FileDragOver, FileDragOver, FileDragLeave, FileDragOver} {
		q = queueFileDrop(q, FileDropEvent{Kind: k})
	}
	var kinds []FileDropKind
	for _, e := range q {
		kinds = append(kinds, e.Kind)
	}
	want := []FileDropKind{FileDragEnter, FileDragOver, FileDragLeave, FileDragOver}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
}

func TestParseURIList(t *testing.T) {
	in := "# dragged from Nautilus\r\n" +
		"file:///home/k/Pictures/shot%201.png\r\n" +
		"file://localhost/tmp/a%23b.txt\r\n" +
		"https://example.com/not-a-file\r\n" +
		"file://otherhost/srv/share.txt\r\n" +
		"\r\n" +
		"file:///home/k/caf%C3%A9.pdf"
	got := parseURIList(in)
	want := []string{"/home/k/Pictures/shot 1.png", "/tmp/a#b.txt", "/home/k/café.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseURIList:\n got %q\nwant %q", got, want)
	}
	if got := parseURIList(""); got != nil {
		t.Fatalf("parseURIList(\"\") = %q", got)
	}
}
