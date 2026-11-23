package core

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/expandable"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's expandables store (boot-shared 5vqP, build 9f450b8ad3db):
// storeKey "expandables", persistenceEnabled with persistImmediately, an
// id object map (Ua4u) of expandable key (internal/expandable) to
// isExpanded. A key with no state reads undefined, so each part falls
// back to its own default (an image's from the prefs); a stored false
// stays false, a collapse, not a deletion. Writers: setExpanded (9ZlE:
// a caret, "Hide image", "just show this one") and bulkSetExpanded (T03W:
// /collapse and /expand, every key at once); both are addObjects, which
// skips an empty key and changes nothing for a value it already holds.
//
// The state lives here for the whole account, as Slack's does, and is
// kept in expandables.json (store.ExpandablesFile) the moment it changes
// (persistImmediately): a preview collapsed before a restart is still
// collapsed after it. The keys of an attachment without an id carry its
// image URL or embed HTML; those stay in memory (no message content goes
// to disk).
//
// The defaults (isAttachmentExpanded, FDIC; isImageTooLarge, WGgb;
// shouldCollapseFromUnverifiedUser, Ns/U) read the account's prefs and,
// in a DM or group DM, the sender's organisation:
//
//   - expand_inline_imgs false ("Show images and files from linked
//     websites" off) collapses every linked image, video and audio clip;
//   - with obey_inline_img_limit on, an image over 20 MB (0x1400000 bytes)
//     stays collapsed until shown;
//   - a member of an external organisation that isn't verified
//     (team.info is_verified), in a DM or a group DM, whom the viewer
//     hasn't allowed (allowed_unfurl_senders), gets the preview hidden
//     behind ExpandableContainer's UnverifiedDisclosure (ljcx).
//
// Every change bumps ExpandablesGen and emits Event{Type: "expandables"},
// so the message views lay out their parts again.

type expandablesState struct {
	mu     sync.Mutex
	m      map[string]bool
	loaded bool // the disk's copy is in (or there is none)
	dirty  bool // changed since the last write
	saving bool // a write is under way (saveExpandablesSoon's goroutine)
	gen    atomic.Uint64
}

type expandablesDisk struct {
	V        int             `json:"v"`
	Team     string          `json:"team"`
	User     string          `json:"user"`
	Expanded map[string]bool `json:"expanded"`
}

// Expanded is getObjectById (Qa6R) of the expandables store: key's state,
// ok false when it has none (undefined).
func (a *App) Expanded(key string) (on, ok bool) {
	x := &a.expd
	x.mu.Lock()
	defer x.mu.Unlock()
	on, ok = x.m[key]
	return on, ok
}

// SetExpanded is setExpanded (9ZlE): key expanded or collapsed.
func (a *App) SetExpanded(key string, on bool) {
	a.BulkSetExpanded(map[string]bool{key: on})
}

// BulkSetExpanded is bulkSetExpanded (T03W) and addObjects (Ua4u): every
// key in m set at once, an empty key skipped; one event when anything
// changed.
func (a *App) BulkSetExpanded(m map[string]bool) {
	x := &a.expd
	changed := false
	x.mu.Lock()
	for k, on := range m {
		if k == "" {
			continue
		}
		if old, ok := x.m[k]; ok && old == on {
			continue
		}
		if x.m == nil {
			x.m = map[string]bool{}
		}
		x.m[k] = on
		changed = true
	}
	x.mu.Unlock()
	if !changed {
		return
	}
	x.gen.Add(1)
	a.emit(Event{Type: "expandables"})
	a.saveExpandablesSoon()
}

// ExpandablesGen counts the changes to what the expandables read: the
// store, and the sender organisations the unverified rule checks. A view
// lays its parts out again when it moved.
func (a *App) ExpandablesGen() uint64 { return a.expd.gen.Load() }

// loadExpandables reads expandables.json for the account (team, user) once,
// at Boot's warm load: the keys set this session already keep their
// state.
func (a *App) loadExpandables(team, user string) {
	var d expandablesDisk
	ok := false
	if team != "" || user != "" {
		b, err := a.S.ReadFile(store.ExpandablesFile)
		ok = err == nil && json.Unmarshal(b, &d) == nil && d.V == store.CacheVersion && d.Team == team && d.User == user
	}
	x := &a.expd
	x.mu.Lock()
	if x.loaded {
		x.mu.Unlock()
		return
	}
	x.loaded = true
	n := 0
	if ok {
		if x.m == nil {
			x.m = make(map[string]bool, len(d.Expanded))
		}
		for k, on := range d.Expanded {
			if _, newer := x.m[k]; !newer && k != "" {
				x.m[k] = on
				n++
			}
		}
	}
	x.mu.Unlock()
	if n > 0 {
		x.gen.Add(1)
		a.emit(Event{Type: "expandables"})
	}
}

// saveExpandablesSoon writes the store now, off the caller's goroutine
// (persistImmediately); a change during a write is written after it.
func (a *App) saveExpandablesSoon() {
	x := &a.expd
	x.mu.Lock()
	x.dirty = true
	if x.saving {
		x.mu.Unlock()
		return
	}
	x.saving = true
	x.mu.Unlock()
	go func() {
		for {
			x.mu.Lock()
			if !x.dirty {
				x.saving = false
				x.mu.Unlock()
				return
			}
			x.dirty = false
			out := make(map[string]bool, len(x.m))
			for k, on := range x.m {
				if expandable.OnDisk(k) {
					out[k] = on
				}
			}
			x.mu.Unlock()
			a.writeExpandables(out)
		}
	}()
}

// writeExpandables writes expandables.json: never after sign-out.
func (a *App) writeExpandables(m map[string]bool) {
	if a.S.CacheDir() == "" || a.S.Dropped() || a.C.SignedOut() {
		return
	}
	team, user := a.bootAccount()
	b, err := json.Marshal(&expandablesDisk{V: store.CacheVersion, Team: team, User: user, Expanded: m})
	if err != nil {
		return
	}
	if err := a.S.WriteFile(store.ExpandablesFile, b); err != nil {
		log.Printf("cache: expandables: %v", err)
	}
}

// inlineMediaLimit is isImageTooLarge's 0x1400000 bytes (20 MB).
const inlineMediaLimit = 0x1400000

// inlineImgsOff is getUserPref("expand_inline_imgs") === false.
func (a *App) inlineImgsOff() bool {
	v, ok := a.Pref("expand_inline_imgs")
	return ok && !prefTruthy(v, true)
}

// ImageTooLarge is isImageTooLarge (WGgb zE): key has no state, linked
// images expand, and bytes is over 20 MB with obey_inline_img_limit on.
func (a *App) ImageTooLarge(key string, bytes int64) bool {
	if _, ok := a.Expanded(key); ok {
		return false
	}
	return !a.inlineImgsOff() && bytes > inlineMediaLimit && a.PrefBool("obey_inline_img_limit", false)
}

// AttachmentExpanded is isAttachmentExpanded (FDIC $): key's state, else
// expanded unless linked images are off, the image is too large, or the
// sender is an unverified outsider in a DM (CollapseFromUnverifiedUser).
func (a *App) AttachmentExpanded(key string, bytes int64, sender, ch string) bool {
	if on, ok := a.Expanded(key); ok {
		return on
	}
	return !(a.inlineImgsOff() || a.ImageTooLarge(key, bytes) || a.CollapseFromUnverifiedUser(sender, ch))
}

// UnverifiedDisclosure is ExpandableContainer's
// shouldShowUnverifiedDisclosure (pOD6): no state, linked images on, not
// too large, and the sender is an unverified outsider in a DM.
func (a *App) UnverifiedDisclosure(key string, bytes int64, sender, ch string) bool {
	if _, ok := a.Expanded(key); ok || a.inlineImgsOff() || a.ImageTooLarge(key, bytes) {
		return false
	}
	return a.CollapseFromUnverifiedUser(sender, ch)
}

// allowedUnfurlSenders is the allowed_unfurl_senders pref's members (4iNl
// pI: the comma list).
func (a *App) allowedUnfurlSenders() []string {
	s := a.PrefString("allowed_unfurl_senders")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// CollapseFromUnverifiedUser is shouldCollapseFromUnverifiedUser (Ns/U t):
// in a DM or a group DM (zdO/ JM), a sender the viewer hasn't allowed
// (allowed_unfurl_senders) whose team (Xt53 Ux: the viewer's own for a
// member of the viewer's workspace, else the member's team_id) is
// external (MPZo isTeamExternal: neither the viewer's team nor org, not
// in the viewer's enterprise) and not verified (z17y). A team core hasn't
// read yet isn't external (Slack's getTeamById finds none): it is read
// (team.info), and the parts lay out again when it arrives.
func (a *App) CollapseFromUnverifiedUser(sender, ch string) bool {
	if sender == "" || ch == "" {
		return false
	}
	c, ok := a.S.Conv(ch)
	if !ok || !(c.IsIM || c.IsMPIM) {
		return false
	}
	if slices.Contains(a.allowedUnfurlSenders(), sender) {
		return false
	}
	u, ok := a.S.User(sender)
	if !ok {
		return false
	}
	mine := a.CurrentTeamID()
	team := u.TeamID
	if team == mine || (u.IsSlackbot || u.ID == "USLACKBOT") || u.EnterpriseUser != nil && slices.Contains(u.EnterpriseUser.Teams, mine) {
		team = mine
	}
	if team == "" || team == mine || team == a.EnterpriseID() {
		return false
	}
	rec, known := a.teamRecord(team)
	if !known {
		a.readTeamSoon(team)
		return false
	}
	if ent := a.EnterpriseID(); ent != "" && ent == rec.enterpriseID {
		return false
	}
	return !rec.verified
}

// AllowPreviewsFrom is AllowUnfurlsFrom in the background (the UI's
// click).
func (a *App) AllowPreviewsFrom(member string) {
	if member == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.AllowUnfurlsFrom(ctx, member); err != nil {
			log.Printf("allowed_unfurl_senders: %v", err)
		}
	}()
}

// PrefMemberName is getPrefCompliantMemberNameById (R6Qy I): the member's
// name as display_real_names has it, else the id.
func (a *App) PrefMemberName(id string) string {
	u, ok := a.S.User(id)
	if !ok {
		return id
	}
	a.S.RLock()
	realNames := a.S.Prefs.DisplayRealNames
	a.S.RUnlock()
	return prefMemberName(u, realNames)
}

// AllowUnfurlsFrom is UnverifiedDisclosure's "Allow all previews from
// {memberName}": the member joins allowed_unfurl_senders.
func (a *App) AllowUnfurlsFrom(ctx context.Context, member string) error {
	list := a.allowedUnfurlSenders()
	if slices.Contains(list, member) {
		return nil
	}
	return a.SetPref(ctx, "allowed_unfurl_senders", strings.Join(append(list, member), ","))
}

// teamRecord is what team.info said of team: its enterprise and whether
// it is verified (known false before it answered).
type teamRecord struct {
	enterpriseID string
	verified     bool
}

func (a *App) teamRecord(team string) (teamRecord, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.icon.teams[team]
	return r, ok
}

// setTeamRecord keeps team.info's enterprise and is_verified for team;
// a change lays the previews out again (the unverified rule reads them).
func (a *App) setTeamRecord(team string, r teamRecord) {
	if team == "" {
		return
	}
	a.mu.Lock()
	if a.icon.teams == nil {
		a.icon.teams = map[string]teamRecord{}
	}
	old, had := a.icon.teams[team]
	a.icon.teams[team] = r
	a.mu.Unlock()
	if !had || old != r {
		a.expd.gen.Add(1)
		a.emit(Event{Type: "expandables"})
	}
}

// readTeamSoon reads team with team.info once, in the background.
func (a *App) readTeamSoon(team string) {
	a.mu.Lock()
	if a.icon.teamReads == nil {
		a.icon.teamReads = map[string]bool{}
	}
	if a.icon.teamReads[team] {
		a.mu.Unlock()
		return
	}
	a.icon.teamReads[team] = true
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a.fetchTeam(ctx, team)
	}()
}

// ExpandCommand is /collapse (on false) and /expand (on true): Slack's
// collapseCommand and expandCommand (gantry-v2-async-slash-commands-local
// 229d, s/xk). Every message the client holds for ch (Slack's
// messages[ch]: the history, its windows, the threads read, the messages
// fetched by id), in a thread only the thread's (thread_ts threadTS),
// has each of its previews' expandables (getMessageExpandableKeys with
// the command's view context, the composer's) set to on at once
// (bulkSetExpanded). Outside a thread Slackbot says so in ch
// (addEphemeralFeedback with temporary false: a message of Slackbot's
// the next command doesn't take away); in a thread it says nothing.
func (a *App) ExpandCommand(ch, threadTS, viewContext string, on bool) {
	keys := map[string]bool{}
	a.S.VisitMessages(ch, func(string) {}, func(m *slack.Message) {
		if threadTS != "" && m.ThreadTS != threadTS {
			return
		}
		for _, k := range expandable.MessageKeys(m, viewContext) {
			keys[k] = on
		}
	})
	a.BulkSetExpanded(keys)
	if threadTS != "" {
		return
	}
	text := "I’ve collapsed all files in this channel for you."
	if on {
		text = "I’ve expanded all files in this channel for you."
	}
	a.CommandNote(ch, text)
}

// CommandNote is addEphemeralFeedback with temporary false: the
// channel's command feedback goes (removeEphemeralMsgsByType), then
// Slackbot's text stays in ch, untouched by later commands
// (addEphemeralBotMessage with no type).
func (a *App) CommandNote(ch, text string) string {
	a.clearCommandFeedbackIn(ch)
	return a.LocalEphemeral(ch, "", text)
}

// clearCommandFeedbackIn is removeEphemeralMsgsByType for the channel
// command feedback of ch alone.
func (a *App) clearCommandFeedbackIn(ch string) {
	ix := a.Interactive()
	ix.mu.Lock()
	keys := ix.feedback[feedbackType]
	var keep, drop []string
	for _, k := range keys {
		if c, _, _ := strings.Cut(k, "/"); c == ch {
			drop = append(drop, k)
		} else {
			keep = append(keep, k)
		}
	}
	if len(drop) > 0 {
		ix.feedback[feedbackType] = keep
	}
	ix.mu.Unlock()
	for _, k := range drop {
		c, ts, _ := strings.Cut(k, "/")
		if ix.Ephemeral(c, ts) {
			ix.DismissEphemeral(c, ts)
		}
	}
}
