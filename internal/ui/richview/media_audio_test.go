package richview

// An audio card that Options.Media plays (videoplayer's audio player,
// Slack's FileAudio): the pillow is a media slot, the doc draws its frame
// and no static player under the live one; a press on the player is the
// player's and never opens the file, while the card's hover FileActions
// (Download, Share, More) draw over the player and stay the doc's.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// audioMedia plays every audio file, counting the presses its slots get.
type audioMedia struct {
	presses int
	slots   []MediaSlot
}

func (m *audioMedia) MediaTile(f *rt.File, files int, overflow bool) bool { return f.IsAudio }

func (m *audioMedia) LayoutMedia(gtx layout.Context, s MediaSlot) layout.Dimensions {
	m.slots = append(m.slots, s)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: m, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			m.presses++
		}
	}
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, m)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func TestAudioCardSlot(t *testing.T) {
	uipref.Reset()
	f := &rt.File{ID: "F7", Mode: "hosted", IsAudio: true, Name: "a.mp3", Type: "mp3", DurationMS: 154000, Size: 900 << 10, Permalink: "https://x.test/F7"}
	h := &actHandler{}
	m := &audioMedia{}
	hs := &harness{st: newTestStyle(), v: New(doc(fileBlock(f)), Options{Media: m}), h: h, w: 600}
	hs.frame()
	if len(hs.v.d.media) != 1 || len(hs.v.d.bars) != 1 {
		t.Fatalf("%d media slots, %d bars; want the card as one of each", len(hs.v.d.media), len(hs.v.d.bars))
	}
	slot := hs.v.d.media[0].r
	if card := hs.v.d.bars[0].card; card != slot {
		t.Errorf("the FileActions' card %v, the slot %v", card, slot)
	}
	if slot.Dx() != 426 || slot.Dy() != 92 {
		t.Errorf("slot %v, want Slack's 426×92 card", slot)
	}
	for _, b := range hs.v.d.boxes {
		if b.kind == bCircle || b.kind == bPlay {
			t.Errorf("the doc drew a static play button %v under the player", b.r)
		}
	}
	if len(m.slots) == 0 || m.slots[len(m.slots)-1].File != f {
		t.Fatal("Media drew no slot for the card")
	}

	// a press on the player is the player's, not the file's
	pp := f32.Pt(float32(slot.Min.X+30), float32(slot.Min.Y+30))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pp})
	hs.send(press(pp, time.Second), release(pp, time.Second+20*time.Millisecond))
	if m.presses != 1 {
		t.Errorf("the player got %d presses, want 1", m.presses)
	}
	if len(h.recHandler.files) != 0 {
		t.Error("a press on the player opened the file")
	}

	// hovered, the FileActions show and take their own clicks
	rs := hs.v.FileActionRects()
	if len(rs) == 0 {
		t.Fatal("no FileActions while the card is hovered")
	}
	last := rs[len(rs)-1]
	p := rectCenter(last.R)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, 2*time.Second), release(p, 2*time.Second+20*time.Millisecond))
	if len(h.acts) != 1 || h.acts[0] != last.Action || h.files[0] != f {
		t.Errorf("actions %v, want %v on the card's file", h.acts, last.Action)
	}
	if len(h.recHandler.files) != 0 {
		t.Error("a FileActions button opened the file")
	}

	// without Media the same card is the static one, and opens the file
	h2 := &actHandler{}
	hs2 := newHarness(doc(fileBlock(f)), h2)
	hs2.w = 600
	hs2.frame()
	if len(hs2.v.d.media) != 0 {
		t.Errorf("%d media slots with no Media", len(hs2.v.d.media))
	}
	card := hs2.v.d.bars[0].card
	cp := f32.Pt(float32(card.Min.X+30), float32(card.Min.Y+30))
	hs2.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: cp})
	hs2.send(press(cp, time.Second), release(cp, time.Second+20*time.Millisecond))
	if len(h2.recHandler.files) != 1 {
		t.Errorf("the static card's click opened %d files, want 1", len(h2.recHandler.files))
	}
}

// A voice clip's flat row is a slot too, 426×60, with no FileActions
// (MessageFile shows none for an audio message).
func TestAudioClipSlot(t *testing.T) {
	uipref.Reset()
	f := &rt.File{ID: "F8", Mode: "hosted", IsAudio: true, Subtype: "slack_audio", Name: "audio_message.webm", DurationMS: 5000, Waveform: []int{10, 50, 90, 40}}
	m := &audioMedia{}
	hs := &harness{st: newTestStyle(), v: New(doc(fileBlock(f)), Options{Media: m, CompactFiles: true}), h: &actHandler{}, w: 600}
	hs.frame()
	if len(hs.v.d.media) != 1 {
		t.Fatalf("%d media slots", len(hs.v.d.media))
	}
	if r := hs.v.d.media[0].r; r != image.Rect(0, 0, 426, 60) {
		t.Errorf("the clip's slot %v, want (0,0)-(426,60)", r)
	}
	if len(hs.v.d.bars) != 0 {
		t.Errorf("%d FileActions on a voice clip", len(hs.v.d.bars))
	}
	for _, b := range hs.v.d.boxes {
		if b.kind == bCircle || b.kind == bPlay {
			t.Errorf("the doc drew a static play button %v under the player", b.r)
		}
	}
}
