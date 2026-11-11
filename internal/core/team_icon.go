package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The rail's workspace button, as Slack picks it (docs/SIDEBAR-RULES.md, "Rail"):
//
//   - qxQQ: on Enterprise Grid the current team is the org; Slack shows the
//     workspace's icon instead when the workspace's team pref
//     use_workspace_icon_for_single_workspace_users is on and the user is
//     in exactly one workspace (users.info enterprise_user.teams).
//   - qVVL BaseTeamIcon + wqqh getIconSizeToFit: the image_N closest to
//     twice the 36px button (the smallest N >= 72, so image_88), or the
//     team's initials when icon.image_default is set.
//   - freshness: Slack's RTM handler teamIconChanged (boot-render) stores
//     team_icon_change's icon for team_id || source_team || the current
//     team. Hack Club's icon rotates (an app changes it every ~30 minutes),
//     so with live updates off Clack refreshes it with team.info every
//     TeamIconRefresh while polling.

// TeamIconRefresh is how often the poll loop re-reads the team icon while
// the realtime socket is off or down.
var TeamIconRefresh = 10 * time.Minute

// railIconPx is the workspace button's size (RmBj: 36, 24 when narrow).
const railIconPx = 36

type teamIconState struct {
	icons     map[string]json.RawMessage // team id -> icon object
	names     map[string]string          // team id -> name (team.info)
	useWS     bool                       // use_workspace_icon_for_single_workspace_users
	prefKnown bool
	lastFetch time.Time
}

// pickTeamIcon is getIconImage(icon, 2*size): the smallest image_N >= 2*size
// (the largest if none is), "" when the team has the default icon.
func pickTeamIcon(raw json.RawMessage, size int) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if d, _ := m["image_default"].(bool); d {
		return ""
	}
	var sizes []int
	for k, v := range m {
		if s, ok := v.(string); ok && s != "" && strings.HasPrefix(k, "image_") {
			if n, err := strconv.Atoi(strings.TrimPrefix(k, "image_")); err == nil && n > 0 {
				sizes = append(sizes, n)
			}
		}
	}
	if len(sizes) == 0 {
		return ""
	}
	sort.Ints(sizes)
	want, pick := 2*size, sizes[len(sizes)-1]
	for i := len(sizes) - 1; i >= 0; i-- {
		if sizes[i] >= want {
			pick = sizes[i]
		}
	}
	u, _ := m["image_"+strconv.Itoa(pick)].(string)
	return u
}

// iconTeam is the team whose icon the rail shows (qxQQ).
func (a *App) iconTeam() string {
	ws, org := a.C.Creds.Team, a.C.Creds.EnterpriseID
	if org == "" {
		a.S.RLock()
		id := a.S.TeamID
		a.S.RUnlock()
		return firstNonEmpty(id, ws)
	}
	a.mu.Lock()
	useWS := a.icon.useWS
	a.mu.Unlock()
	single := false
	if u, ok := a.S.User(a.C.Creds.UserID); ok && u.EnterpriseUser != nil {
		single = len(u.EnterpriseUser.Teams) == 1
	}
	if useWS && single && ws != "" {
		return ws
	}
	return org
}

// setTeamIcon records a team's icon object and updates the rail icon; it
// reports whether the shown icon changed.
func (a *App) setTeamIcon(team string, icon json.RawMessage) bool {
	if team == "" || len(icon) == 0 {
		return false
	}
	a.mu.Lock()
	if a.icon.icons == nil {
		a.icon.icons = map[string]json.RawMessage{}
	}
	a.icon.icons[team] = icon
	a.mu.Unlock()
	return a.applyTeamIcon()
}

func (a *App) applyTeamIcon() bool {
	team := a.iconTeam()
	a.S.RLock()
	cur := a.S.TeamID
	a.S.RUnlock()
	a.mu.Lock()
	raw, ok := a.icon.icons[team]
	if !ok { // not fetched yet: the current team's (team.info at boot)
		raw, ok = a.icon.icons[cur]
	}
	a.mu.Unlock()
	if !ok {
		return false
	}
	u := pickTeamIcon(raw, railIconPx)
	a.S.Lock()
	changed := a.S.TeamIcon != u
	a.S.TeamIcon = u
	a.S.Unlock()
	return changed
}

// refreshTeamIcon reads the icon (and, once, the team pref) and tells the UI
// when it changed.
func (a *App) refreshTeamIcon(ctx context.Context) {
	a.mu.Lock()
	known := a.icon.prefKnown
	a.icon.lastFetch = time.Now()
	a.mu.Unlock()
	if !known && a.C.Creds.EnterpriseID != "" && a.C.Creds.Team != "" {
		var r struct {
			Prefs struct {
				UseWS *bool `json:"use_workspace_icon_for_single_workspace_users"`
			} `json:"prefs"`
		}
		if err := a.C.Call(ctx, "team.prefs.get", nil, &r); err == nil {
			a.mu.Lock()
			a.icon.useWS = r.Prefs.UseWS != nil && *r.Prefs.UseWS
			a.icon.prefKnown = true
			a.mu.Unlock()
		}
	}
	team := a.iconTeam()
	changed := false
	// Slackbot custom responses show the org (customResponseTeam); when the
	// rail shows the workspace instead, read the org's name and icon once.
	if org := a.C.Creds.EnterpriseID; org != "" && org != team {
		a.mu.Lock()
		_, known := a.icon.icons[org]
		a.mu.Unlock()
		if !known {
			changed = a.fetchTeam(ctx, org)
		}
	}
	changed = a.fetchTeam(ctx, team) || changed
	if changed {
		a.emit(Event{Type: "team"})
	}
}

// fetchTeam reads a team's name and icon with team.info (team "" is the
// session's own) and reports whether the rail icon changed.
func (a *App) fetchTeam(ctx context.Context, team string) bool {
	var r struct {
		Team struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Icon json.RawMessage `json:"icon"`
		} `json:"team"`
	}
	var p url.Values
	if team != "" {
		p = url.Values{"team": {team}}
	}
	if err := a.C.Call(ctx, "team.info", p, &r); err != nil {
		return false
	}
	a.setTeamName(firstNonEmpty(r.Team.ID, team), r.Team.Name)
	changed := a.setTeamIcon(firstNonEmpty(r.Team.ID, team), r.Team.Icon)
	if team != "" && r.Team.ID != team {
		a.setTeamName(team, r.Team.Name)
		changed = a.setTeamIcon(team, r.Team.Icon) || changed
	}
	return changed
}

// setTeamName records a team's name (team.info, team_rename).
func (a *App) setTeamName(team, name string) {
	if team == "" || name == "" {
		return
	}
	a.mu.Lock()
	if a.icon.names == nil {
		a.icon.names = map[string]string{}
	}
	a.icon.names[team] = name
	a.mu.Unlock()
}

// customResponseTeam is the team Slack shows as the sender of a Slackbot
// custom response: BaseMessageAvatar/MessageSender read selfTeamIds
// (3+gS Kc: enterpriseId || teamId), so the org on Enterprise Grid.
func (a *App) customResponseTeam() string {
	if org := a.C.Creds.EnterpriseID; org != "" {
		return org
	}
	a.S.RLock()
	id := a.S.TeamID
	a.S.RUnlock()
	return firstNonEmpty(id, a.C.Creds.Team)
}

// CustomResponseTeam is who Slack shows as the sender of a Slackbot
// custom response (Message.CustomResponse; Slack's client stopped reading
// reskin_custom_responses and always draws the team): the team's name and
// its icon for an avatar of px pixels (BaseTeamIcon: the image_N for 2*px; "" for the
// default icon, which Slack draws as initials). Until the org has been
// read it answers with the workspace's.
func (a *App) CustomResponseTeam(px int) (name, icon string) {
	team := a.customResponseTeam()
	a.S.RLock()
	cur, curName := a.S.TeamID, a.S.TeamName
	a.S.RUnlock()
	a.mu.Lock()
	name = a.icon.names[team]
	raw, have := a.icon.icons[team]
	if !have {
		raw, have = a.icon.icons[cur]
	}
	a.mu.Unlock()
	if name == "" {
		name = curName
	}
	if have {
		icon = pickTeamIcon(raw, px)
	}
	return name, icon
}

// teamIconDue reports whether the poll loop should re-read the icon.
func (a *App) teamIconDue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Since(a.icon.lastFetch) >= TeamIconRefresh
}

// onTeamIconChange is teamIconChanged: team_icon_change carries the new
// icon object for team_id, else source_team, else the current team.
func (a *App) onTeamIconChange(raw json.RawMessage) {
	var e struct {
		TeamID     string          `json:"team_id"`
		SourceTeam string          `json:"source_team"`
		Icon       json.RawMessage `json:"icon"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	team := firstNonEmpty(e.TeamID, e.SourceTeam, a.iconTeam())
	if a.setTeamIcon(team, e.Icon) {
		a.emit(Event{Type: "team"})
	}
}
