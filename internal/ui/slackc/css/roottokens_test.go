package css

import (
	"image/color"
	"testing"
)

const themeSel = ":root,html,body,.sk-client-theme--dark,.sk-client-theme--light"

func TestRootTokensNoSheets(t *testing.T) {
	e := NewEngine()
	tk := e.RootTokens(Env{Dark: true})
	if tk.Overridden() || tk.Key() != 0 {
		t.Fatal("empty engine reports overrides")
	}
	c, own, ok := tk.Color("var(--dt_color-base-pry)")
	if !ok || own || c != (color.NRGBA{0x1a, 0x1d, 0x21, 0xff}) {
		t.Fatalf("base-pry = %v own=%v ok=%v, want Slack's #1a1d21", c, own, ok)
	}
	if _, ok := tk.Background("p-theme_background"); ok {
		t.Fatal("background without sheets")
	}
}

func TestRootTokensTheme(t *testing.T) {
	e := NewEngine()
	// Nord's shape: palette ramp + vars in Slick's one rule, raw css after.
	e.Set("slick:theme", themeSel+"{--dt_color-plt-aubergine-0:46,52,64 !important;--dt_color-base-pry:#2e3440 !important;--sk_primary_background:46,52,64 !important}\n"+
		".p-theme_background{background:#2e3440 !important;}\n"+
		".p-view_contents--sidebar{background:linear-gradient(#000,#111),#3b4252 !important;--dt_color-content-pry:#aa0000}")
	tk := e.RootTokens(Env{Dark: true})
	if !tk.Overridden() || tk.Key() == 0 {
		t.Fatal("theme not seen")
	}
	want := color.NRGBA{0x2e, 0x34, 0x40, 0xff}
	for _, expr := range []string{"var(--dt_color-base-pry)", "rgb(var(--sk_primary_background))", "rgba(var(--dt_color-plt-aubergine-0),1)"} {
		if c, own, ok := tk.Color(expr); !ok || !own || c != want {
			t.Errorf("%s = %v own=%v ok=%v, want %v", expr, c, own, ok, want)
		}
	}
	// the override propagates through Slack's own var() chain
	// (theme-base-inv-pry is rgba(var(--dt_color-plt-aubergine-0), 1))
	if c, own, ok := tk.Color("var(--dt_color-theme-base-inv-pry)"); !ok || !own || c != want {
		t.Errorf("theme-base-inv-pry = %v own=%v ok=%v", c, own, ok)
	}
	// an untouched token keeps Slack's value and own=false
	if c, own, ok := tk.Color("var(--dt_color-content-pry)"); !ok || own || c != (color.NRGBA{0xf8, 0xf8, 0xf8, 0xff}) {
		t.Errorf("content-pry = %v own=%v ok=%v", c, own, ok)
	}
	if c, ok := tk.Background("p-theme_background"); !ok || c != want {
		t.Errorf("window bg = %v %v", c, ok)
	}
	if c, ok := tk.Background("p-view_contents p-view_contents--sidebar"); !ok || c != (color.NRGBA{0x3b, 0x42, 0x52, 0xff}) {
		t.Errorf("sidebar bg = %v %v", c, ok)
	}
	sb := tk.Scope("p-view_contents p-view_contents--sidebar")
	if c, own, ok := sb.Color("var(--dt_color-content-pry)"); !ok || !own || c != (color.NRGBA{0xaa, 0, 0, 0xff}) {
		t.Errorf("sidebar-scoped content-pry = %v own=%v ok=%v", c, own, ok)
	}
	if sb2 := tk.Scope("p-view_header"); sb2 != tk {
		t.Error("a scope nothing targets should be the root tokens")
	}
	// light mode: the .sk-client-theme--light branch of the selector matches too
	if c, own, ok := e.RootTokens(Env{}).Color("var(--dt_color-base-pry)"); !ok || !own || c != want {
		t.Errorf("light base-pry = %v own=%v ok=%v", c, own, ok)
	}
}

func TestRootTokensUserCSSOverridesTheme(t *testing.T) {
	e := NewEngine()
	e.Set("slick:theme", themeSel+"{--dt_color-base-pry:#2e3440 !important;--dt_color-content-pry:#eceff4 !important}")
	// user.css comes after the theme: the same selector with !important wins
	e.Set("slick:user", themeSel+"{--dt_color-base-pry:#ff00ff !important}\n:root{--dt_color-content-pry:#00ff00}")
	tk := e.RootTokens(Env{Dark: true})
	if c, _, _ := tk.Color("var(--dt_color-base-pry)"); c != (color.NRGBA{0xff, 0, 0xff, 0xff}) {
		t.Errorf("base-pry = %v, want user.css #ff00ff", c)
	}
	// a normal :root declaration loses to the theme's !important
	if c, _, _ := tk.Color("var(--dt_color-content-pry)"); c != (color.NRGBA{0xec, 0xef, 0xf4, 0xff}) {
		t.Errorf("content-pry = %v, want the theme's #eceff4", c)
	}
	e.Remove("slick:theme")
	if c, own, _ := e.RootTokens(Env{Dark: true}).Color("var(--dt_color-content-pry)"); !own || c != (color.NRGBA{0, 0xff, 0, 0xff}) {
		t.Errorf("content-pry without theme = %v own=%v, want user's #00ff00", c, own)
	}
}

func TestParseColorTriple(t *testing.T) {
	for in, want := range map[string]color.NRGBA{
		"46,52,64":             {46, 52, 64, 0xff},
		" 0, 0, 0 ":            {0, 0, 0, 0xff},
		"#eceff414":            {0xec, 0xef, 0xf4, 0x14},
		"rgba(129,161,193,.4)": {129, 161, 193, 102},
		"red":                  {0xff, 0, 0, 0xff},
	} {
		if c, ok := ParseColor(in); !ok || c != want {
			t.Errorf("ParseColor(%q) = %v %v, want %v", in, c, ok, want)
		}
	}
	for _, in := range []string{"", "var(--x)", "1px solid", "12px"} {
		if _, ok := ParseColor(in); ok {
			t.Errorf("ParseColor(%q) ok", in)
		}
	}
}
