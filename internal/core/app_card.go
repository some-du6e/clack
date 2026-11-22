package core

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// AppCard is what Slack's AppHoverCard (AppProfileCard, boot-render
// f0+8) shows for an app or a bot user: apps.profile.get's app_profile as
// Slack's formatAppProfileFromApiResponse (ToML) maps it.
type AppCard struct {
	AppID, BotID string
	Name         string // config.real_name || config.username || name
	Tagline      string // config.descriptive_label || desc
	Color        string // app_card_color ("" → Slack's #2C2D30)
	Icon         string // image_192 (config.icons || icons)
	AppUserID    string // bot_user.id (app_user_id for xoxa apps)
	InstallerID  string // config.created_by || auth.created_by
	Active       bool   // installation.is_active: not deleted, not revoked
	AppType      string // xoxp | xoxa | slack | custom_integration
	Workflow     bool
	Published    bool // is_directory_published
	Distributed  bool
	HomeTeamID   string // team_id (home_workspace_id)
	HomeOrgID    string // enterprise_id (home_enterprise_id)
	External     bool   // isAppExternal: the app's top-level team isn't the viewer's
	Commands     []AppCommand
	// ShownAsAgent is legacy_app.is_shown_as_agent (Slack keeps the raw
	// app_profile as legacy_app): isAppShownAsAgent, the AGENT badge's
	// app rule (badge.go).
	ShownAsAgent bool
	// AgentApp is legacy_app.is_agent_app (SM2u isAgentAppProfile).
	AgentApp bool
	// ActiveAgentApp is SM2u isActiveAgentAppProfile: is_agent_app, and
	// is_shown_as_agent true or missing.
	ActiveAgentApp bool

	// What the App details flexpane reads (popups.AppDetailsView): the raw
	// desc (the header's line under the name), long_desc (ToML's
	// profile.description, the Description section), developer_name
	// ("by …"), bot_user.memberships_count (installation.memberships_count:
	// "Active in N channels and DMs"), app_home_metadata's
	// messages_tab_enabled (supportsDmInteraction, default on) and
	// home_tab_enabled (shouldShowAppHome), and is_installed (nil when the
	// API leaves it out).
	// IconDefault: the app has no icon of its own (no image_N, icons
	// is_default, or Slack's default_new_app_icon URL), so Slack's
	// getAppIcon falls back to its bundled default_new_app_icon-936d613.png
	// and Clack draws its bundled copy instead of fetching one.
	IconDefault bool

	Desc, LongDesc, Developer string
	Memberships               int
	MessagesTab, HomeTab      bool
	Installed                 *bool
}

// TopTeam is the app's top-level team (getAppTopLevelTeamId:
// home_enterprise_id || home_workspace_id).
func (c AppCard) TopTeam() string { return firstNonEmpty(c.HomeOrgID, c.HomeTeamID) }

type appCardEntry struct {
	card AppCard
	err  error
	at   time.Time
	wait chan struct{}
	// last is the card a refetch replaces, kept while it runs and after
	// it fails: Slack's stored app profile (+MzB T, 2MXT v ?? gvD6 sO)
	// stays while it refetches, so what reads it (the Tasks tab rule)
	// doesn't see an app that stopped being an agent for a moment
	last    AppCard
	hasLast bool
	// app is the app the fetch asked for with the bot (the user's
	// api_app_id), so an app event finds an entry whose fetch failed
	app string
}

// appCardAttempt is one apps.profile.get's limit and appCardTries how
// many a fetch makes while they time out (UnknownFetchManager's
// fetchWithRetries, 0mwJ: 65 s, three attempts; any other error ends it).
var appCardAttempt = 65 * time.Second

const appCardTries = 3

// AppCardSettled is CachedAppCard for a reader that must not start a
// fetch on every frame (the Tasks tab rule): known also holds, with no
// card, for an app whose fetch failed, which Slack keeps as non-existent
// until a slow reconnect or an app event marks it stale.
func (a *App) AppCardSettled(userID, botID string) (card AppCard, ok, known bool) {
	k := a.appCardKey(userID, botID)
	c := a.appCards()
	c.mu.Lock()
	defer c.mu.Unlock()
	a.appCardsFreshLocked(c)
	e := c.m[k]
	switch {
	case e == nil:
		return AppCard{}, false, false
	case e.wait != nil:
		return e.last, e.hasLast, e.hasLast
	case e.err != nil:
		return AppCard{}, false, true
	}
	return e.card, true, true
}

type appCardCache struct {
	mu sync.Mutex
	m  map[string]*appCardEntry
	// epoch is the slow connection count the cards were read in
	// (_maybeResetState's freshness epoch: 1 before any connect)
	epoch int
}

var appCardIndex = newPerApp() // *App → *appCardCache

// appCardTTL is how long a fetched card is reused.
const appCardTTL = 10 * time.Minute

func (a *App) appCards() *appCardCache {
	v, _ := appCardIndex.LoadOrStore(a, &appCardCache{m: map[string]*appCardEntry{}})
	return v.(*appCardCache)
}

// appCardsFreshLocked is _maybeResetState for the cards (p7oS, which the
// apps refetcher FftE runs with no max age and the default slow-reconnect
// rule): a slow connection count other than the one the cards were read
// in marks them all stale ("Connection count a -> n: marking all as
// stale"). A fetch in flight runs its course. c.mu is held; it reports
// whether any was marked.
func (a *App) appCardsFreshLocked(c *appCardCache) bool {
	n := max(int(a.slowConnectionCount().Load()), 1)
	if c.epoch == 0 {
		c.epoch = 1
	}
	if n == c.epoch {
		return false
	}
	c.epoch = n
	marked := false
	for k, e := range c.m {
		if e.wait == nil {
			staleAppCardLocked(c, k, e)
			marked = true
		}
	}
	return marked
}

// staleAppCardLocked marks entry k stale (markObjectsAsStale): an app
// remembered as non-existent is forgotten, so its next read asks again;
// a card stays for its next read to refetch (kept while that runs).
func staleAppCardLocked(c *appCardCache, k string, e *appCardEntry) {
	if e.err != nil {
		delete(c.m, k)
		return
	}
	e.at = time.Time{}
}

// appCardsReconnected runs the cards' reset at a slow reconnect
// (socketConnected), so what reads them (the Tasks tab rule) reads again
// at once: an "app_card" event when any was marked stale.
func (a *App) appCardsReconnected() {
	c := a.appCards()
	c.mu.Lock()
	marked := a.appCardsFreshLocked(c)
	c.mu.Unlock()
	if marked {
		a.emit(Event{Type: "app_card"})
	}
}

// onAppCardEvent is boot-render's app events for the cards: apps_installed
// and apps_changed (sharedUpsertAppFromImsg: markObjectsAsStale of
// event.app.id), apps_uninstalled (sharedAppUninstalled:
// markAsStaleByAppId of event.app_id) and bot_changed (botChanged: of
// event.bot.app_id, or the bot). Each card of that app or bot is marked
// stale, and an "app_card" event lets what reads them read again.
func (a *App) onAppCardEvent(typ string, raw json.RawMessage) {
	var e struct {
		App *struct {
			ID string `json:"id"`
		} `json:"app"`
		AppID string `json:"app_id"`
		Bot   *struct {
			ID    string `json:"id"`
			AppID string `json:"app_id"`
		} `json:"bot"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	var app, bot string
	switch typ {
	case "apps_installed", "apps_changed":
		if e.App != nil {
			app = e.App.ID
		}
	case "apps_uninstalled":
		app = e.AppID
	case "bot_changed":
		if e.Bot != nil {
			app, bot = e.Bot.AppID, e.Bot.ID
		}
	}
	if app == "" && bot == "" {
		return
	}
	c := a.appCards()
	c.mu.Lock()
	marked := false
	for k, en := range c.m {
		if en.wait != nil {
			continue
		}
		if app != "" && (en.app == app || en.card.AppID == app) || bot != "" && (k == "B:"+bot || en.card.BotID == bot) {
			staleAppCardLocked(c, k, en)
			marked = true
		}
	}
	c.mu.Unlock()
	if marked {
		a.emit(Event{Type: "app_card"})
	}
}

// CachedAppCard returns a card fetched before (AppCardOf) for the bot
// user userID or the bot botID.
func (a *App) CachedAppCard(userID, botID string) (AppCard, bool) {
	k := a.appCardKey(userID, botID)
	c := a.appCards()
	c.mu.Lock()
	defer c.mu.Unlock()
	a.appCardsFreshLocked(c)
	e, ok := c.m[k]
	if !ok {
		return AppCard{}, false
	}
	if e.wait != nil || e.err != nil {
		return e.last, e.hasLast
	}
	return e.card, true
}

func (a *App) appCardKey(userID, botID string) string {
	if botID == "" && userID != "" {
		if u, ok := a.S.User(userID); ok {
			botID = u.Profile.BotID
		}
	}
	if botID != "" {
		return "B:" + botID
	}
	return "U:" + userID
}

// ErrNotAnApp is AppCardOf's answer for a member that has no bot.
var ErrNotAnApp = errors.New("not an app")

// AppCardOf reads the app card of the bot user userID or the bot botID
// (apps.profile.get bot=…&app=…), cached for appCardTTL. Concurrent
// callers share one fetch, which runs on its own clock (runAppCardFetch):
// a caller whose context ends stops waiting without ending it for the
// others. An app whose fetch failed is Slack's non-existent app: every
// caller gets the error without a request until a slow reconnect or an
// app event marks it stale.
func (a *App) AppCardOf(ctx context.Context, userID, botID string) (AppCard, error) {
	appID := ""
	if userID != "" {
		if _, ok := a.S.User(userID); !ok {
			a.EnsureUsers(ctx, []string{userID})
		}
		if u, ok := a.S.User(userID); ok {
			botID = firstNonEmpty(botID, u.Profile.BotID)
			appID = u.Profile.APIAppID
		}
	}
	k := a.appCardKey(userID, botID)
	c := a.appCards()
	c.mu.Lock()
	a.appCardsFreshLocked(c)
	if botID == "" && appID == "" { // no app to ask for (getAppById answers false): known, never fetched
		if e, ok := c.m[k]; !ok || e.wait == nil {
			c.m[k] = &appCardEntry{err: ErrNotAnApp, at: time.Now()}
		}
		c.mu.Unlock()
		return AppCard{}, ErrNotAnApp
	}
	e := c.m[k]
	switch {
	case e == nil || e.wait == nil && e.err == nil && time.Since(e.at) >= appCardTTL:
		ne := &appCardEntry{wait: make(chan struct{}), app: appID}
		if e != nil { // a stale card: kept while this one is fetched
			ne.last, ne.hasLast = e.card, true
		}
		c.m[k] = ne
		e = ne
		go a.runAppCardFetch(c, e, botID, appID, userID)
	case e.wait == nil && e.err != nil: // non-existent until it is marked stale
		c.mu.Unlock()
		return AppCard{}, e.err
	case e.wait == nil:
		c.mu.Unlock()
		return e.card, nil
	}
	w := e.wait
	c.mu.Unlock()
	select {
	case <-w:
	case <-ctx.Done():
		return AppCard{}, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return e.card, e.err
}

// runAppCardFetch is UnknownFetchManager.fetchWithRetries for one card,
// on the App's context: each apps.profile.get gets appCardAttempt, one
// that times out is sent again up to appCardTries in all, and any other
// error ends the fetch. A fetch that fails keeps the card it refetched
// (Slack's refetcher swallows the error and marks the app fresh); with no
// card the app is non-existent (upsertNonExistent, marked fresh with no
// max age) until it is marked stale. Every attempt goes past the client's
// failure backoff (slack.NoBackoff): the card's own rule decides when to
// ask (a first read, a read after a reset, a stale card), and the guard
// would answer the read after a reset with the failure it just forgot.
func (a *App) runAppCardFetch(c *appCardCache, e *appCardEntry, botID, appID, userID string) {
	var card AppCard
	var err error
	for try := 1; try <= appCardTries; try++ {
		actx, cancel := context.WithTimeout(slack.NoBackoff(a.bg), appCardAttempt)
		card, err = a.fetchAppCard(actx, botID, appID, a.botHomeTeam(botID, userID))
		cancel()
		if !appCardTimedOut(err) {
			break
		}
	}
	if err == nil && card.AppUserID == "" {
		card.AppUserID = userID
	}
	c.mu.Lock()
	e.at = time.Now()
	switch {
	case err == nil:
		e.card, e.err = card, nil
	case e.hasLast:
		e.card, e.err = e.last, nil
	default:
		e.card, e.err = AppCard{}, err
	}
	e.last, e.hasLast = AppCard{}, false
	close(e.wait)
	e.wait = nil
	c.mu.Unlock()
}

// appCardTimedOut is fetchWithRetries' TimeoutError test: the attempt ran
// out of time, its own limit or the request's (the client's "timeout").
func appCardTimedOut(err error) bool {
	var ne net.Error
	return err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout())
}

func (a *App) fetchAppCard(ctx context.Context, botID, appID, home string) (AppCard, error) {
	p := url.Values{}
	if botID != "" {
		p.Set("bot", botID)
		if home != "" { // fcgE fetchProfileViaApi: bot_home_team, the homeWorkspaceId param
			p.Set("bot_home_team", home)
		}
	}
	if appID != "" {
		p.Set("app", appID)
	}
	var res struct {
		AppProfile json.RawMessage `json:"app_profile"`
	}
	if err := a.C.Call(ctx, "apps.profile.get", p, &res); err != nil {
		return AppCard{}, err
	}
	return a.appCardOf(res.AppProfile, botID), nil
}

// botHomeTeam is the homeWorkspaceId Slack's hover card asks for the app
// with (useProfileHoverApp → rIIG getTeamIdFromBotOrUser): the bot's
// team_id when it is a workspace's, else the member's (getTeamIdFromBotAppUser:
// its team_id when a workspace's), else the viewer's workspace. Without it
// apps.profile.get answers a bot the member's profile names but bots.info
// no longer finds (most of Hack Club's #new-bots) as the org's app, not
// installed; with it, as the workspace's install.
func (a *App) botHomeTeam(_, userID string) string {
	ws := func(id string) bool { return strings.HasPrefix(id, "T") }
	// (the store keeps no team for a bot; a message's bot_profile.team_id
	// is the viewer's workspace for every bot seen so far)
	if userID != "" {
		if u, ok := a.S.User(userID); ok && ws(u.TeamID) {
			return u.TeamID
		}
	}
	return firstNonEmpty(a.C.Creds.Team, a.S.TeamID)
}

// appCardOf maps app_profile like Slack's ToML F.
func (a *App) appCardOf(raw json.RawMessage, botID string) AppCard {
	type icons map[string]any
	var r struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Desc          string `json:"desc"`
		Color         string `json:"app_card_color"`
		AppUserID     string `json:"app_user_id"`
		TeamID        string `json:"team_id"`
		EnterpriseID  string `json:"enterprise_id"`
		IsXoxa        bool   `json:"is_xoxa_app"`
		IsSlack       bool   `json:"is_slack_integration"`
		IsWorkflow    bool   `json:"is_workflow_app"`
		IsPublished   bool   `json:"is_directory_published"`
		IsDistributed bool   `json:"is_distributed"`
		ShownAsAgent  *bool  `json:"is_shown_as_agent"`
		AgentApp      bool   `json:"is_agent_app"`
		LongDesc      string `json:"long_desc"`
		Developer     string `json:"developer_name"`
		IsInstalled   *bool  `json:"is_installed"`
		HomeMeta      *struct {
			MessagesTab *bool `json:"messages_tab_enabled"`
			HomeTab     *bool `json:"home_tab_enabled"`
		} `json:"app_home_metadata"`
		Icons       icons           `json:"icons"`
		CommandsAny json.RawMessage `json:"commands"`
		BotUser     *struct {
			ID, Username string
			Memberships  int `json:"memberships_count"`
		} `json:"bot_user"`
		Auth *struct {
			CreatedBy string `json:"created_by"`
			Revoked   any    `json:"revoked"`
		} `json:"auth"`
		Config *struct {
			RealName            string `json:"real_name"`
			Username            string `json:"username"`
			DescriptiveLabel    string `json:"descriptive_label"`
			CreatedBy           string `json:"created_by"`
			DateDeleted         any    `json:"date_deleted"`
			IsActive            any    `json:"is_active"`
			IsCustomIntegration bool   `json:"is_custom_integration"`
			Icons               icons  `json:"icons"`
		} `json:"config"`
	}
	_ = json.Unmarshal(raw, &r)
	c := AppCard{AppID: r.ID, BotID: botID, Name: r.Name, Tagline: r.Desc, Color: strings.TrimPrefix(r.Color, "#"),
		HomeTeamID: r.TeamID, HomeOrgID: r.EnterpriseID, Workflow: r.IsWorkflow,
		Published: r.IsPublished, Distributed: r.IsDistributed, AppType: "xoxp", ShownAsAgent: r.ShownAsAgent != nil && *r.ShownAsAgent, AgentApp: r.AgentApp,
		ActiveAgentApp: r.AgentApp && (r.ShownAsAgent == nil || *r.ShownAsAgent),
		Desc:           r.Desc, LongDesc: r.LongDesc, Developer: r.Developer, Installed: r.IsInstalled, MessagesTab: true}
	if m := r.HomeMeta; m != nil {
		if m.MessagesTab != nil {
			c.MessagesTab = *m.MessagesTab
		}
		c.HomeTab = m.HomeTab != nil && *m.HomeTab
	}
	if r.BotUser != nil {
		c.Memberships = r.BotUser.Memberships
	}
	switch {
	case r.IsXoxa:
		c.AppType = "xoxa"
	case r.Config != nil && r.Config.IsCustomIntegration:
		c.AppType = "custom_integration"
	case r.IsSlack:
		c.AppType = "slack"
	}
	ic := r.Icons
	c.Active = false
	if cf := r.Config; cf != nil {
		c.Active = true
		if n := jsonInt(cf.DateDeleted); n != 0 {
			c.Active = false
		}
		if cf.IsActive != nil && jsonInt(cf.IsActive) < 1 {
			c.Active = false
		}
		c.InstallerID = cf.CreatedBy
		c.Name = firstNonEmpty(cf.RealName, cf.Username, c.Name)
		c.Tagline = firstNonEmpty(cf.DescriptiveLabel, c.Tagline)
		if len(cf.Icons) > 0 {
			ic = cf.Icons
		}
	} else if r.Auth != nil {
		c.Active = !jsonTrue(r.Auth.Revoked)
	}
	if r.Auth != nil && r.Auth.CreatedBy != "" {
		c.InstallerID = r.Auth.CreatedBy
	}
	switch {
	case r.IsXoxa && r.AppUserID != "":
		c.AppUserID = r.AppUserID
	case r.BotUser != nil && r.BotUser.ID != "":
		c.AppUserID = r.BotUser.ID
	}
	for _, k := range []string{"image_192", "image_128", "image_96", "image_72", "image_64", "image_48"} {
		if s, ok := ic[k].(string); ok && s != "" {
			c.Icon = s
			break
		}
	}
	c.IconDefault = c.Icon == "" || jsonTrue(ic["is_default"]) || strings.Contains(c.Icon, "default_new_app_icon")
	// commands: Slack's API sends an object keyed by name or an array
	var arr []struct{ Name, Desc, Usage string }
	if json.Unmarshal(r.CommandsAny, &arr) != nil {
		var obj map[string]struct{ Desc, Usage string }
		if json.Unmarshal(r.CommandsAny, &obj) == nil {
			for n, v := range obj {
				arr = append(arr, struct{ Name, Desc, Usage string }{n, v.Desc, v.Usage})
			}
			sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })
		}
	}
	for _, cm := range arr {
		n := cm.Name
		if n != "" && !strings.HasPrefix(n, "/") {
			n = "/" + n
		}
		c.Commands = append(c.Commands, AppCommand{Name: n, Desc: cm.Desc, Usage: cm.Usage})
	}
	top := firstNonEmpty(a.C.Creds.EnterpriseID, a.C.Creds.Team)
	c.External = top != "" && c.TopTeam() != "" && c.TopTeam() != top
	return c
}

func jsonInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case bool:
		if t {
			return 1
		}
	}
	return 0
}

func jsonTrue(v any) bool { return jsonInt(v) != 0 }

// ---- external teams (Slack Connect) ----

// Team returns a team's name and its icon for a px-pixel square (Slack's
// BaseTeamIcon: the image_N for 2*px; "" for the default icon, drawn as
// initials) once EnsureTeam has read it.
func (a *App) Team(id string, px int) (name, icon string, ok bool) {
	if id == "" {
		return "", "", false
	}
	a.mu.Lock()
	name, ok = a.icon.names[id]
	raw := a.icon.icons[id]
	a.mu.Unlock()
	if raw != nil {
		icon = pickTeamIcon(raw, px)
	}
	return name, icon, ok
}

// EnsureTeam reads an unknown team's name and icon (team.info team=…).
func (a *App) EnsureTeam(ctx context.Context, id string) {
	if id == "" {
		return
	}
	if _, _, ok := a.Team(id, 20); ok {
		return
	}
	a.fetchTeam(ctx, id)
}
