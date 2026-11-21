package richview

import (
	"sync/atomic"

	"github.com/some-du6e/clack/internal/richtext"
)

// Message slugs read their label as they draw, as Slack's MessageMention
// (client-boot EwNg) selects getMessageSlugAttributes on every render:
// the linked message and its sender as the store has them now
// (core/msg_slug.go). The ui root installs SlugRead (core's
// MessageSlugLabel, asking: a message the store doesn't hold is fetched
// by id, its sender by users/info), and a layout runs again once a label
// it drew reads differently (slugsChanged): the message arrived, Slack
// said there is none, its sender got a name. Without the hook (tests, a
// span without its channel) the span's own label stands.

// SlugRead is the label of the message slug linking to ts of channel, as
// getMessageSlugAttributes words it now: lock and pre as Span.Lock and
// Span.Pre, off as Span.Off (disabled: "Private message", not a link).
// It must be cheap and never block.
type SlugRead func(channel, ts string) (label string, lock bool, pre int, off bool)

var slugRead atomic.Pointer[SlugRead]

// SetSlugRead installs (or with nil removes) the slug read.
func SetSlugRead(f SlugRead) {
	if f == nil {
		slugRead.Store(nil)
		return
	}
	slugRead.Store(&f)
}

func slugReadFn() SlugRead {
	if p := slugRead.Load(); p != nil {
		return *p
	}
	return nil
}

// slugUse is a message slug a layout drew and the label it read.
type slugUse struct {
	ch, ts, label string
	lock, off     bool
	pre           int
}

// slugShown is sp's label as it draws now: SlugRead's, else the span's.
func slugShown(sp *richtext.Span) (label string, lock bool, pre int, off bool) {
	if f := slugReadFn(); f != nil && sp.Chan != "" && sp.ID != "" {
		return f(sp.Chan, sp.ID)
	}
	return sp.Text, sp.Lock, sp.Pre, sp.Off
}

// slugLabel is slugShown, the read recorded so slugsChanged can tell when
// to lay out again.
func (l *lay) slugLabel(sp *richtext.Span) (label string, lock bool, pre int, off bool) {
	label, lock, pre, off = slugShown(sp)
	if slugReadFn() != nil && sp.Chan != "" && sp.ID != "" {
		l.d.slugs = append(l.d.slugs, slugUse{ch: sp.Chan, ts: sp.ID, label: label, lock: lock, off: off, pre: pre})
	}
	return label, lock, pre, off
}

// slugsChanged reports a message slug the last layout drew whose label
// reads differently now. It reads each one on every frame, as Slack's
// MessageMention reads the store on every render (a read asks for what
// the store lacks only while nothing has it out).
func (v *View) slugsChanged() bool {
	f := slugReadFn()
	if f == nil {
		return false
	}
	changed := false
	for _, u := range v.d.slugs {
		if label, lock, pre, off := f(u.ch, u.ts); label != u.label || lock != u.lock || pre != u.pre || off != u.off {
			changed = true
		}
	}
	return changed
}

// SlugText is a message slug span's label as a message draws it now
// (SlugRead), as text (a private channel's lock icon is "#", as Slack's
// plain label has it), for text built from a stored Doc (a preview's
// richtext.PlainTextWith); ok false for any other span, and when no read
// is installed (the span's own label stands).
func SlugText(sp richtext.Span) (string, bool) {
	if sp.Kind != richtext.MessageMention {
		return "", false
	}
	f := slugReadFn()
	if f == nil || sp.Chan == "" || sp.ID == "" {
		return "", false
	}
	label, lock, pre, _ := f(sp.Chan, sp.ID)
	if lock && pre > 0 && pre <= len(label) {
		label = label[:pre] + "#" + label[pre:]
	}
	return label, true
}
