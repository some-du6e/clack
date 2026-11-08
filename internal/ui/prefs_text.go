package ui

import (
	"image"
	"image/color"
	"regexp"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Plugin settings text that Taut draws with Slack's MrkdwnElement (the
// plugin page's "Category · by …" line, About's credits) goes through
// richview, Clack's native MrkdwnElement: links, inline code and member
// mentions look exactly like they do in messages.

// prefsText caches one richview.View per text for a settings page, and
// one richview.Style per text colour (a palette change re-records every
// View of a style, so colours don't share one).
type prefsText struct {
	views  map[string]*prefsTextView
	styles map[color.NRGBA]*prefsTextStyle
}

type prefsTextStyle struct {
	st   *richview.Style
	dark bool
}

type prefsTextView struct {
	src string
	st  *richview.Style // the style it was laid out with (Views track a style's gen, not which style)
	v   *richview.View
}

// textStyle is the richview style drawing body text in col.
func (p *tautPage) textStyle(col color.NRGBA) *richview.Style {
	t := &p.text
	th := p.a.th
	if t.styles == nil {
		t.styles = map[color.NRGBA]*prefsTextStyle{}
	}
	ps := t.styles[col]
	if ps == nil {
		var imgs richview.Images // emoji in descriptions
		if p.a.images != nil {
			imgs = p.a.images
		}
		st := richview.NewStyle(th.Shaper, imgs, richview.LightPalette())
		st.Face = theme.Typeface
		st.EmojiURL = th.EmojiURL
		ps = &prefsTextStyle{st: st, dark: !th.Dark} // forces the palette below
		t.styles[col] = ps
	}
	if ps.dark != th.Dark {
		ps.dark = th.Dark
		pal := richview.LightPalette()
		ps.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		ps.st.LightTextFade = 0
		if th.Dark {
			pal = richview.DarkPalette()
			ps.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			ps.st.LightTextFade = 0.235
		}
		pal.Text, pal.Link = col, p.colors().link
		ps.st.SetPalette(pal)
	}
	return ps.st
}

// mrkdwn draws s (Slack mrkdwn) at 13/18 (small) or 15/22 in col.
func (p *tautPage) mrkdwn(gtx layout.Context, key, s string, small bool, col color.NRGBA) layout.Dimensions {
	t := &p.text
	st := p.textStyle(col)
	if t.views == nil {
		t.views = map[string]*prefsTextView{}
	}
	tv := t.views[key]
	if tv == nil || tv.src != s || tv.st != st {
		var r richtext.Resolver
		if c := p.a.Core(); c != nil {
			r = c.S
		}
		doc := richtext.Doc{Blocks: richtext.FromMrkdwn(r, s)}
		tv = &prefsTextView{src: s, st: st, v: richview.New(doc, richview.Options{Compact: small})}
		t.views[key] = tv
	}
	return tv.v.Layout(gtx, st, prefsTextHandler{p.a})
}

// prefsTextHandler opens what settings text links to.
type prefsTextHandler struct{ a *App }

func (h prefsTextHandler) OpenURL(u string) {
	if !strings.HasPrefix(u, "slack://") {
		openURL(u)
	}
}
func (h prefsTextHandler) OpenUser(id string, anchor image.Point) {
	h.a.OpenProfile(h.a.PointerPos(), id)
}
func (h prefsTextHandler) OpenChannel(id string) {
	h.a.dismissTop() // the Preferences dialog
	h.a.Navigate("c/" + id)
}
func (prefsTextHandler) OpenUsergroup(string, image.Point) {}
func (prefsTextHandler) OpenImage(richtext.Image)          {}
func (prefsTextHandler) OpenFile(*richtext.File)           {}
func (prefsTextHandler) Button(richtext.Button)            {}

var (
	mrkdwnUserRE    = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|([^>]*))?>`)
	mrkdwnChannelRE = regexp.MustCompile(`<#(C[A-Z0-9]+)(?:\|([^>]*))?>`)
)

// tautChannels are the Hack Club channels Taut's settings link to.
var tautChannels = map[string]string{"C0A057686SF": "taut"}

// tautAuthorBySlackID finds a known Taut author by Hack Club member id.
func tautAuthorBySlackID(id string) (taut.Author, bool) {
	for _, a := range taut.KnownAuthors() {
		if a.SlackID == id {
			return a, true
		}
	}
	return taut.Author{}, false
}

// tautMrkdwnHere is Taut's rule for mrkdwn in its settings: member and
// channel mentions only resolve in the Hack Club Slack, so anywhere else
// they become the author's name (linked to their page) or a link to the
// channel on the Hack Club Slack.
func tautMrkdwnHere(s string, hackClub bool) string {
	if hackClub {
		return s
	}
	s = mrkdwnUserRE.ReplaceAllStringFunc(s, func(m string) string {
		sm := mrkdwnUserRE.FindStringSubmatch(m)
		if a, ok := tautAuthorBySlackID(sm[1]); ok {
			if a.URL != "" {
				return "<" + a.URL + "|" + a.Name + ">"
			}
			return a.Name
		}
		if sm[2] != "" {
			return sm[2]
		}
		return "a Hack Club Slack member"
	})
	return mrkdwnChannelRE.ReplaceAllStringFunc(s, func(m string) string {
		sm := mrkdwnChannelRE.FindStringSubmatch(m)
		u := "https://hackclub.slack.com/archives/" + sm[1]
		name := tautChannels[sm[1]]
		if name == "" {
			name = sm[2]
		}
		if name != "" {
			return "<" + u + "|#" + name + " on the Hack Club Slack>"
		}
		return "<" + u + "|a channel on the Hack Club Slack>"
	})
}

// tautAuthorsMrkdwn is a plugin's authors as Taut shows them: mentions in
// the Hack Club Slack, else names (linked when they have a page), joined
// with ", ".
func tautAuthorsMrkdwn(pl taut.PluginInfo, hackClub bool) string {
	if len(pl.Authors) == 0 {
		return tautMrkdwnHere(pl.AuthorsMrkdwn, hackClub)
	}
	out := make([]string, 0, len(pl.Authors))
	for _, a := range pl.Authors {
		switch {
		case hackClub && a.SlackID != "":
			out = append(out, "<@"+a.SlackID+"|"+a.Name+">")
		case a.URL != "":
			out = append(out, "<"+a.URL+"|"+mrkdwnEscape(a.Name)+">")
		default:
			out = append(out, mrkdwnEscape(a.Name))
		}
	}
	return strings.Join(out, ", ")
}

// mrkdwnEscape keeps a plain name from being read as markup.
func mrkdwnEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
