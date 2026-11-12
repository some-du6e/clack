package core

// Posting permissions in channel details' Settings, read from Slack's web
// client (gantry-v2 build d3de3a487b1c, 2026-11-12; chunk
// shared-…admin-channel-modal_tsx, module oWlM, unless noted):
//
//   - the row: ViewPostingPermissions, an EditableField titled "Posting
//     permissions" whose text is PostingPermissionsSubtitle. The
//     AdminChannelModal draws it first in the "permissions" section when
//     additionalOptionsToShow (boot-shared Xihp) says postingPermissions
//     shouldShow: canManageChannelPostingPermissions (boot-shared Z8EV E)
//     or the Standard-plan upsell (ef below). It is disabled when the viewer
//     can't act as a workspace admin in the channel and isn't a member, or
//     for the upsell.
//   - Z8EV E: a public or private channel, not a DM or group DM, not an
//     externally shared channel hosted by another org (5DYq ju, 2ES1 b);
//     then #general or the manage_channel_posting__access feature, and
//     MANAGE_CHANNEL_PERMISSIONS in the channel, or (not an org default
//     channel, or an org admin) MANAGE_CHANNEL_POSTING_PERMISSIONS and the
//     viewer can post (o/IU E).
//   - ef: a Standard-plan team (mAu1 KJ), boost_bus_plus_awareness_and_
//     upgrades "treatment" (boot-shared hwcP RN), no
//     manage_channel_posting__access, not #general, no active trial
//     (L4wy iH).
//   - the reads: four channels.prefs.get {channel_id, pref_name} (6ndL Kn
//     getChannelPref, vendors 60sj) for who_can_post, can_thread,
//     enable_at_here and enable_at_channel, the subtitle's with reason
//     channel-options-ia-posting-permission-subtitle, the modal's with
//     channel-options-ia-posting-permission-preferences.
//   - the write: PostingPermissionsModalContents' Save is
//     setChannelPrefsByAPIAndUpdateStore (boot-shared fDCj y): one
//     channels.prefs.set {channel_id, prefs} (6ndL ZL, vendors WQyo), prefs
//     the JSON of who_can_post (formatWhoCanPostApi), can_thread
//     ("type:ra" when threads are allowed, else who_can_post's value),
//     enable_at_here and enable_at_channel ("true" / "false", both the
//     @here/@channel checkbox).
//   - features: the featureAccess store (client-boot X+JB G2 isEnabled),
//     filled at boot by fetchFeatureAccessData: features.access.policies.list
//     (vendors 25MS) answers feature_policies {name: {is_enabled}}. Every
//     feature is off until it answers.

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Feature names the posting permissions read.
const (
	FeatureManageChannelPosting = "manage_channel_posting__access"
	FeatureFrontline            = "frontline__access"
)

// The pref names of PostingPermissionsSubtitle and PostingPreferences.
const (
	PrefWhoCanPost   = "who_can_post"
	PrefCanThread    = "can_thread"
	PrefEnableHere   = "enable_at_here"
	PrefEnableAtChan = "enable_at_channel"
)

// ChannelPrefValue is one channels.prefs.get answer.
type ChannelPrefValue struct {
	OK  bool            // the call answered ok
	Raw json.RawMessage // pref_value (nil when the call failed)
}

// present is `!!pref_value`: a value that isn't null.
func (v ChannelPrefValue) present() bool {
	t := strings.TrimSpace(string(v.Raw))
	return v.OK && t != "" && t != "null" && t != "false" && t != `""` && t != "0"
}

// prefObject is a pref_value read as Slack reads it: type, user,
// enabled, and whether it has keys (lodash isEmpty).
type prefObject struct {
	typ     []string
	hasType bool // `pref_value.type` is truthy (an array, even empty)
	users   []string
	enabled json.RawMessage
	empty   bool
}

func readPref(raw json.RawMessage) prefObject {
	var o prefObject
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		o.empty = true // lodash isEmpty of a non-object
		return o
	}
	o.empty = len(m) == 0
	if t, ok := m["type"]; ok {
		var l []string
		var s string
		switch {
		case json.Unmarshal(t, &l) == nil && l != nil:
			o.typ, o.hasType = l, true
		case json.Unmarshal(t, &s) == nil && s != "":
			o.typ, o.hasType = []string{s}, true
		}
	}
	if u, ok := m["user"]; ok {
		_ = json.Unmarshal(u, &o.users)
	}
	o.enabled = m["enabled"]
	return o
}

func (o prefObject) type0() string {
	if len(o.typ) == 0 {
		return ""
	}
	return o.typ[0]
}

// enabledOn is `pref_value?.enabled || isEmpty(pref_value)` compared with
// true: only a JSON true, or an empty value, counts.
func (o prefObject) enabledOn() bool {
	if t := strings.TrimSpace(string(o.enabled)); t != "" && t != "null" && t != "false" && t != "0" && t != `""` {
		return t == "true"
	}
	return o.empty
}

// PostingPrefs are the four reads.
type PostingPrefs struct {
	WhoCanPost, CanThread, AtHere, AtChannel ChannelPrefValue
}

// PostingViewer is what the derivations need to know about the viewer and
// the channel.
type PostingViewer struct {
	SelfID          string
	Enterprise      bool // the team is on Enterprise Grid (jDlU r7)
	EnterpriseAdmin bool // enterprise_user.is_admin (Yyn8 n5; Xfgd FQ)
	WorkspaceAdmin  bool // an admin or owner (Xfgd Zo; 7RIn S4 in the channel)
	ManagePerms     bool // MANAGE_CHANNEL_PERMISSIONS in the channel
	OrgDefault      bool // an org-wide default channel (Opi6 lQ)
	OrgShared       bool // shared across the org's workspaces (tZ5x M)
}

func notSelf(us []string, self string) []string {
	var out []string
	for _, u := range us {
		if u != self {
			out = append(out, u)
		}
	}
	return out
}

func isAdminType(t string) bool { return t == "admin" || t == "owner" || t == "org_admin" }

// PostingSubtitle is PostingPermissionsSubtitle's state: whoCanPost (one of
// ra, regular, admin, org_admin, admin_multi, owner_multi,
// org_admin_multi, multi; "" draws nothing), whether everyone can reply
// and whether @channel/@here may be used. ok is false while the
// who_can_post read failed or had no value (the subtitle stays empty).
func PostingSubtitle(p PostingPrefs, v PostingViewer) (who string, canThread, atHere, ok bool) {
	if !p.WhoCanPost.present() {
		return "", false, false, false
	}
	o := readPref(p.WhoCanPost.Raw)
	can := (v.Enterprise && v.EnterpriseAdmin || !v.Enterprise && v.WorkspaceAdmin) || v.ManagePerms
	others := notSelf(o.users, v.SelfID)
	t := o.type0()
	if o.hasType {
		switch {
		case len(others) > 0 && t == "admin":
			who = "admin_multi"
		case len(others) > 0 && t == "owner":
			who = "owner_multi"
		case len(others) > 0 && t == "org_admin":
			who = "org_admin_multi"
		case !isAdminType(t) || can:
			who = postingNormalize(t, v)
		default:
			who = "multi"
		}
	} else if v.OrgDefault {
		who = "org_admin"
	} else {
		who, canThread = "ra", true
	}
	if ct := readPref(p.CanThread.Raw); p.CanThread.OK && ct.hasType {
		canThread = ct.type0() == "all" || ct.type0() == "ra"
	}
	atHere = postingAtHere(p)
	return who, canThread, atHere, true
}

// postingNormalize maps a stored type the way both components do: org_admin
// off Grid reads as admin, owner as org_admin (Grid) or admin, and an org
// default channel's anything as org_admin.
func postingNormalize(t string, v PostingViewer) string {
	n := t
	if !v.Enterprise && t == "org_admin" {
		n = "admin"
	}
	if n == "owner" {
		n = "admin"
		if v.Enterprise {
			n = "org_admin"
		}
	}
	if v.OrgDefault && n != "org_admin" {
		n = "org_admin"
	}
	return n
}

// postingAtHere: both mention prefs read, either enabled; a failed read
// leaves mentions on.
func postingAtHere(p PostingPrefs) bool {
	if !p.AtHere.OK || !p.AtChannel.OK {
		return true
	}
	return readPref(p.AtHere.Raw).enabledOn() || readPref(p.AtChannel.Raw).enabledOn()
}

// PostingForm is PostingPreferences' state after its reads: the selected
// radio (ra, regular, admin, org_admin, multi), the people picked for
// multi (the stored user list, the viewer included), Allow threads and
// the mentions checkbox. ok is false until who_can_post has a value: the
// form keeps its spinner.
type PostingForm struct {
	Who       string
	Users     []string
	CanThread bool
	AtHere    bool
	OK        bool
}

// PostingFormState derives PostingForm from the reads.
func PostingFormState(p PostingPrefs, v PostingViewer) PostingForm {
	var f PostingForm
	if !p.WhoCanPost.present() {
		return f
	}
	o := readPref(p.WhoCanPost.Raw)
	d := (v.Enterprise && v.EnterpriseAdmin || !v.Enterprise && v.WorkspaceAdmin) || v.ManagePerms
	others := notSelf(o.users, v.SelfID)
	t := o.type0()
	if o.hasType {
		switch {
		case len(others) > 0 && isAdminType(t):
			f.Who, f.Users = "multi", append([]string(nil), o.users...)
		case !isAdminType(t) || d:
			f.Who = postingNormalize(t, v)
		default:
			f.Who = "multi"
		}
	} else if v.OrgDefault {
		f.Who = "org_admin"
	} else {
		f.Who, f.CanThread = "ra", true
	}
	if ct := readPref(p.CanThread.Raw); p.CanThread.OK && ct.hasType {
		f.CanThread = ct.type0() == "all" || ct.type0() == "ra"
	}
	f.AtHere = postingAtHere(p)
	f.OK = true
	return f
}

// FormatWhoCanPostAPI is formatWhoCanPostApi: "type:<who>", or for multi
// "type:admin" ("type:org_admin" on an org-shared Grid channel) and
// ",user:<id>" for each person.
func FormatWhoCanPostAPI(who string, users []string, orgAdmin bool) string {
	if who != "multi" {
		return "type:" + who
	}
	s := "type:admin"
	if orgAdmin {
		s = "type:org_admin"
	}
	for _, u := range users {
		s += ",user:" + u
	}
	return s
}

// PostingSavePrefs is the prefs JSON PostingPermissionsModalContents saves
// for the form's values.
func PostingSavePrefs(who string, picked []string, canThread, atHere bool, v PostingViewer) string {
	a := who
	if who == "admin" || who == "org_admin" {
		a = "multi"
	}
	grid := v.Enterprise && v.OrgShared
	admin := v.WorkspaceAdmin
	if grid {
		admin = v.EnterpriseAdmin
	}
	var self []string
	if !admin {
		self = []string{v.SelfID}
	}
	users := self
	if who == "multi" {
		users = nil
		for _, u := range append(append([]string(nil), picked...), self...) {
			if !slices.Contains(users, u) { // lodash uniq
				users = append(users, u)
			}
		}
	}
	i := FormatWhoCanPostAPI(a, users, grid)
	ct := i
	if canThread {
		ct = "type:ra"
	}
	on := "false"
	if atHere {
		on = "true"
	}
	b, _ := json.Marshal(struct {
		Who  string `json:"who_can_post"`
		CT   string `json:"can_thread"`
		Here string `json:"enable_at_here"`
		Chan string `json:"enable_at_channel"`
	}{i, ct, on, on})
	return string(b)
}

// ChannelPref is getChannelPref: channels.prefs.get {channel_id,
// pref_name} (Clack's calls carry no _x_reason).
func (a *App) ChannelPref(ctx context.Context, ch, name string) ChannelPrefValue {
	var r struct {
		Value json.RawMessage `json:"pref_value"`
	}
	p := url.Values{"channel_id": {ch}, "pref_name": {name}}
	if err := a.C.Call(ctx, "channels.prefs.get", p, &r); err != nil {
		return ChannelPrefValue{}
	}
	return ChannelPrefValue{OK: true, Raw: r.Value}
}

// ChannelPostingPrefs runs the four reads together (Promise.all).
func (a *App) ChannelPostingPrefs(ctx context.Context, ch string) PostingPrefs {
	var p PostingPrefs
	var wg sync.WaitGroup
	for name, dst := range map[string]*ChannelPrefValue{PrefWhoCanPost: &p.WhoCanPost, PrefCanThread: &p.CanThread,
		PrefEnableHere: &p.AtHere, PrefEnableAtChan: &p.AtChannel} {
		wg.Add(1)
		go func(name string, dst *ChannelPrefValue) {
			defer wg.Done()
			*dst = a.ChannelPref(ctx, ch, name)
		}(name, dst)
	}
	wg.Wait()
	return p
}

// SetChannelPrefs is setChannelPrefsFetcher: channels.prefs.set
// {channel_id, prefs}.
func (a *App) SetChannelPrefs(ctx context.Context, ch, prefs string) error {
	p := url.Values{"channel_id": {ch}, "prefs": {prefs}}
	return a.C.Call(ctx, "channels.prefs.set", p, nil)
}

// PostingVisibility is additionalOptionsToShow's postingPermissions for a
// channel.
type PostingVisibility struct {
	Show, Disabled bool
}

// PostingVisIn is what the rule reads.
type PostingVisIn struct {
	Channel         bool // a public or private channel (aK5v jq), not a DM or group DM
	ExtSharedHosted bool // externally shared and hosted by another org
	General         bool
	OrgDefault      bool
	Member          bool
	FeatureAccess   bool // manage_channel_posting__access
	ManagePerms     bool // MANAGE_CHANNEL_PERMISSIONS
	ManagePosting   bool // MANAGE_CHANNEL_POSTING_PERMISSIONS
	EnterpriseAdmin bool
	WorkspaceAdmin  bool // canUserActAsWorkspaceAdmin in the channel
	CanPost         bool // canUserPostInChannel
	StandardPlan    bool
	UpsellTreatment bool // boost_bus_plus_awareness_and_upgrades = treatment
	TrialActive     bool
}

// PostingPermsVisibility is the rule (Xihp postingPermissions, Z8EV E).
func PostingPermsVisibility(in PostingVisIn) PostingVisibility {
	can := false
	if in.Channel && !in.ExtSharedHosted && (in.General || in.FeatureAccess) {
		can = in.ManagePerms || (!in.OrgDefault || in.EnterpriseAdmin) && in.ManagePosting && in.CanPost
	}
	upsell := in.StandardPlan && in.UpsellTreatment && !in.FeatureAccess && !in.General && !in.TrialActive
	return PostingVisibility{
		Show:     can || upsell,
		Disabled: !in.WorkspaceAdmin && !in.Member || upsell,
	}
}

// OnStandardPlan is mAu1 isOnStandardPlan for the current team: plan
// "std" (false until team.info answers).
func (a *App) OnStandardPlan() bool {
	t := a.teamPlan()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.known && t.plan == "std"
}

// OnPlusPlanOrHigher is mAu1 isOnPlusPlanOrHigher for the current team:
// Plus, a compliance plan or Enterprise Grid. PostingPermissionsModal is
// titled "Channel permissions" there, else "Posting permissions".
func (a *App) OnPlusPlanOrHigher() bool {
	if a.C.Creds.EnterpriseID != "" {
		return true
	}
	t := a.teamPlan()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.known && (t.enterpriseID != "" || t.plan == "plus" || t.plan == "compliance")
}

// PostingAtChannelLocked is shouldStopChannelMentions (uXk7) with
// allowToAdmins, as PostingPreferences asks it: the mentions checkbox is
// off and disabled in a channel past loud_channel_mentions_limit for
// someone who isn't an admin.
func (a *App) PostingAtChannelLocked(ch string) bool { return a.stopped(nil, ch) }

// ---- features.access.policies.list ----

type featureCache struct {
	mu      sync.Mutex
	known   bool
	loading bool
	at      time.Time
	on      map[string]bool
}

var featureIndex sync.Map // *App → *featureCache

func (a *App) features() *featureCache {
	v, _ := featureIndex.LoadOrStore(a, &featureCache{on: map[string]bool{}})
	return v.(*featureCache)
}

// FeatureAccess is the featureAccess store's isEnabled(name) without
// blocking: false until features.access.policies.list answered, which it
// starts the first time (again after an hour). The answer emits a
// "features" event.
func (a *App) FeatureAccess(name string) bool {
	f := a.features()
	f.mu.Lock()
	start := (!f.known || time.Since(f.at) > time.Hour) && !f.loading
	if start {
		f.loading = true
	}
	on := f.on[name]
	f.mu.Unlock()
	if start {
		go a.loadFeatures(a.bg)
	}
	return on
}

func (a *App) loadFeatures(ctx context.Context) {
	var r struct {
		Policies map[string]struct {
			Enabled bool `json:"is_enabled"`
		} `json:"feature_policies"`
	}
	err := a.C.Call(ctx, "features.access.policies.list", url.Values{}, &r)
	f := a.features()
	f.mu.Lock()
	f.loading = false
	f.known, f.at = true, time.Now() // a failed read keeps every feature off for the hour, as Slack's boot does
	if err == nil {
		on := map[string]bool{}
		for k, v := range r.Policies {
			on[k] = v.Enabled
		}
		f.on = on
	}
	f.mu.Unlock()
	if err == nil {
		a.emit(Event{Type: "features"})
	}
}

// The experiments the posting permissions read (TestExperimentReadsListed):
// PostingPermissions' frontline label and the Standard-plan upsell.
func init() {
	experimentReads["frontline_users"] = on
	experimentReads["boost_bus_plus_awareness_and_upgrades"] = []string{"treatment"}
}
