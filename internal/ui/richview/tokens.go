package richview

import (
	"image/color"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Message text follows the stylesheets' Slack design tokens (Slick themes,
// user.css): each field below is painted from that token by Slack's
// stylesheet (body text, .c-timestamp, a, .c-member_slug, code, …).
var paletteTokens = []struct {
	f   func(*Palette) *color.NRGBA
	tok theme.Tok
}{
	{func(p *Palette) *color.NRGBA { return &p.Panel }, theme.TokPaneBg},
	{func(p *Palette) *color.NRGBA { return &p.Text }, theme.TokText},
	{func(p *Palette) *color.NRGBA { return &p.Text2 }, theme.TokSecondary},
	{func(p *Palette) *color.NRGBA { return &p.Text3 }, theme.Both("var(--dt_color-content-sec)")},
	{func(p *Palette) *color.NRGBA { return &p.Link }, theme.TokLink},
	{func(p *Palette) *color.NRGBA { return &p.Edited }, theme.Both("rgb(var(--sk_foreground_high_solid))")},
	{func(p *Palette) *color.NRGBA { return &p.CardText }, theme.Both("var(--dt_color-content-sec)")},
	{func(p *Palette) *color.NRGBA { return &p.CardFooter }, theme.Both("var(--dt_color-content-ter)")},
	{func(p *Palette) *color.NRGBA { return &p.MentionText }, theme.Both("var(--dt_color-content-hgl-1)")},
	{func(p *Palette) *color.NRGBA { return &p.MentionBg }, theme.Both("var(--dt_color-surf-hgl-1)")},
	{func(p *Palette) *color.NRGBA { return &p.MentionHoverBg }, theme.Both("rgba(var(--sk_highlight_accent),0.2)")},
	{func(p *Palette) *color.NRGBA { return &p.SelfMentionText }, theme.Tok{Dark: "var(--dt_color-content-hgl-3)"}},
	{func(p *Palette) *color.NRGBA { return &p.SelfMentionBg }, theme.Both("var(--dt_color-surf-hgl-3)")},
	{func(p *Palette) *color.NRGBA { return &p.CodeText }, theme.Tok{Light: "rgb(var(--dt_color-plt-flamingo-70))"}},
	{func(p *Palette) *color.NRGBA { return &p.CodeBg }, theme.Both("rgba(var(--sk_foreground_min),0.04)")},
	{func(p *Palette) *color.NRGBA { return &p.CodeBorder }, theme.Both("rgba(var(--sk_foreground_low),0.13)")},
	{func(p *Palette) *color.NRGBA { return &p.PreBg }, theme.Both("rgba(var(--sk_foreground_min),0.04)")},
	{func(p *Palette) *color.NRGBA { return &p.PreBorder }, theme.Both("rgba(var(--sk_foreground_low),0.13)")},
	{func(p *Palette) *color.NRGBA { return &p.QuoteBar }, theme.Tok{Light: "rgb(var(--sk_foreground_low_solid))", Dark: "rgba(var(--sk_foreground_low),0.2)"}},
	{func(p *Palette) *color.NRGBA { return &p.Border }, theme.Tok{Light: "var(--dt_color-otl-ter)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(p *Palette) *color.NRGBA { return &p.BorderHover }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.3)"}},
	{func(p *Palette) *color.NRGBA { return &p.Divider }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.13)"}},
	{func(p *Palette) *color.NRGBA { return &p.ButtonBorder }, theme.Tok{Light: "var(--dt_color-otl-sec)", Dark: "rgba(var(--sk_foreground_low),0.3)"}},
	{func(p *Palette) *color.NRGBA { return &p.ButtonHoverBg }, theme.Both("rgb(var(--sk_foreground_min_solid))")},
	{func(p *Palette) *color.NRGBA { return &p.Placeholder }, theme.Tok{Light: "var(--dt_color-base-ter)", Dark: "rgb(var(--sk_foreground_min_solid))"}},
	{func(p *Palette) *color.NRGBA { return &p.Highlight }, theme.Tok{Light: "var(--dt_color-surf-hgl-3)", Dark: "rgba(var(--sk_secondary_highlight),0.3)"}},
	{func(p *Palette) *color.NRGBA { return &p.Primary }, theme.Both("var(--dt_color-base-inv-hgl-2)")},
	{func(p *Palette) *color.NRGBA { return &p.Danger }, theme.Both("var(--dt_color-base-inv-imp)")},
}

// Themed is th's mode palette (LightPalette/DarkPalette) with the tokens
// the stylesheets override laid over it. ok is false when no stylesheet
// overrides a token: use the plain palette.
func Themed(th *theme.Theme) (p Palette, ok bool) {
	if th == nil || th.Tokens == nil || !th.Tokens.Overridden() {
		return Palette{}, false
	}
	if th.Dark {
		p = DarkPalette()
	} else {
		p = LightPalette()
	}
	for _, f := range paletteTokens {
		theme.SetTok(th.Tokens, f.f(&p), f.tok, th.Dark)
	}
	return p.flat(), true
}
