package theme

import (
	"image/color"
	"strings"
)

// Tokens are Slack's design tokens as the active stylesheets leave them at
// the document root (slackc's css.RootTokens): a Slick theme's
// `:root,…{--dt_color-base-pry:#2e3440 !important}` or a user.css
// override. Every native surface Slack paints from a token reads it here,
// so a theme recolours the whole client the way it does in Slack.
type Tokens interface {
	// Color resolves a CSS color expression over the tokens
	// ("var(--dt_color-base-pry)", "rgb(var(--sk_primary_foreground))").
	// own reports that a stylesheet's declaration took part.
	Color(expr string) (c color.NRGBA, own, ok bool)
	// Overridden reports that a stylesheet declares any root token.
	Overridden() bool
	// Key fingerprints the overrides (0: none).
	Key() uint64
}

func tokensKey(t Tokens) uint64 {
	if t == nil {
		return 0
	}
	return t.Key()
}

// SetColor sets *dst from the first expr a stylesheet overrides a token
// of, and reports whether it did. With nothing overridden *dst keeps its
// measured default, so an unthemed client is unchanged.
func SetColor(t Tokens, dst *color.NRGBA, exprs ...string) bool {
	if t == nil {
		return false
	}
	for _, e := range exprs {
		if e == "" {
			continue
		}
		if c, own, ok := t.Color(e); own && ok {
			*dst = c
			return true
		}
	}
	return false
}

// Tok is the token expression per mode that paints a surface in Slack
// ("" = none in that mode). Alternatives are separated by " | ": the
// first one a stylesheet overrides wins (Slack's pane is painted from
// --sk_primary_background, its newer surfaces from --dt_color-base-pry;
// themes set both, user CSS often only one).
type Tok struct{ Light, Dark string }

// Exprs is the mode's expression list.
func (k Tok) Exprs(dark bool) []string {
	e := k.Light
	if dark {
		e = k.Dark
	}
	if e == "" {
		return nil
	}
	return strings.Split(e, " | ")
}

// Both is the same expression in both modes.
func Both(expr string) Tok { return Tok{expr, expr} }

// SetTok is SetColor with the mode's expressions of k.
func SetTok(t Tokens, dst *color.NRGBA, k Tok, dark bool) bool {
	return SetColor(t, dst, k.Exprs(dark)...)
}

// Token bindings that the palettes here, in views and in richview share.
var (
	TokPaneBg    = Both("rgb(var(--sk_primary_background)) | var(--dt_color-base-pry)")
	TokText      = Both("rgb(var(--sk_primary_foreground)) | var(--dt_color-content-pry)")
	TokLink      = Both("var(--dt_color-content-hgl-1) | rgb(var(--sk_highlight))")
	TokHairline  = Both("rgba(var(--sk_foreground_low),0.13) | var(--dt_color-otl-ter)")
	TokMsgHover  = Both("rgb(var(--sk_foreground_min_solid)) | var(--dt_color-base-pry-hover)")
	TokSecondary = Both("rgb(var(--sk_foreground_max_solid)) | var(--dt_color-content-sec)")
)

// paletteTokens is the Slack token behind each Palette field, read from
// the web client's stylesheets (2026-11-07). The chrome and sidebar sit in
// Slack's .sk-client-theme--light-inverted-sidebar scope in light mode,
// which re-declares --dt_color-theme-* from the plt ramps, so their light
// expressions are written on the ramps (a theme's --dt_color-theme-* never
// reaches the light sidebar in Slack either, its ramps do). Fields not
// listed (presence, code, reactions…) have no theme token in Slack or are
// themed by the stylesheets' own rules on Slack class names.
var paletteTokens = []struct {
	f   func(*Palette) *color.NRGBA
	tok Tok
}{
	// window chrome: .p-theme_background's radial + conic gradient over
	// two aubergine stops (the dark ramp is inverted, hence 10/0)
	{func(p *Palette) *color.NRGBA { return &p.ChromeA }, Tok{"rgb(var(--dt_color-plt-aubergine-90))", "rgb(var(--dt_color-plt-aubergine-10))"}},
	{func(p *Palette) *color.NRGBA { return &p.ChromeB }, Tok{"rgb(var(--dt_color-plt-aubergine-100))", "rgb(var(--dt_color-plt-aubergine-0))"}},
	{func(p *Palette) *color.NRGBA { return &p.ChromeC }, Tok{"rgb(var(--dt_color-plt-aubergine-90))", "rgb(var(--dt_color-plt-aubergine-10))"}},
	// top nav / rail: .p-tab_rail, .p-top_nav__search
	{func(p *Palette) *color.NRGBA { return &p.ChromeText }, Tok{"", "var(--dt_color-theme-content-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.ChromeTextDim }, Tok{"rgba(var(--dt_color-plt-aubergine-5),0.9)", "var(--dt_color-theme-content-inv-sec)"}},
	{func(p *Palette) *color.NRGBA { return &p.ChromeActive }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.25)", "var(--dt_color-theme-surf-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.SearchBg }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.25)", "var(--dt_color-theme-surf-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.SearchText }, Tok{"", "var(--dt_color-theme-content-inv-pry)"}},

	// sidebar: .p-view_contents--sidebar, .p-channel_sidebar__channel…
	{func(p *Palette) *color.NRGBA { return &p.SidebarBg }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.11)", "var(--dt_color-theme-surf-inv-sec)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarText }, Tok{"rgba(var(--dt_color-plt-aubergine-5),0.9)", "var(--dt_color-theme-content-inv-sec)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarTextUnread }, Tok{"", "var(--dt_color-theme-content-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarHover }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.08)", "var(--dt_color-theme-surf-inv-ter)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarActive }, Tok{"rgb(var(--dt_color-plt-aubergine-0))", "var(--dt_color-theme-base-hgl-1)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarActiveText }, Tok{"rgb(var(--dt_color-plt-aubergine-100))", "var(--dt_color-theme-content-hgl-1)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarDivider }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.22)", "var(--dt_color-theme-otl-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.SidebarButton }, Tok{"rgba(var(--dt_color-plt-aubergine-0),0.25)", "var(--dt_color-theme-surf-inv-pry)"}},
	{func(p *Palette) *color.NRGBA { return &p.WorkspaceBorder }, Tok{"", "var(--dt_color-otl-ter)"}},
	// mention badge (.c-badge--themed)
	{func(p *Palette) *color.NRGBA { return &p.Badge }, Tok{"rgb(var(--dt_color-plt-aubergine-20))", "var(--dt_color-theme-base-imp)"}},
	{func(p *Palette) *color.NRGBA { return &p.BadgeText }, Tok{"rgb(var(--dt_color-plt-aubergine-100))", "var(--dt_color-theme-content-imp)"}},
	{func(p *Palette) *color.NRGBA { return &p.RailBadge }, Tok{"rgb(var(--dt_color-plt-aubergine-20))", "var(--dt_color-theme-base-imp)"}},
	{func(p *Palette) *color.NRGBA { return &p.RailBadgeText }, Tok{"rgb(var(--dt_color-plt-aubergine-100))", "var(--dt_color-theme-content-imp)"}},

	// primary panel: .p-view_contents--primary, body text, .c-message_kit…
	{func(p *Palette) *color.NRGBA { return &p.Bg }, TokPaneBg},
	{func(p *Palette) *color.NRGBA { return &p.BgHover }, TokMsgHover},
	{func(p *Palette) *color.NRGBA { return &p.BgRaised }, Both("var(--dt_color-ctr-pry)")},
	{func(p *Palette) *color.NRGBA { return &p.BgInset }, Both("rgb(var(--sk_foreground_min_solid)) | var(--dt_color-ctr-sec)")},
	{func(p *Palette) *color.NRGBA { return &p.Fg }, TokText},
	{func(p *Palette) *color.NRGBA { return &p.FgMuted }, Both("var(--dt_color-content-sec)")},
	{func(p *Palette) *color.NRGBA { return &p.FgSubtle }, Tok{"rgb(var(--sk_foreground_high_solid))", "rgb(var(--sk_foreground_max_solid))"}},
	{func(p *Palette) *color.NRGBA { return &p.Border }, TokHairline},
	{func(p *Palette) *color.NRGBA { return &p.BorderStrong }, Both("rgba(var(--sk_foreground_low),0.3) | var(--dt_color-otl-sec)")},
	{func(p *Palette) *color.NRGBA { return &p.Skeleton }, Both("var(--dt_color-base-ter)")},
	{func(p *Palette) *color.NRGBA { return &p.Overlay }, Both("var(--dt_color-base-modal)")},
	{func(p *Palette) *color.NRGBA { return &p.Link }, TokLink},
	{func(p *Palette) *color.NRGBA { return &p.Accent }, Both("var(--dt_color-theme-base-pry)")},
	{func(p *Palette) *color.NRGBA { return &p.Mention }, Both("var(--dt_color-content-hgl-1)")},
	{func(p *Palette) *color.NRGBA { return &p.MentionBg }, Both("var(--dt_color-surf-hgl-1)")},
	{func(p *Palette) *color.NRGBA { return &p.SelfMention }, Both("var(--dt_color-surf-hgl-3)")},
	{func(p *Palette) *color.NRGBA { return &p.Highlight }, Tok{"rgba(var(--sk_secondary_highlight),0.2)", "rgba(var(--sk_secondary_highlight),0.1)"}},
	{func(p *Palette) *color.NRGBA { return &p.CodeBg }, Both("rgba(var(--sk_foreground_min),0.04)")},
	{func(p *Palette) *color.NRGBA { return &p.CodeBorder }, Both("rgba(var(--sk_foreground_low),0.13)")},
	{func(p *Palette) *color.NRGBA { return &p.PreBg }, Both("rgba(var(--sk_foreground_min),0.04)")},
	{func(p *Palette) *color.NRGBA { return &p.QuoteBar }, Tok{"rgb(var(--sk_foreground_low_solid))", "rgba(var(--sk_foreground_low),0.2)"}},
	// .c-message_list__unread_divider: --sk_raspberry_red
	{func(p *Palette) *color.NRGBA { return &p.NewLine }, Both("rgb(var(--sk_raspberry_red))")},
	// .c-wysiwyg_container__button--send
	{func(p *Palette) *color.NRGBA { return &p.Send }, Both("var(--TextyTheme-send-bg)")},
	{func(p *Palette) *color.NRGBA { return &p.SendText }, Both("var(--TextyTheme-send-fg)")},
	{func(p *Palette) *color.NRGBA { return &p.Danger }, Both("var(--dt_color-base-inv-imp)")},
	// .c-tooltip__tip: --sk_tooltip-background: var(--dt_color-base-inv-pry)
	{func(p *Palette) *color.NRGBA { return &p.Tooltip }, Both("var(--dt_color-base-inv-pry)")},
	{func(p *Palette) *color.NRGBA { return &p.TooltipText }, Both("var(--dt_color-content-inv-pry)")},
	{func(p *Palette) *color.NRGBA { return &p.TooltipSub }, Both("var(--dt_color-content-inv-sec)")},
	// .sk-client-theme--dark .c-tooltip__tip: drop-shadow(0 0 1px var(--dt_color-otl-pry))
	{func(p *Palette) *color.NRGBA { return &p.TooltipGlow }, Tok{"", "var(--dt_color-otl-pry)"}},
}

// ApplyTokens lays the overridden tokens over p (an unflattened
// LightPalette/DarkPalette copy). Fields whose tokens no stylesheet
// touches keep their values, so with no theme p is unchanged.
func ApplyTokens(p *Palette, t Tokens, dark bool) {
	if t == nil || !t.Overridden() {
		return
	}
	for _, f := range paletteTokens {
		SetTok(t, f.f(p), f.tok, dark)
	}
}

// ApplySidebarTokens re-reads the sidebar fields from t, the tokens a
// stylesheet declares on .p-view_contents--sidebar itself (Hot Dog Stand
// sets Slack's --dt_color-theme-* there). Those declarations beat Slack's
// light inverted-sidebar block too, so the --dt_color-theme-* expressions
// apply in both modes.
func ApplySidebarTokens(p *Palette, t Tokens) {
	if t == nil || !t.Overridden() {
		return
	}
	for _, f := range paletteTokens {
		switch f.f(p) {
		case &p.SidebarBg, &p.SidebarText, &p.SidebarTextUnread, &p.SidebarHover, &p.SidebarActive,
			&p.SidebarActiveText, &p.SidebarDivider, &p.SidebarButton, &p.Badge, &p.BadgeText:
			SetTok(t, f.f(p), f.tok, true)
		}
	}
}
