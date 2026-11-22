package core

// The eighth wave of the account's experiments (branch exp-wave-8, CDN
// build c07ab895506f, 2026-11-15; docs/EXPERIMENTS.md "wave 8"): an app
// DM's About tab is Slack's About page.
//
// BaseAppSpace (gantry-v2-async-shared-js_modern_components_WIP_tabbed-
// channels_hooks_use-tab-view_ts, module 2Fle) draws the app space's
// About tab. With agent_about_page on (Aoh1 _Z) and the app profile's
// app_id, it's about_page_AboutPage over the apps-about-info store
// (gantry-v2-async-shared-js_modern_redux_stores_apps-about-info-store_…,
// module rmod): apps.about.info {app_id, reason: "fetch-unknown-apps-
// about-info"} (EU4x appsAboutInfoFetcher), kept per app id for the
// session (storeKey appsAboutInfo, persistence off). Off, it's the older
// p-app_space_profile layout. This file is the store and what the page's
// selector (2Fle ea over rmod wN) makes of it; the page is
// ui/popups/about_page.go.

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExpAgentAboutPage is agent_about_page: BaseAppSpace's
// `"on"===(0,et._Z)(e,"agent_about_page")` picks the About page, and the
// page's selector passes it on as experimentAgentAboutPageGroupOn (the
// empty state, the permissions and the security sections draw only
// with it).
const ExpAgentAboutPage = "agent_about_page"

// ExpFlexpaneContentParity is flexpane_content_parity (HLQ9 X7, read
// here through 2Fle ea): with it the workflow collaborator check reads
// the about info's owner_ids instead of the app profile's
// legacy_app.owner_ids.
const ExpFlexpaneContentParity = "flexpane_content_parity"

// ExpRevampAgentAppProfile is revamp_agent_app_profile (HLQ9 sD): with
// it AppNotFound (gpb+) says "Agent not found" for an AI app.
const ExpRevampAgentAppProfile = "revamp_agent_app_profile"

// ExpScopeAppInstallReadsToViewerTeam is
// scope_app_install_reads_to_viewer_team: SM2u shouldShowSettingsButton
// asks isAppInstalledOnMyTeam with it on, isAppInstalled off.
const ExpScopeAppInstallReadsToViewerTeam = "scope_app_install_reads_to_viewer_team"

var wave8Reads = map[string][]string{
	ExpAgentAboutPage: on, ExpFlexpaneContentParity: on, ExpRevampAgentAppProfile: on,
	ExpScopeAppInstallReadsToViewerTeam: on,
}

var wave8Defaults = map[string]string{
	ExpAgentAboutPage: "on", ExpFlexpaneContentParity: "on", ExpRevampAgentAppProfile: "on",
	ExpScopeAppInstallReadsToViewerTeam: "on",
}

func init() {
	for name, groups := range wave8Reads {
		experimentReads[name] = groups
	}
	for name, g := range wave8Defaults {
		defaultExperimentGroups[name] = g
	}
}

// AgentAboutPage is BaseAppSpace's choice: the About tab is the About
// page (agent_about_page on).
func (a *App) AgentAboutPage() bool { return a.Experiment(ExpAgentAboutPage) == "on" }

// RevampAgentAppProfile is HLQ9 sD.
func (a *App) RevampAgentAppProfile() bool { return a.Experiment(ExpRevampAgentAppProfile) == "on" }

// ---- apps.about.info ----

// AppAboutInfo is apps.about.info's answer (what rmod keeps as
// appsAboutInfo[app_id]).
type AppAboutInfo struct {
	AppID, Name, AuthFullName string
	LongDesc, AssistantDesc   string
	Icon32, Icon512           string
	AIApp, AgentApp           bool
	ShownAsAgent              bool
	Agentforce                bool
	NativeAgent               string // native_agent.type ("" without one)
	Categories                []AppAboutCategory
	Screenshots               []AppScreenshot
	YoutubeURL                string
	SupportURL, HomeURL       string
	PrivacyURL, SupportEmail  string
	Languages                 []string // supported_languages' locale codes
	Pricing                   string   // pricing_info
	DirectoryApproved         bool
	SlackIntegration          bool
	CustomIntegration         bool
	Distributed, Published    bool
	WorkflowApp               bool
	CallsApp, OrgReadyApp     bool
	HomeTeamID, HomeOrgID     string
	ServiceID                 string
	AppUserID, AppUsername    string
	OwnerIDs                  []string
	HomeTab, MessagesTab      bool
	GlobalShortcuts           []AppShortcutRef
	MessageShortcuts          []AppShortcutRef
	SlashCommands             []AppSlashCommand
	MCPServers                []AppMCPServer
	// TokenRotation is security_token_rotation_enabled (nil: absent).
	TokenRotation *bool
	// Security is security_compliance (nil: absent or null).
	Security map[string]json.RawMessage
	// Scopes is scope_info, camel-cased the way Slack's camelCaseDeep
	// (9AQx XK) leaves it (nil: absent).
	Scopes *AppScopeInfo
}

// AppAboutCategory is one of the app's App Directory categories.
type AppAboutCategory struct{ ID, Name string }

// AppScreenshot is normalizeScreenshots' entry (both sizes present).
type AppScreenshot struct{ ID, Image440, Image1600 string }

// AppShortcutRef is a global or message shortcut of the app.
type AppShortcutRef struct{ ID, Name string }

// AppSlashCommand is a slash command as rmod maps it.
type AppSlashCommand struct{ Name, Desc, UsageHint string }

// AppMCPServer is an MCP server with at least one tool (rmod drops the
// others).
type AppMCPServer struct {
	ID, Name, URL, IconEmoji string
	Tools                    []AppMCPTool
}

// AppMCPTool is a server's tool: its name and title (title, else
// annotations.title, else name).
type AppMCPTool struct{ Name, Title string }

// AppScopeInfo is scope_info: content and actions, each a primary group.
type AppScopeInfo struct{ Content, Actions *AppScopeGroup }

// AppScopeGroup is a primary group: its heading and its secondary groups
// keyed by camel-cased name (channelActions, userContent, …).
type AppScopeGroup struct {
	Heading         string
	SecondaryGroups map[string]AppScopeSecondary
}

// AppScopeSecondary is a secondary group.
type AppScopeSecondary struct {
	Heading    string
	ScopesInfo []AppScopeEntry
}

// AppScopeEntry is one scope_info row.
type AppScopeEntry struct {
	Scopes      []string
	Description string
	TokenType   string // "bot" or "user" (Slack's default)
	Optional    bool
}

type appAboutEntry struct {
	info *AppAboutInfo // nil: non-existent (the fetch failed)
	wait chan struct{}
}

type appAboutStore struct {
	mu sync.Mutex
	m  map[string]*appAboutEntry
}

var appAboutIndex = newPerApp() // *App → *appAboutStore

func (a *App) appAbout() *appAboutStore {
	v, _ := appAboutIndex.LoadOrStore(a, &appAboutStore{m: map[string]*appAboutEntry{}})
	return v.(*appAboutStore)
}

// CachedAppAboutInfo is getAppsAboutInfo without fetching: the stored
// info, and whether the store has an answer for appID (ok with a nil
// info: the fetch failed, Slack's non-existent placeholder).
func (a *App) CachedAppAboutInfo(appID string) (info *AppAboutInfo, ok bool) {
	s := a.appAbout()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.m[appID]
	if e == nil || e.wait != nil {
		return nil, false
	}
	return e.info, true
}

// AppAboutInfoOf is getAppsAboutInfo: the store's answer for appID, or
// apps.about.info once (concurrent callers share the request). A failed
// fetch is remembered as non-existent (nil, nil), as the object fetcher
// does; ctx errors aren't remembered.
func (a *App) AppAboutInfoOf(ctx context.Context, appID string) (*AppAboutInfo, error) {
	if appID == "" {
		return nil, nil
	}
	s := a.appAbout()
	s.mu.Lock()
	if e := s.m[appID]; e != nil {
		w := e.wait
		s.mu.Unlock()
		if w != nil {
			select {
			case <-w:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		info, _ := a.CachedAppAboutInfo(appID)
		return info, nil
	}
	e := &appAboutEntry{wait: make(chan struct{})}
	s.m[appID] = e
	s.mu.Unlock()

	info, err := a.fetchAppAbout(ctx, appID)
	s.mu.Lock()
	close(e.wait)
	e.wait = nil
	if err != nil && ctx.Err() != nil {
		delete(s.m, appID)
		s.mu.Unlock()
		return nil, err
	}
	e.info = info
	s.mu.Unlock()
	return info, nil
}

func (a *App) fetchAppAbout(ctx context.Context, appID string) (*AppAboutInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := url.Values{"app_id": {appID}, "reason": {"fetch-unknown-apps-about-info"}}
	var raw json.RawMessage
	if err := a.C.Call(ctx, "apps.about.info", p, &raw); err != nil {
		return nil, err
	}
	info := ParseAppAboutInfo(raw)
	if info.AppID == "" {
		info.AppID = appID
	}
	return &info, nil
}

// ParseAppAboutInfo maps an apps.about.info answer.
func ParseAppAboutInfo(raw json.RawMessage) AppAboutInfo {
	var r struct {
		AppID             string  `json:"app_id"`
		Name              string  `json:"name"`
		AuthFullName      *string `json:"auth_full_name"`
		LongDesc          *string `json:"long_description"`
		AssistantDesc     *string `json:"assistant_description"`
		Icons             map[string]any
		IsAIApp           bool            `json:"is_ai_app"`
		IsAgentApp        bool            `json:"is_agent_app"`
		IsShownAsAgent    bool            `json:"is_shown_as_agent"`
		Agentforce        json.RawMessage `json:"agentforce"`
		NativeAgent       *struct{ Type string }
		Categories        []struct{ ID, Name any }
		Screenshots       []map[string]any
		YoutubeURL        *string         `json:"youtube_url"`
		SupportURL        *string         `json:"support_url"`
		HomeURL           *string         `json:"home_url"`
		PrivacyLink       *string         `json:"privacy_link"`
		SupportEmail      *string         `json:"support_email"`
		Languages         json.RawMessage `json:"supported_languages"`
		Pricing           *string         `json:"pricing_info"`
		DirectoryApproved bool            `json:"is_directory_approved"`
		SlackIntegration  bool            `json:"is_slack_integration"`
		CustomIntegration bool            `json:"is_custom_integration"`
		Distributed       bool            `json:"is_distributed"`
		Published         bool            `json:"is_published"`
		WorkflowApp       bool            `json:"is_workflow_app"`
		CallsApp          bool            `json:"is_calls_app"`
		OrgReadyApp       bool            `json:"is_org_ready_app"`
		HomeWorkspaceID   string          `json:"home_workspace_id"`
		HomeEnterpriseID  string          `json:"home_enterprise_id"`
		ServiceID         string          `json:"service_id"`
		AppUser           *struct {
			ID, Username string
		} `json:"app_user"`
		OwnerIDs []string `json:"owner_ids"`
		HomeMeta *struct {
			HomeTab     *bool `json:"home_tab_enabled"`
			MessagesTab *bool `json:"messages_tab_enabled"`
		} `json:"app_home_metadata"`
		Shortcuts struct {
			Global  []struct{ ID, Name string } `json:"global_shortcuts"`
			Message []struct{ ID, Name string } `json:"message_shortcuts"`
		}
		SlashCommands []struct {
			Name, Description string
			UsageHint         *string `json:"usage_hint"`
		} `json:"slash_commands"`
		MCPServers []struct {
			ID, Name, URL string
			IconEmoji     string `json:"icon_emoji"`
			Tools         *struct {
				Tools []struct {
					Name        string
					Title       *string
					Annotations *struct{ Title *string }
				}
			} `json:"tools_snapshot"`
		} `json:"mcp_servers"`
		TokenRotation *bool                      `json:"security_token_rotation_enabled"`
		Security      map[string]json.RawMessage `json:"security_compliance"`
		ScopeInfo     json.RawMessage            `json:"scope_info"`
	}
	_ = json.Unmarshal(raw, &r)
	s := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	info := AppAboutInfo{AppID: r.AppID, Name: r.Name, AuthFullName: s(r.AuthFullName),
		LongDesc: s(r.LongDesc), AssistantDesc: s(r.AssistantDesc),
		AIApp: r.IsAIApp, AgentApp: r.IsAgentApp, ShownAsAgent: r.IsShownAsAgent,
		Agentforce: len(r.Agentforce) > 0 && !jsonFalsy(r.Agentforce),
		YoutubeURL: s(r.YoutubeURL), SupportURL: s(r.SupportURL), HomeURL: s(r.HomeURL),
		PrivacyURL: s(r.PrivacyLink), SupportEmail: s(r.SupportEmail), Pricing: s(r.Pricing),
		DirectoryApproved: r.DirectoryApproved, SlackIntegration: r.SlackIntegration,
		CustomIntegration: r.CustomIntegration, Distributed: r.Distributed, Published: r.Published,
		WorkflowApp: r.WorkflowApp, CallsApp: r.CallsApp, OrgReadyApp: r.OrgReadyApp,
		HomeTeamID: r.HomeWorkspaceID, HomeOrgID: r.HomeEnterpriseID, ServiceID: r.ServiceID,
		OwnerIDs: r.OwnerIDs, MessagesTab: true, TokenRotation: r.TokenRotation, Security: r.Security}
	if r.NativeAgent != nil {
		info.NativeAgent = r.NativeAgent.Type
		if info.NativeAgent == "" {
			info.NativeAgent = "?"
		}
	}
	if v, ok := r.Icons["image_32"].(string); ok {
		info.Icon32 = v
	}
	info.Icon512, _ = r.Icons["image_512"].(string)
	for _, c := range r.Categories {
		info.Categories = append(info.Categories, AppAboutCategory{ID: anyString(c.ID), Name: anyString(c.Name)})
	}
	// normalizeScreenshots: both sizes, ids by position
	for i, sc := range r.Screenshots {
		a, _ := sc["image_440"].(string)
		b, _ := sc["image_1600"].(string)
		if a != "" && b != "" {
			info.Screenshots = append(info.Screenshots, AppScreenshot{ID: strconv.Itoa(i), Image440: a, Image1600: b})
		}
	}
	// normalizeLocaleCodes: an array, or an object's values
	var langs []string
	if json.Unmarshal(r.Languages, &langs) != nil {
		var obj map[string]string
		if json.Unmarshal(r.Languages, &obj) == nil {
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys) // Object.values keeps insertion order; JSON objects here are keyed 0, 1, …
			for _, k := range keys {
				langs = append(langs, obj[k])
			}
		}
	}
	info.Languages = langs
	if u := r.AppUser; u != nil {
		info.AppUserID, info.AppUsername = u.ID, u.Username
	}
	if m := r.HomeMeta; m != nil {
		info.HomeTab = m.HomeTab != nil && *m.HomeTab
		if m.MessagesTab != nil {
			info.MessagesTab = *m.MessagesTab
		}
	}
	for _, sc := range r.Shortcuts.Global {
		info.GlobalShortcuts = append(info.GlobalShortcuts, AppShortcutRef{ID: sc.ID, Name: sc.Name})
	}
	for _, sc := range r.Shortcuts.Message {
		info.MessageShortcuts = append(info.MessageShortcuts, AppShortcutRef{ID: sc.ID, Name: sc.Name})
	}
	for _, c := range r.SlashCommands {
		info.SlashCommands = append(info.SlashCommands, AppSlashCommand{Name: c.Name, Desc: c.Description, UsageHint: s(c.UsageHint)})
	}
	for _, m := range r.MCPServers {
		srv := AppMCPServer{ID: m.ID, Name: m.Name, URL: m.URL, IconEmoji: m.IconEmoji}
		if m.Tools != nil {
			for _, t := range m.Tools.Tools {
				title := t.Name
				if t.Title != nil {
					title = *t.Title
				} else if t.Annotations != nil && t.Annotations.Title != nil {
					title = *t.Annotations.Title
				}
				srv.Tools = append(srv.Tools, AppMCPTool{Name: t.Name, Title: title})
			}
		}
		if len(srv.Tools) > 0 {
			info.MCPServers = append(info.MCPServers, srv)
		}
	}
	info.Scopes = parseScopeInfo(r.ScopeInfo)
	return info
}

func jsonFalsy(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", `""`:
		return true
	}
	return false
}

func anyString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

// parseScopeInfo is camelCaseDeep (9AQx XK) over scope_info, typed.
func parseScopeInfo(raw json.RawMessage) *AppScopeInfo {
	if jsonFalsy(raw) {
		return nil
	}
	type entry struct {
		Scopes      []string
		Description string
		TokenType   *string `json:"token_type"`
		IsOptional  *bool   `json:"is_optional"`
	}
	type secondary struct {
		Heading    string
		ScopesInfo []entry `json:"scopes_info"`
	}
	type primary struct {
		Heading         string
		SecondaryGroups map[string]secondary `json:"secondary_groups"`
	}
	var r struct{ Content, Actions *primary }
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	conv := func(p *primary) *AppScopeGroup {
		if p == nil {
			return nil
		}
		g := &AppScopeGroup{Heading: p.Heading, SecondaryGroups: map[string]AppScopeSecondary{}}
		for k, sg := range p.SecondaryGroups {
			out := AppScopeSecondary{Heading: sg.Heading}
			for _, e := range sg.ScopesInfo {
				t := "user"
				if e.TokenType != nil {
					t = *e.TokenType
				}
				out.ScopesInfo = append(out.ScopesInfo, AppScopeEntry{Scopes: e.Scopes, Description: e.Description,
					TokenType: t, Optional: e.IsOptional != nil && *e.IsOptional})
			}
			g.SecondaryGroups[camelCase(k)] = out
		}
		return g
	}
	return &AppScopeInfo{Content: conv(r.Content), Actions: conv(r.Actions)}
}

// camelCase is lodash camelCase for the snake_case keys scope_info uses.
func camelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		p = strings.ToLower(p)
		if i > 0 && p != "" {
			p = strings.ToUpper(p[:1]) + p[1:]
		}
		parts[i] = p
	}
	return strings.Join(parts, "")
}

// sanitizeHTMLText is dNbh q: decode HTML entities (nnQC), then drop
// tags.
var htmlTagRe = regexp.MustCompile(`</?[a-zA-Z][^>]*?>`)

func sanitizeHTMLText(s string) string { return htmlTagRe.ReplaceAllString(html.UnescapeString(s), "") }

// ---- the page's model (rmod wN, 2Fle ea) ----

// AboutPermissionBlock is processPrimaryGroup's answer (0to8): the
// heading and its non-empty secondary groups in Slack's order.
type AboutPermissionBlock struct {
	Heading string
	Groups  []AboutPermissionGroup
}

// AboutPermissionGroup is a secondary group's sanitized descriptions;
// Key is its camel-cased name (the row's icon: tS).
type AboutPermissionGroup struct {
	Key, Heading string
	Descriptions []string
}

// AboutPermissions is getPermissionBlocks: required and optional, each
// a content block and an actions block (nil when empty).
type AboutPermissions struct {
	RequiredContent, RequiredActions *AboutPermissionBlock
	OptionalContent, OptionalActions *AboutPermissionBlock
}

// AboutDetailRow is buildDetailRow's row: Type "url", "boolean" or
// "text".
type AboutDetailRow struct{ Label, Value, Type string }

// AboutSecurityCategory is one of the security section's accordions.
type AboutSecurityCategory struct {
	Title string
	Rows  []AboutDetailRow
}

// AboutScopeGroup is a "App has N … token scopes" accordion.
type AboutScopeGroup struct {
	Title, Icon string
	Scopes      []string
}

// AboutPage is what about_page_AboutPage reads (2Fle ea over rmod wN
// and S) for an app.
type AboutPage struct {
	Loaded, Loading      bool // infoLoaded / infoLoading
	AppName              string
	AIApp, AgentApp      bool
	InternalApp          bool
	HomeTeamName         string
	Description          string // long_description, trimmed
	AssistantDescription string
	HasDescription       bool
	Icon32               string
	GlobalShortcuts      []AppShortcutRef
	MessageShortcuts     []AppShortcutRef
	SlashCommands        []AppSlashCommand
	CallsApp, OrgReady   bool
	MCPServers           []AppMCPServer
	HasFeatures          bool
	Screenshots          []AppScreenshot
	YoutubeURL           string
	ShowScreenshots      bool
	ShowPermissions      bool
	Permissions          AboutPermissions
	HasScopeInfo         bool // scopeInfo has content or actions
	SecurityCompliance   bool // security_compliance present
	TokenRotation        *bool
	DeveloperName        string
	Security             []AboutSecurityCategory
	ScopeGroups          []AboutScopeGroup
	AppUserID, Username  string
	SupportURL, HomeURL  string
	SupportEmail         string
	PrivacyURL           string
	Categories           []AppAboutCategory
	Languages            []string // language names
	PricingLabel         string
	ShowSettingsButton   bool
	SettingsURL          string
	WorkflowApp          bool
	WorkflowCollaborator bool
	ShowReportApp        bool
}

// aboutLanguageNames is developer_app_settings' table (b88T).
var aboutLanguageNames = map[string]string{
	"ar": "Arabic", "ca-ES": "Catalan", "zh-Hans": "Chinese (Simplified)", "zh-Hant": "Chinese (Traditional)",
	"hr-HR": "Croatian", "cs-CZ": "Czech", "da-DK": "Danish", "nl-NL": "Dutch", "en-GB": "English (U.K.)",
	"en-US": "English (U.S.)", "fi-FI": "Finnish", "fr-CA": "French (Canada)", "fr-FR": "French (France)",
	"de-DE": "German", "el-GR": "Greek", "he-IL": "Hebrew", "hi-IN": "Hindi", "hu-HU": "Hungarian",
	"id-ID": "Indonesian", "it-IT": "Italian", "ja-JP": "Japanese", "ko-KR": "Korean", "ms": "Malay",
	"no": "Norwegian", "pl-PL": "Polish", "pt-BR": "Portuguese (Brazil)", "pt-PT": "Portuguese (Portugal)",
	"ro-RO": "Romanian", "ru-RU": "Russian", "sk-SK": "Slovak", "es-LA": "Spanish (Latin America)",
	"es-ES": "Spanish (Spain)", "sv-SE": "Swedish", "th-TH": "Thai", "tr-TR": "Turkish", "uk-UA": "Ukrainian",
	"vi-VN": "Vietnamese",
}

// aboutPricingLabel is getPricingLabel (slack_marketplace).
func aboutPricingLabel(p string) string {
	switch p {
	case "free":
		return "Free"
	case "freemium":
		return "Free and paid plans available"
	case "paid_free_trial":
		return "Paid with free trial"
	case "paid":
		return "Paid"
	case "included_with_subscription":
		return "Included with service subscription"
	}
	return ""
}

// aboutPermissionKeys is 0to8's _h: the secondary groups in order.
var aboutPermissionKeys = []string{"adminActions", "channelActions", "channelContent", "userActions", "userContent", "workspaceActions", "workspaceContent"}

// AboutPermissionIcons is 0to8's tS: a secondary group's icon.
var AboutPermissionIcons = map[string]string{
	"channelActions": "channel", "channelContent": "channel", "userActions": "user", "userContent": "user",
	"workspaceActions": "workspace", "workspaceContent": "workspace", "adminActions": "key",
}

func processPrimaryGroup(g *AppScopeGroup, optional bool) *AboutPermissionBlock {
	if g == nil {
		return nil
	}
	var groups []AboutPermissionGroup
	for _, k := range aboutPermissionKeys {
		sg, ok := g.SecondaryGroups[k]
		if !ok {
			continue
		}
		var ds []string
		for _, e := range sg.ScopesInfo {
			if e.Optional == optional {
				ds = append(ds, sanitizeHTMLText(e.Description))
			}
		}
		if len(ds) > 0 {
			groups = append(groups, AboutPermissionGroup{Key: k, Heading: sanitizeHTMLText(sg.Heading), Descriptions: ds})
		}
	}
	if len(groups) == 0 {
		return nil
	}
	return &AboutPermissionBlock{Heading: sanitizeHTMLText(g.Heading), Groups: groups}
}

// AboutPermissionBlocks is getPermissionBlocks.
func AboutPermissionBlocks(s *AppScopeInfo) AboutPermissions {
	if s == nil {
		return AboutPermissions{}
	}
	return AboutPermissions{
		RequiredContent: processPrimaryGroup(s.Content, false), RequiredActions: processPrimaryGroup(s.Actions, false),
		OptionalContent: processPrimaryGroup(s.Content, true), OptionalActions: processPrimaryGroup(s.Actions, true),
	}
}

// Any is hasRenderablePermissions.
func (p AboutPermissions) Any() bool {
	return p.RequiredContent != nil || p.RequiredActions != nil || p.OptionalContent != nil || p.OptionalActions != nil
}

// extractScopes is jG2q il: every scope of a primary group, bot tokens
// apart from user ones (Object.values order: the API's key order, which
// Go's map loses, so secondary groups go in Slack's _h order, then any
// others by name).
func extractScopes(g *AppScopeGroup, user, bot *[]string) {
	if g == nil {
		return
	}
	keys := make([]string, 0, len(g.SecondaryGroups))
	seen := map[string]bool{}
	for _, k := range aboutPermissionKeys {
		if _, ok := g.SecondaryGroups[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range g.SecondaryGroups {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range append(keys, rest...) {
		for _, e := range g.SecondaryGroups[k].ScopesInfo {
			for _, sc := range e.Scopes {
				if e.TokenType == "bot" {
					*bot = append(*bot, sc)
				} else {
					*user = append(*user, sc)
				}
			}
		}
	}
}

// normalizeFieldValue is jG2q's: null or missing is nothing; arrays
// are joined with ", " (null and "" items dropped), everything else is
// String(v) trimmed.
func normalizeFieldValue(raw json.RawMessage) string {
	if t := strings.TrimSpace(string(raw)); t == "" || t == "null" {
		return ""
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, v := range arr {
			if v == nil {
				continue
			}
			if s, ok := v.(string); ok && s == "" {
				continue
			}
			parts = append(parts, jsString(v))
		}
		return strings.Join(parts, ", ")
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(jsString(v))
}

// jsString is String(v) for JSON values.
func jsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return "null"
	case map[string]any:
		return "[object Object]"
	case []any:
		var parts []string
		for _, x := range t {
			if x == nil {
				parts = append(parts, "")
			} else {
				parts = append(parts, jsString(x))
			}
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func isBooleanLike(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "no", "true", "false":
		return true
	}
	return false
}

// buildDetailRow is jG2q CL.
func buildDetailRow(label string, sec map[string]json.RawMessage, key string) *AboutDetailRow {
	v := normalizeFieldValue(sec[key])
	if v == "" {
		return nil
	}
	v = sanitizeHTMLText(v)
	t := "text"
	switch {
	case strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://"):
		t = "url"
	case isBooleanLike(v):
		t = "boolean"
	}
	return &AboutDetailRow{Label: label, Value: v, Type: t}
}

// aboutSecurity is AboutPageSecurityCompliance's two lists (yZZv x and
// y), with agent_about_page on.
func aboutSecurity(info *AppAboutInfo, developer string) ([]AboutSecurityCategory, []AboutScopeGroup) {
	yesNo := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}
	localize := func(r *AboutDetailRow) *AboutDetailRow {
		if r == nil || r.Type != "boolean" {
			return r
		}
		v := strings.ToLower(strings.TrimSpace(r.Value))
		out := *r
		out.Value = yesNo(v == "yes" || v == "true")
		return &out
	}
	build := func(title string, rows ...*AboutDetailRow) *AboutSecurityCategory {
		var out []AboutDetailRow
		for _, r := range rows {
			if r = localize(r); r != nil {
				out = append(out, *r)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return &AboutSecurityCategory{Title: title, Rows: out}
	}
	var cats []AboutSecurityCategory
	a := info.Security
	t := info.TokenRotation
	if a == nil {
		if t != nil {
			cats = []AboutSecurityCategory{{Title: "Security", Rows: []AboutDetailRow{{Label: "Uses token rotation", Value: yesNo(*t), Type: "boolean"}}}}
		}
	} else {
		r := func(label, key string) *AboutDetailRow { return buildDetailRow(label, a, key) }
		var dev *AboutDetailRow
		if developer != "" {
			dev = &AboutDetailRow{Label: "Developer", Value: developer, Type: "text"}
		}
		general := build("General", r("Company headquarters location", "company_location"), r("Terms of service", "tos_url"), dev)
		privacy := build("Privacy & data governance",
			r("Data retention policy", "data_retention_policy"), r("Data archiving and removal policy", "data_archival_removal_policy"),
			r("Data storage policy", "data_storage_policy"), r("Data center location(s)", "data_center_location"),
			r("Data hosting details", "data_how_host"), r("Data hosting company", "data_host_company"),
			r("App/service has sub-processors", "cert_comply_sub_processors"), r("Guidelines for sub-processors", "cert_comply_guidelines_sub_processors_url"),
			r("Uses LLM/AI", "use_llm"), r("LLM description", "llm_desc"), r("LLM data retention settings", "llm_retention_settings_desc"),
			r("LLM data tenancy", "llm_data_tenancy_desc"), r("LLM data residency", "llm_data_residency_desc"))
		cert := build("Certification & compliance",
			r("SOC attestation", "cert_comply_soc_cert_url"), r("COPPA compliance", "cert_comply_coppa_compliance_url"),
			r("ISO/IEC-27001 certification", "cert_comply_iso_iec_27001_cert_url"), r("ISO/IEC-27017 attestation", "cert_comply_iso_iec_27017_cert_url"),
			r("ISO/IEC-27018 attestation", "cert_comply_iso_iec_27018_cert_url"), r("FedRAMP ATO", "cert_comply_fedramp_cert_url"),
			r("CSA Star level", "cert_comply_csa_star_level_cert_url"), r("HIPAA compliant", "cert_comply_hipaa_compliant"),
			r("Privacy Shield notice", "cert_comply_privacy_shield_notice_url"), r("GDPR commitment", "cert_comply_gdpr_commitment_url"),
			r("Data deletion request procedure", "cert_comply_data_deletion_requests_handling"),
			r("FedRAMP Authorization", "fedramp_authorization"), r("DOD SRG ILx", "dod_srg_ilx"), r("ITAR Compliant", "itar_compliant"))
		secRows := []*AboutDetailRow{
			r("Security support SSO", "security_support_sso"), r("Supports Single Sign On (SSO) with the following providers", "security_what_sso"),
			r("Supports Security Assertion Markup Language (SAML)", "security_support_saml"), r("Has a dedicated security team", "security_dedicated_security"),
			r("Contact for security issues", "security_email"), r("Has a vulnerability disclosure program", "security_vulnerability_program"),
			r("Vulnerability disclosure program URL", "security_vulnerability_url"), r("Vulnerability disclosure program covers Slack app", "security_vulnerability_program_slack"),
			r("Has a bug bounty program", "security_bug_bounty_program"), r("Bug bounty program URL", "security_bug_bounty_url"),
			r("Requires third party authorization/connections", "security_third_party_auths_required"),
			r("Third party services used by this app", "security_third_party_services"),
		}
		if t != nil {
			// pushed as "text", so the boolean localization leaves it
			secRows = append(secRows, &AboutDetailRow{Label: "Uses token rotation", Value: yesNo(*t), Type: "text"})
		}
		sec := build("Security", secRows...)
		for _, c := range []*AboutSecurityCategory{general, privacy, cert, sec} {
			if c != nil {
				cats = append(cats, *c)
			}
		}
	}
	var groups []AboutScopeGroup
	if s := info.Scopes; s != nil {
		var user, bot []string
		extractScopes(s.Actions, &user, &bot)
		extractScopes(s.Content, &user, &bot)
		user, bot = uniqueStrings(user), uniqueStrings(bot)
		if len(user) > 0 {
			groups = append(groups, AboutScopeGroup{Title: "App has " + strconv.Itoa(len(user)) + " user token scopes", Icon: "message", Scopes: user})
		}
		if len(bot) > 0 {
			groups = append(groups, AboutScopeGroup{Title: "App has " + strconv.Itoa(len(bot)) + " bot token scopes", Icon: "workspace", Scopes: bot})
		}
	}
	return cats, groups
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// isNativeFlowAppID is SM2u isNativeFlowAppId: the native workflow and
// Slackbot apps (Clack knows neither id, as Slack's v.a_/i.Io read them
// from boot data Clack doesn't load).
func isNativeFlowAppID(string) bool { return false }

// AboutPageOf is the page's selector (2Fle ea over rmod wN) for an app:
// info is the store's answer (nil: not there; loading tells pending from
// non-existent), card the app profile (AppCardOf; nil while unknown).
func (a *App) AboutPageOf(info *AppAboutInfo, loading bool, card *AppCard) AboutPage {
	p := AboutPage{Loading: loading}
	if info == nil {
		return p
	}
	p.Loaded = true
	r := info
	knownAI := false // 3NYU isKnownAiAppId: Clack doesn't load the AI apps list (assistant.search.context's apps)
	p.AIApp = knownAI || r.AIApp || r.AgentApp
	name := r.Name
	if r.Agentforce && r.AuthFullName != "" {
		name = r.AuthFullName
	}
	p.AppName = sanitizeHTMLText(name)
	p.InternalApp = !r.DirectoryApproved && !r.SlackIntegration
	p.Description = strings.TrimSpace(r.LongDesc)
	p.AssistantDescription = strings.TrimSpace(r.AssistantDesc)
	p.HasDescription = p.AIApp && p.AssistantDescription != "" || p.Description != ""
	p.AgentApp = r.AgentApp
	p.Icon32 = r.Icon32
	p.GlobalShortcuts, p.MessageShortcuts = r.GlobalShortcuts, r.MessageShortcuts
	p.SlashCommands = r.SlashCommands
	p.CallsApp, p.OrgReady = r.CallsApp, r.OrgReadyApp
	p.MCPServers = r.MCPServers
	p.HasFeatures = p.AIApp || p.AgentApp || len(p.GlobalShortcuts) > 0 || len(p.MessageShortcuts) > 0 ||
		len(p.SlashCommands) > 0 || p.CallsApp || p.OrgReady || len(p.MCPServers) > 0
	p.Screenshots, p.YoutubeURL = r.Screenshots, r.YoutubeURL
	p.ShowScreenshots = len(p.Screenshots) > 0 || p.YoutubeURL != ""
	p.Permissions = AboutPermissionBlocks(r.Scopes)
	p.HasScopeInfo = r.Scopes != nil && (r.Scopes.Content != nil || r.Scopes.Actions != nil)
	p.SecurityCompliance = r.Security != nil
	p.TokenRotation = r.TokenRotation
	// company_developer_name ?? name: only null or missing falls back
	dev := r.Name
	var cdn *string
	if raw, ok := r.Security["company_developer_name"]; ok && json.Unmarshal(raw, &cdn) == nil && cdn != nil {
		dev = *cdn
	}
	p.DeveloperName = sanitizeHTMLText(dev)
	p.AppUserID, p.Username = r.AppUserID, r.AppUsername
	p.SupportURL, p.HomeURL, p.SupportEmail, p.PrivacyURL = r.SupportURL, r.HomeURL, r.SupportEmail, r.PrivacyURL
	p.Categories = r.Categories
	for _, l := range r.Languages {
		if n, ok := aboutLanguageNames[l]; ok {
			l = n
		}
		p.Languages = append(p.Languages, l)
	}
	p.PricingLabel = aboutPricingLabel(r.Pricing)
	p.ShowReportApp = r.DirectoryApproved && !r.SlackIntegration
	p.WorkflowApp = r.WorkflowApp
	on := a.AgentAboutPage()

	// isUndistributedNonNativeApp (rmod Dr) and showAppDirectoryLink (SM2u
	// JE over the app profile)
	undistributed := !r.Distributed && !r.WorkflowApp && r.NativeAgent == "" && !isNativeFlowAppID(r.AppID)
	dirLink := false
	if card != nil && card.AppID != "" && r.NativeAgent != "channel_expert" {
		slack, custom := card.AppType == "slack", card.AppType == "custom_integration"
		listed := !isNativeFlowAppID(card.AppID) && (slack || custom || (card.External && card.Published) || (!card.External && card.Distributed))
		dirLink = listed && a.TeamURL() != ""
	}
	hasScopes := p.HasScopeInfo
	if on {
		// shouldShowPermissionsSection (0to8 UN)
		p.ShowPermissions = p.Permissions.Any() && !r.Agentforce && (dirLink || p.AIApp && hasScopes || undistributed)
	} else {
		p.ShowPermissions = p.Permissions.Any() && (dirLink || p.AIApp && !r.Agentforce && hasScopes)
	}
	if on {
		p.Security, p.ScopeGroups = aboutSecurity(r, sanitizeHTMLText(dev))
	}

	// the app profile's: the settings button (SM2u KN, BX), the home team
	// (hY) and the workflow collaborator check
	if card != nil {
		p.ShowSettingsButton = a.showSettingsButton(card, r)
		if p.ShowSettingsButton && !r.WorkflowApp {
			p.SettingsURL = a.settingsButtonURL(card, r)
		}
		p.HomeTeamName = a.appHomeTeamName(card)
	}
	var owners []string
	if a.Experiment(ExpFlexpaneContentParity) == "on" {
		owners = r.OwnerIDs
	}
	self := a.Self().ID
	for _, o := range owners {
		if o != "" && o == self {
			p.WorkflowCollaborator = true
		}
	}
	return p
}

// showSettingsButton is SM2u shouldShowSettingsButton: not for a guest
// or an external app, and the app installed on the viewer's team
// (scope_app_install_reads_to_viewer_team on) or installed at all (off),
// with a settings URL or an app id.
func (a *App) showSettingsButton(c *AppCard, r *AppAboutInfo) bool {
	if c.External || a.Self().Guest || isNativeFlowAppID(c.AppID) {
		return false
	}
	installed := c.Installed != nil && *c.Installed
	if a.Experiment(ExpScopeAppInstallReadsToViewerTeam) == "on" {
		top := firstNonEmpty(a.C.Creds.EnterpriseID, a.C.Creds.Team)
		installed = installed && (c.TopTeam() == "" || c.TopTeam() == top)
	}
	return installed && (a.settingsURL(c, r) != "" || c.AppID != "")
}

// settingsURL is SM2u getSettingsUrl: the team's /services/{service_id}
// ?settings=1, unless the app is external or has no service id.
func (a *App) settingsURL(c *AppCard, r *AppAboutInfo) string {
	sid := firstNonEmpty(r.ServiceID, c.BotID)
	base := strings.TrimSuffix(a.TeamURL(), "/")
	if c.External || sid == "" || base == "" || a.Self().Guest {
		return ""
	}
	return base + "/services/" + sid + "?settings=1"
}

// settingsButtonURL is SM2u getSettingsButtonUrl: the settings URL, else
// /apps/{app_id}?settings=1 on the team.
func (a *App) settingsButtonURL(c *AppCard, r *AppAboutInfo) string {
	if u := a.settingsURL(c, r); u != "" {
		return u
	}
	if c.AppID == "" {
		return ""
	}
	return strings.TrimSuffix(a.TeamURL(), "/") + "/apps/" + c.AppID + "?settings=1"
}

// appHomeTeamName is SM2u getAppHomeTeamDisplayName: the app's top-level
// team's name (the viewer's own team for its internal apps).
func (a *App) appHomeTeamName(c *AppCard) string {
	if name, _, ok := a.Team(c.TopTeam(), 20); ok {
		return name
	}
	if top := firstNonEmpty(a.C.Creds.EnterpriseID, a.C.Creds.Team); c.TopTeam() == "" || c.TopTeam() == top || c.TopTeam() == a.C.Creds.Team {
		a.S.RLock()
		defer a.S.RUnlock()
		return a.S.TeamName
	}
	return ""
}

// ReportAppURL is useReportAppLink (nzWh): the team's
// /help/requests/new?app_id=….
func (a *App) ReportAppURL(appID string) string {
	return strings.TrimSuffix(a.TeamURL(), "/") + "/help/requests/new?app_id=" + url.QueryEscape(appID)
}
