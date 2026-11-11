package popups

import (
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/i18n"
)

// The status modal's semantic emoji (boot-shared D72a CustomStatusModal,
// the semantic_emoji_matching experiment, O2By RY): with the experiment
// on and no emoji chosen by hand (the modal's tl ref: an emoji it opened
// with, a picked emoji, a preset or an edited scheduled status with one;
// Clear resets it), each change to the status text runs a2, debounced
// 250 ms (useDebouncedCallback's default): acrank.StatusEmoji's match
// with the preferred_skin_tone applied (getSkinToneModifiedName), or no
// emoji when nothing matches.

// statusSemanticDelay is useDebouncedCallback's default wait.
const statusSemanticDelay = 250 * time.Millisecond

// semanticOn is the experiment's read (RY: "on" only).
func (s *actStatus) semanticOn() bool {
	a := s.h.Core()
	return a != nil && a.Experiment("semantic_emoji_matching") == "on"
}

// semanticTyped is a5's tail: the text changed. The editor reports a
// programmatic SetText as a change too, so a text a5 already saw (the
// status the modal opened with) isn't typing.
func (s *actStatus) semanticTyped(now time.Time) {
	t := s.ed.Text()
	if t == s.semSeen {
		return
	}
	s.semSeen = t
	if s.emojiPicked || !s.semanticOn() {
		return
	}
	s.semAt = now.Add(statusSemanticDelay)
}

// semanticTick runs a2 once the debounce has passed.
func (s *actStatus) semanticTick(gtx layout.Context) {
	if s.semAt.IsZero() {
		return
	}
	if gtx.Now.Before(s.semAt) {
		gtx.Execute(op.InvalidateCmd{At: s.semAt})
		return
	}
	s.semAt = time.Time{}
	if s.emojiPicked {
		return
	}
	s.emoji = statusSemanticEmoji(s.h, s.ed.Text())
}

// statusSemanticEmoji is a2: the matched emoji with the skin tone, "" for
// none.
func statusSemanticEmoji(h Host, text string) string {
	name := acrank.StatusEmoji(text, func(k string) string { return i18n.T("current_status", k) })
	if name == "" || h == nil || h.Core() == nil {
		return name
	}
	// getSkinToneModifiedName(name, preferred_skin_tone, team)
	tone := epParseTone(h.Core().Prefs().SkinTone)
	if tone < 2 {
		return name
	}
	x, _ := epIndexFor(h)
	if x == nil {
		return name
	}
	if id, ok := x.byName[name]; ok && x.hasSkins(id) {
		return name + "::skin-tone-" + strconv.Itoa(tone)
	}
	return name
}
