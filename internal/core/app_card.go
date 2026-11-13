package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
}

type appCardCache struct {
	mu sync.Mutex
	m  map[string]*appCardEntry
}

var appCardIndex sync.Map // *App → *appCardCache

// appCardTTL is how long a fetched card is reused.
const appCardTTL = 10 * time.Minute

func (a *App) appCards() *appCardCache {
	v, _ := appCardIndex.LoadOrStore(a, &appCardCache{m: map[string]*appCardEntry{}})
	return v.(*appCardCache)
}

// CachedAppCard returns a card fetched before (AppCardOf) for the bot
// user userID or the bot botID.
func (a *App) CachedAppCard(userID, botID string) (AppCard, bool) {
	k := a.appCardKey(userID, botID)
	c := a.appCards()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || e.wait != nil || e.err != nil {
		return AppCard{}, false
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
// (apps.profile.get bot=…&app=…), cached for appCardTTL; concurrent
// callers share one request.
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
	if botID == "" && appID == "" {
		return AppCard{}, ErrNotAnApp
	}
	k := a.appCardKey(userID, botID)
	c := a.appCards()
	c.mu.Lock()
	if e, ok := c.m[k]; ok {
		if w := e.wait; w != nil {
			c.mu.Unlock()
			select {
			case <-w:
			case <-ctx.Done():
				return AppCard{}, ctx.Err()
			}
			c.mu.Lock()
			e = c.m[k]
			c.mu.Unlock()
			if e == nil {
				return AppCard{}, ErrNotAnApp
			}
			return e.card, e.err
		}
		if e.err == nil && time.Since(e.at) < appCardTTL {
			c.mu.Unlock()
			return e.card, nil
		}
	}
	e := &appCardEntry{wait: make(chan struct{})}
	c.m[k] = e
	c.mu.Unlock()

	card, err := a.fetchAppCard(ctx, botID, appID)
	if err == nil && card.AppUserID == "" {
		card.AppUserID = userID
	}
	c.mu.Lock()
	e.card, e.err, e.at = card, err, time.Now()
	close(e.wait)
	e.wait = nil
	if err != nil {
		delete(c.m, k) // retry next time
	}
	c.mu.Unlock()
	return card, err
}

func (a *App) fetchAppCard(ctx context.Context, botID, appID string) (AppCard, error) {
	p := url.Values{}
	if botID != "" {
		p.Set("bot", botID)
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
		ShownAsAgent  bool   `json:"is_shown_as_agent"`
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
		Published: r.IsPublished, Distributed: r.IsDistributed, AppType: "xoxp", ShownAsAgent: r.ShownAsAgent,
		Desc: r.Desc, LongDesc: r.LongDesc, Developer: r.Developer, Installed: r.IsInstalled, MessagesTab: true}
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
