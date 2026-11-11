package messages

import (
	"image/color"
	"sync"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The message list follows the stylesheets' Slack design tokens (Slick
// themes, user.css) like views' and richview's palettes: each field below
// is painted from that token in Slack (.p-message_pane background, message
// text, timestamps, hover, dividers, links). With no token overridden
// colorsFor returns the measured palettes unchanged.
var colorTokens = []struct {
	f   func(*colors) *color.NRGBA
	tok theme.Tok
}{
	{func(c *colors) *color.NRGBA { return &c.Bg }, theme.TokPaneBg},
	{func(c *colors) *color.NRGBA { return &c.ThreadBg }, theme.TokPaneBg},
	{func(c *colors) *color.NRGBA { return &c.Hover }, theme.TokMsgHover},
	{func(c *colors) *color.NRGBA { return &c.Text }, theme.TokText},
	{func(c *colors) *color.NRGBA { return &c.Text3 }, theme.TokText},
	{func(c *colors) *color.NRGBA { return &c.Text2 }, theme.TokSecondary},
	{func(c *colors) *color.NRGBA { return &c.TimeSystem }, theme.TokSecondary},
	{func(c *colors) *color.NRGBA { return &c.SystemText }, theme.TokSecondary},
	{func(c *colors) *color.NRGBA { return &c.ToolbarIcon }, theme.TokSecondary},
	{func(c *colors) *color.NRGBA { return &c.Edited }, theme.Both("rgb(var(--sk_foreground_high_solid))")},
	{func(c *colors) *color.NRGBA { return &c.Link }, theme.TokLink},
	{func(c *colors) *color.NRGBA { return &c.ReactSelfBorder }, theme.Tok{Light: theme.TokLink.Light}}, // no ring in dark
	{func(c *colors) *color.NRGBA { return &c.ReactSelfText }, theme.Tok{Light: theme.TokLink.Light, Dark: theme.TokText.Dark}},
	{func(c *colors) *color.NRGBA { return &c.DayLine }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(c *colors) *color.NRGBA { return &c.PillBorder }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(c *colors) *color.NRGBA { return &c.ThreadBorder }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(c *colors) *color.NRGBA { return &c.HeaderBorder }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(c *colors) *color.NRGBA { return &c.ReactBg }, theme.Both("rgba(var(--sk_foreground_min),0.06)")},
	{func(c *colors) *color.NRGBA { return &c.ToolbarHoverBg }, theme.TokMsgHover},
	{func(c *colors) *color.NRGBA { return &c.Important }, theme.Both("var(--dt_color-content-imp)")},
}

var (
	tokColMu  sync.Mutex
	tokColGen uint64
	tokColTh  *theme.Theme
	tokCol    colors
)

// themedColors is the mode's colors with the overridden tokens laid over
// them, flattened, cached per theme generation. nil: no token overridden.
func themedColors(th *theme.Theme) *colors {
	if th == nil || th.Tokens == nil || !th.Tokens.Overridden() {
		return nil
	}
	tokColMu.Lock()
	defer tokColMu.Unlock()
	if tokColTh != th || tokColGen != th.Gen {
		c := lightColors
		if th.Dark {
			c = darkColors
		}
		for _, f := range colorTokens {
			theme.SetTok(th.Tokens, f.f(&c), f.tok, th.Dark)
		}
		tokCol, tokColTh, tokColGen = flatten(c), th, th.Gen
	}
	cp := tokCol // callers hold it for a frame; a copy survives a rebuild
	return &cp
}
