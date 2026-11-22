package videoplayer

import (
	"image"
	"image/color"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// A member's name recording plays as NamePronunciationAudioPlayer (Slack's
// boot-shared BG0H, build 9f450b8ad3db) plays it: a plain Audio element
// on the file's url_private, no player UI. The button's click plays it
// from the start (currentTime = 0, play()), or pauses it while it plays;
// it stops at the end; it's one of the players the coordinator keeps one
// of playing (MediaPlaybackCoordinator's register and playbackStarted),
// and the button unmounting pauses it. It keeps and sends no progress.

// nameKey is the sound's tile: the file in pane, for member user.
func nameKey(pane, user string, f *richtext.File) key { return key{pane, "name", user, f.ID} }

// NameSound is the sound for f, a name recording of member user shown in
// pane: whether it plays now. The button calls it each frame it draws,
// which keeps the element mounted (a frame without it pauses the sound,
// EndFrame).
func (c *Controller) NameSound(pane, user string, f *richtext.File) bool {
	if c == nil || f == nil || f.ID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.nameTileLocked(pane, user, f)
	t.laid = c.frame
	if t.unmount != nil {
		t.unmount.Stop()
		t.unmount = nil
	}
	if t.h != nil {
		c.applyAudioLocked(t)
	}
	return t.playing || t.loading
}

// ToggleNameSound is the button's click (m(!u)): a sound that plays
// pauses; one that doesn't plays from the start.
func (c *Controller) ToggleNameSound(pane, user string, f *richtext.File) {
	if c == nil || f == nil || f.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.nameTileLocked(pane, user, f)
	if t.playing || t.loading {
		if t.h != nil {
			t.h.Pause()
		}
		t.loading = false
		return
	}
	if t.h == nil {
		c.mountLocked(t, true)
		return
	}
	t.elapsed = 0
	t.h.Seek(0)
	t.h.Play()
}

func (c *Controller) nameTileLocked(pane, user string, f *richtext.File) *tile {
	t := c.tileLocked(nameKey(pane, user, f), f)
	if t.au == nil {
		t.au = &audioState{}
	}
	t.au.name, t.au.mini = true, true // the natural rate
	return t
}

// NameSoundButton is NamePronunciationAudioPlayer's button state.
type NameSoundButton struct {
	hover, press bool
}

// LayoutNameSound draws NamePronunciationAudioPlayer for f, member user's
// recording shown in pane, at gtx's origin and takes its clicks: a small
// IconButton in its 30px box (.p-r_member_profile_name_pronunciation--
// unstyled_button; c-icon_button--size_small's 16px icon) with
// sound-medium in content-sec (content-pry over base-ter, hovered) and
// the tooltip "Play name pronunciation" above it; while the sound plays
// the animated icon instead (.p-r_member_profile--animated-sound-icon:
// sound-down, sound-medium and sound-up in highlight, a third of a second
// each, 1s steps(3), no tooltip). Nothing until the file has its
// url_private.
func (c *Controller) LayoutNameSound(gtx layout.Context, b *NameSoundButton, pane, user string, f *richtext.File) layout.Dimensions {
	if c == nil || f == nil || f.URL == "" {
		return layout.Dimensions{}
	}
	sz := gtx.Dp(30)
	box := image.Rect(0, 0, sz, sz)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b, Kinds: pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			b.hover = true
		case pointer.Leave:
			b.hover = false
		case pointer.Cancel:
			b.hover, b.press = false, false
		case pointer.Press:
			if e.Buttons == pointer.ButtonPrimary {
				b.press = true
			}
		case pointer.Release:
			if b.press && image.Pt(int(e.Position.X), int(e.Position.Y)).In(box) {
				c.ToggleNameSound(pane, user, f)
			}
			b.press = false
		}
	}
	playing := c.NameSound(pane, user, f)
	dark := false
	if th := c.theme(); th != nil {
		dark = th.Dark
	}
	vars := css.SlackVars(dark)
	col := func(name string, def color.NRGBA) color.NRGBA {
		if v, ok := css.ParseColor(vars[name]); ok {
			return v
		}
		return def
	}
	is := gtx.Dp(16)
	at := image.Pt((sz-is)/2, (sz-is)/2)
	if playing {
		hi := color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}
		if v, ok := css.ParseColor("rgb(" + vars["--sk_highlight"] + ")"); ok {
			hi = v
		}
		names := [3]string{"sound-down", "sound-medium", "sound-up"}
		step := time.Second / 3
		now := gtx.Now
		i := int(now.UnixNano()/int64(step)) % 3
		off := op.Offset(at).Push(gtx.Ops)
		icons.Get(names[i])(gtx, unit.Dp(16), hi)
		off.Pop()
		next := now.Truncate(step).Add(step)
		gtx.Execute(op.InvalidateCmd{At: next})
	} else {
		ink := col("--dt_color-content-sec", color.NRGBA{R: 0x5e, G: 0x5d, B: 0x60, A: 0xff})
		if b.hover {
			bg := col("--dt_color-base-ter", color.NRGBA{A: 0x0f})
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(box, gtx.Dp(8)).Op(gtx.Ops))
			ink = col("--dt_color-content-pry", color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff})
		}
		off := op.Offset(at).Push(gtx.Ops)
		icons.Get("sound-medium")(gtx, unit.Dp(16), ink)
		off.Pop()
	}
	area := clip.Rect(box).Push(gtx.Ops)
	event.Op(gtx.Ops, b)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
	if !playing && c.env.Tooltip != nil {
		c.env.Tooltip(gtx, b, b.hover, box, false, 0, i18n.T("rimeto_user_profile", "Play name pronunciation"))
	}
	return layout.Dimensions{Size: image.Pt(sz, sz)}
}
