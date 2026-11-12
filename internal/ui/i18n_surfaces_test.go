package ui

import (
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The menus, dialogs and search surfaces (docs/I18N.md, pass 2a): each is
// opened in en-US and again in Slack's es-ES and ja-JP bundles (built
// fresh, as its labels are translated when it's made), drawn through
// theme.DrawHook and compared like the main surfaces (englishLeft): a
// string Slack translates that comes out unchanged fails.

// i18nSurface is one surface: open shows it in the active locale.
type i18nSurface struct {
	name string
	open func(e *perfEnv)
}

// surfacesUnkeyed are strings these surfaces draw unchanged that are fine
// that way:
//   - words of the fake account's messages, which search results draw one
//     by one (Slack keys too: emoji keywords and the like);
//   - "banner", a word of Slack's own es-ES "Verás un banner…" (the
//     Advanced settings modal draws its descriptions word by word);
//   - "Tab", the switcher footer's key chip: Slack's label is the
//     literal "Tab" (es-ES translates the word only in other components).
//
// mainUnkeyed (the account's custom section names) counts as well.
var surfacesUnkeyed = map[string]bool{
	"build": true, "everyone": true, "from": true, "has": true, "new": true, "notes": true, "sync": true, "thanks": true,
	"banner": true,
	"Tab":    true,
}

// surfaceMessage is ch's first loaded message (the menus and dialogs
// that act on one).
func surfaceMessage(t *testing.T, e *perfEnv, ch string) core.Message {
	t.Helper()
	c := e.a.data.core
	if tl := c.S.Timeline(ch); tl != nil && len(tl.Msgs) > 0 {
		return c.MessageFromSlack(ch, &tl.Msgs[len(tl.Msgs)-1])
	}
	t.Fatalf("no message loaded in %s", ch)
	return core.Message{}
}

// i18nSurfaces are pass 2a's surfaces for channel ch.
func i18nSurfaces(t *testing.T, e *perfEnv, ch string) []i18nSurface {
	m := surfaceMessage(t, e, ch)
	at := image.Pt(700, 300)
	section := ""
	for _, s := range e.a.model().Sections {
		if s.Type == "channels" || section == "" {
			section = s.ID
		}
	}
	return []i18nSurface{
		{"message-menu", func(e *perfEnv) { e.a.ShowOverlay(popups.NewMessageMenu(e.a, at, m)) }},
		{"message-context-menu", func(e *perfEnv) {
			e.a.ShowOverlay(popups.NewMessageContextMenu(e.a, at, m, popups.InChannel, popups.MessageMenuOpts{}))
		}},
		{"channel-menu", func(e *perfEnv) { e.a.ShowOverlay(popups.NewChannelMenu(e.a, image.Pt(1400, 60), ch)) }},
		{"sidebar-menu", func(e *perfEnv) { e.a.openSidebarMenu(ch, image.Pt(140, 300), slackc.JSFunc{}) }},
		{"section-menu", func(e *perfEnv) { e.a.openSectionMenu(section, image.Pt(140, 200)) }},
		{"section-context-menu", func(e *perfEnv) { e.a.openSectionContextMenu(section, image.Pt(140, 200)) }},
		{"bell-menu", func(e *perfEnv) { e.a.ShowOverlay(popups.NewChannelNotifyMenu(e.a, image.Pt(1320, 80), ch)) }},
		{"notify-modal", func(e *perfEnv) { e.a.ShowOverlay(popups.NewChannelNotifyModal(e.a, ch)) }},
		{"forward-dialog", func(e *perfEnv) { e.a.ShowOverlay(popups.NewForwardModal(e.a, m)) }},
		{"delete-dialog", func(e *perfEnv) { popups.ConfirmDelete(e.a, m) }},
		{"emoji-picker", func(e *perfEnv) { e.a.ShowOverlay(popups.NewReactionPicker(e.a, at, ch, m.TS, "", nil)) }},
		{"status-dialog", func(e *perfEnv) { e.a.ShowOverlay(popups.NewSetStatusModal(e.a)) }},
		{"team-menu", func(e *perfEnv) { e.a.ShowOverlay(popups.NewTeamMenu(e.a, image.Pt(20, 40))) }},
		{"invite-modal", func(e *perfEnv) { popups.OpenInviteModal(e.a, popups.InviteFromTeamMenu) }},
		{"user-menu", func(e *perfEnv) { e.a.ShowOverlay(popups.NewUserMenu(e.a, image.Pt(20, 860))) }},
		{"switcher", func(e *perfEnv) { e.a.openSwitcher() }},
		{"search-page", func(e *perfEnv) { e.a.Navigate("search?q=" + url.QueryEscape("the")) }},
		// JoinChannelPane (archives): the classic pane, the revamped card
		// (channel_join_experience_with_agents) and the caret's "Join…"
		{"join-pane", func(e *perfEnv) {
			e.a.Core().SetExperimentGroup(core.ExpChannelJoinWithAgents, "")
			e.openChannel(fakeslack.PreviewChannelID)
		}},
		{"join-card", func(e *perfEnv) {
			e.a.Core().SetExperimentGroup(core.ExpChannelJoinWithAgents, "on")
			e.openChannel(fakeslack.PreviewChannelID)
		}},
		{"join-menu", func(e *perfEnv) {
			e.a.Core().SetExperimentGroup(core.ExpChannelJoinWithAgents, "on")
			e.openChannel(fakeslack.PreviewChannelID)
			c := e.a.convoFor(fakeslack.PreviewChannelID, "")
			if c.jp == nil {
				c.jp = &joinPane{}
			}
			e.a.openTempJoinMenu(c)
		}},
	}
}

// closeSurface closes every overlay and goes back to the channel.
func closeSurface(e *perfEnv, ch string) {
	for e.a.dismissTop() {
	}
	if !strings.HasPrefix(string(e.a.router.Current()), "c/"+ch) {
		e.openChannel(ch)
	}
	e.frame()
}

// checkSurfaces runs the "no English left" comparison over the surfaces
// (TestMainNoEnglishLeft).
func checkSurfaces(t *testing.T, e *perfEnv, cats map[string]*i18n.Catalog, ch string) {
	t.Helper()
	extra := map[string]bool{}
	for _, m := range []map[string]bool{mainUnkeyed, surfacesUnkeyed} {
		for k := range m {
			extra[k] = true
		}
	}
	for _, s := range i18nSurfaces(t, e, ch) {
		draw := func() []string {
			s.open(e)
			got := drawnStrings(e, 400*time.Millisecond)
			closeSurface(e, ch)
			return got
		}
		i18n.Use(i18n.EnUS, nil)
		en := draw()
		for _, loc := range []string{"es-ES", "ja-JP"} {
			i18n.Use(loc, cats[loc])
			got := draw()
			if u, _ := englishLeft(cats[loc], en, got, extra); len(u) > 0 {
				reportLeft(t, loc+" "+s.name, u, nil)
			}
		}
	}
	i18n.Use(i18n.EnUS, nil)
}

// shotSurfaces saves one screenshot per surface (TestLocaleShots, in the
// active locale).
func shotSurfaces(t *testing.T, e *perfEnv, dir, loc, ch string) {
	t.Helper()
	for e.a.dismissTop() {
	}
	for _, s := range i18nSurfaces(t, e, ch) {
		s.open(e)
		e.settle(600 * time.Millisecond)
		e.shotTo(t, dir, "i18n-"+loc+"-"+s.name)
		closeSurface(e, ch)
	}
}
