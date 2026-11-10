package huddleui

import (
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// Raise hand and the other stickers (bv1-13): the huddle toolbar's
// HuddleToolbarSegmentedStickerPickerButton (boot-shared zeVD). Its main
// half shows your sticker, or the raised hand when you have none, and
// toggles: "Raise hand" puts raised_hand (with your preferred_skin_tone,
// getSkinToneModifiedName: raised_hand::skin-tone-N) on your tile, and
// with a sticker on it reads "Lower hand" (or "Remove sticker") and takes
// it off. The caret half opens the sticker picker. A sticker is an
// awareness entry (huddle.Session.SetSticker); HuddlePeerTile draws it at
// the tile's top left with its label (eAbZ).
//
// Under the huddles_raised_hand_queue experiment the label reads "Raised
// hand", and a raised hand lowers itself after 5 s as the active speaker
// (useAutoLowerRaisedHand).

// stickers are Slack's sticker picker, in its order, with their labels.
var stickers = []struct{ Emoji, Label string }{
	{"raised_hand", "Raise hand"},
	{"coffee", "Be right back"},
	{"headphones", "DJ’ing"},
	{"speaker", "Audio-only"},
	{"+1", "Yes"},
	{"-1", "No"},
	{"white_check_mark", "Done"},
	{"iphone", "On mobile"},
}

// stickerBase is a sticker's emoji without its skin tone.
func stickerBase(e string) string {
	if i := strings.Index(e, "::"); i >= 0 {
		return e[:i]
	}
	return e
}

// isHand reports whether a sticker is a raised hand (any skin tone).
func isHand(e string) bool { return stickerBase(e) == "raised_hand" }

// stickerLabel is the tile's description of a sticker ("" for none).
func (c *Controller) stickerLabel(e string) string {
	b := stickerBase(e)
	if b == "raised_hand" && c.handQueue() {
		return "Raised hand" // activeStateLabelOverrides
	}
	for _, s := range stickers {
		if s.Emoji == b {
			return s.Label
		}
	}
	return ""
}

func (c *Controller) handQueue() bool {
	app := c.h.Core()
	return app != nil && app.Experiment("huddles_raised_hand_queue") == "on"
}

// handEmoji is raised_hand in your skin tone.
func (c *Controller) handEmoji() string { return c.withSkinTone("raised_hand") }

// withSkinTone is getSkinToneModifiedName for the stickers that take one.
func (c *Controller) withSkinTone(e string) string {
	if e != "raised_hand" && e != "+1" && e != "-1" {
		return e
	}
	app := c.h.Core()
	if app == nil {
		return e
	}
	t := app.PrefString("preferred_skin_tone")
	if t == "" {
		if n := app.PrefInt("preferred_skin_tone", 0); n > 0 {
			t = strconv.Itoa(n)
		}
	}
	if n, err := strconv.Atoi(t); err == nil && n >= 2 && n <= 6 {
		return e + "::skin-tone-" + t
	}
	return e
}

// ToggleHand is the sticker button's main half: take your sticker off,
// or raise your hand.
func (c *Controller) ToggleHand() error {
	s := c.Session()
	if s == nil {
		return nil
	}
	if s.Sticker() != "" {
		return s.SetSticker("")
	}
	return s.SetSticker(c.handEmoji())
}

// PickSticker is a sticker picked in the picker (on, or off when it's
// the one you have on).
func (c *Controller) PickSticker(e string) error {
	s := c.Session()
	if s == nil {
		return nil
	}
	return s.ToggleSticker(c.withSkinTone(e))
}

// handTip is the main half's tooltip.
func handTip(sticker string) string {
	switch {
	case sticker == "":
		return "Raise hand"
	case isHand(sticker):
		return "Lower hand"
	}
	return "Remove sticker"
}

type handState struct {
	speakingSince time.Time
	lastSpoke     time.Time
}

// autoLowerHand is useAutoLowerRaisedHand: under the raised-hand queue,
// 5 s as the active speaker with your hand up lowers it. Runs on session
// changes.
func (c *Controller) autoLowerHand(s *huddle.Session, st huddle.State) {
	if !c.handQueue() || !isHand(s.Sticker()) {
		c.mu.Lock()
		c.hand = handState{}
		c.mu.Unlock()
		return
	}
	now := time.Now()
	speaking := false
	for _, p := range st.Participants {
		if p.Self {
			speaking = p.Speaking(now)
		}
	}
	c.mu.Lock()
	h := &c.hand
	if speaking {
		h.lastSpoke = now
		if h.speakingSince.IsZero() {
			h.speakingSince = now
		}
	} else if now.Sub(h.lastSpoke) > time.Second { // a pause hands the floor back
		h.speakingSince = time.Time{}
	}
	lower := !h.speakingSince.IsZero() && now.Sub(h.speakingSince) >= 5*time.Second
	if lower {
		c.hand = handState{}
	}
	c.mu.Unlock()
	if lower {
		_ = s.SetSticker("")
	}
}
