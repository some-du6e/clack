package views

import (
	"image/color"
	"sync"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The views palette follows the stylesheets' Slack design tokens (Slick
// themes, user.css) like theme.Palette does: every pal field below is
// painted from that token in Slack. With no token overridden palOf
// returns the measured palettes unchanged.
var palTokens = []struct {
	f   func(*pal) *color.NRGBA
	tok theme.Tok
}{
	{func(p *pal) *color.NRGBA { return &p.Bg }, theme.TokPaneBg},
	{func(p *pal) *color.NRGBA { return &p.Text }, theme.Both("var(--dt_color-content-pry)")},
	{func(p *pal) *color.NRGBA { return &p.Muted }, theme.Both("var(--dt_color-content-ter)")},
	{func(p *pal) *color.NRGBA { return &p.Faint }, theme.Tok{Light: "var(--dt_color-otl-pry)", Dark: "var(--dt_color-content-ter)"}},
	{func(p *pal) *color.NRGBA { return &p.Border }, theme.Both("var(--dt_color-otl-ter)")},
	{func(p *pal) *color.NRGBA { return &p.BorderStr }, theme.Both("var(--dt_color-otl-sec)")},
	{func(p *pal) *color.NRGBA { return &p.Hover }, theme.Both("var(--dt_color-base-pry-hover)")},
	{func(p *pal) *color.NRGBA { return &p.SelBg }, theme.Both("var(--dt_color-base-hgl-3)")},
	{func(p *pal) *color.NRGBA { return &p.SelText }, theme.Both("var(--dt_color-content-pry)")},
	{func(p *pal) *color.NRGBA { return &p.Link }, theme.Both("var(--dt_color-content-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.Green }, theme.Both("var(--dt_color-base-inv-hgl-2)")},
	{func(p *pal) *color.NRGBA { return &p.Red }, theme.Both("var(--dt_color-content-imp)")},
	{func(p *pal) *color.NRGBA { return &p.Badge }, theme.Both("var(--dt_color-theme-base-imp)")},
	{func(p *pal) *color.NRGBA { return &p.Skel }, theme.Both("var(--dt_color-base-ter)")},
	{func(p *pal) *color.NRGBA { return &p.Focus }, theme.Both("var(--dt_color-otl-hgl-1-sec)")},
	{func(p *pal) *color.NRGBA { return &p.Chip }, theme.Both("var(--dt_color-base-pry-hover)")},
	{func(p *pal) *color.NRGBA { return &p.ChipSel }, theme.Both("var(--dt_color-base-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.ChipSelText }, theme.Both("var(--dt_color-content-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.Surface }, theme.Both("var(--dt_color-ctr-pry)")},
	{func(p *pal) *color.NRGBA { return &p.Overlay }, theme.Both("var(--dt_color-base-modal)")},
	{func(p *pal) *color.NRGBA { return &p.Mention }, theme.Both("var(--dt_color-surf-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.Yellow }, theme.Both("var(--dt_color-base-hgl-3)")},
	{func(p *pal) *color.NRGBA { return &p.TabLine }, theme.Both("var(--dt_color-theme-base-pry)")},
	{func(p *pal) *color.NRGBA { return &p.Sec }, theme.Both("var(--dt_color-content-sec)")},
	{func(p *pal) *color.NRGBA { return &p.TitleText }, theme.Both("var(--dt_color-theme-content-pry)")},
	{func(p *pal) *color.NRGBA { return &p.Accent }, theme.Both("var(--dt_color-theme-base-imp)")},
	{func(p *pal) *color.NRGBA { return &p.AccentText }, theme.Both("var(--dt_color-theme-content-imp)")},
	{func(p *pal) *color.NRGBA { return &p.ReadFill }, theme.Both("var(--dt_color-base-sec)")},
	{func(p *pal) *color.NRGBA { return &p.Track }, theme.Both("var(--dt_color-base-ter)")},
	{func(p *pal) *color.NRGBA { return &p.PillOnBg }, theme.Both("var(--dt_color-base-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.PillOnFg }, theme.Both("var(--dt_color-content-hgl-1)")},
	{func(p *pal) *color.NRGBA { return &p.Pressed }, theme.Both("var(--dt_color-base-pry-pressed)")},
	{func(p *pal) *color.NRGBA { return &p.CardLine }, theme.Both("var(--dt_color-otl-ter)")},
	{func(p *pal) *color.NRGBA { return &p.Ring }, theme.Both("var(--dt_color-otl-ter)")},
	{func(p *pal) *color.NRGBA { return &p.Page }, theme.Both("var(--dt_color-base-sec)")},
}

var (
	tokPalMu  sync.Mutex
	tokPalGen uint64
	tokPalTh  *theme.Theme
	tokPal    pal
)

// themedPal is the mode's palette with the overridden tokens laid over
// it, cached per theme generation. nil: no token overridden.
func themedPal(th *theme.Theme) *pal {
	if th == nil || th.Tokens == nil || !th.Tokens.Overridden() {
		return nil
	}
	tokPalMu.Lock()
	defer tokPalMu.Unlock()
	if tokPalTh != th || tokPalGen != th.Gen {
		p := lightPal
		if th.Dark {
			p = darkPal
		}
		for _, f := range palTokens {
			theme.SetTok(th.Tokens, f.f(&p), f.tok, th.Dark)
		}
		tokPal, tokPalTh, tokPalGen = flatPal(p), th, th.Gen
	}
	cp := tokPal // callers hold it for a frame; a copy survives a rebuild
	return &cp
}
