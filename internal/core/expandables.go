package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
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
// collapsed after it. Only keys made of ids go to disk (expandable.OnDisk):
// those of an attachment without an id carry its image URL or embed
// HTML, and stay in memory (no message content goes to disk). Like
// Slack's (Ua4u), the store is never pruned. Sign-out deletes the file
// with the account's directory (store's cacheFiles).
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
	// keep: the file was there but couldn't be read (a sharing
	// violation, EIO), so this session writes nothing over the choices
	// it may hold
	keep   bool
	dirty  bool // changed since the last write
	saving bool // a write is under way (saveExpandablesSoon's goroutine)
	gen    atomic.Uint64
	// allowMu serialises AllowUnfurlsFrom: each reads the list the one
	// before it wrote
	allowMu sync.Mutex
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
// at Boot's warm load, alongside boot_data: the keys set this session
// already keep their state. A missing file is an empty store, as is one
// that doesn't decode (or another layout's or account's): the next change
// replaces it. A file that is there but can't be read is left alone:
// nothing this session writes replaces the choices it may hold.
func (a *App) loadExpandables(team, user string) {
	var d expandablesDisk
	ok, keep := false, false
	if team != "" || user != "" {
		b, err := a.S.ReadFile(store.ExpandablesFile)
		switch {
		case err == nil:
			ok = json.Unmarshal(b, &d) == nil && d.V == store.CacheVersion && d.Team == team && d.User == user
		case !errors.Is(err, fs.ErrNotExist):
			keep = true
			log.Printf("cache: expandables: %v (kept: nothing is written over it this session)", err)
		}
	}
	x := &a.expd
	x.mu.Lock()
	if x.loaded {
		x.mu.Unlock()
		return
	}
	x.loaded, x.keep = true, keep
	dirty := x.dirty // set this session before the read: written with it
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
	if dirty {
		a.saveExpandablesSoon()
	}
}

// expandablesWriting reports whether a write of the store is under way
// or due (tests wait it out).
func (a *App) expandablesWriting() bool {
	x := &a.expd
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.saving || x.dirty && x.loaded && !x.keep
}

// saveExpandablesSoon writes the store now, off the caller's goroutine
// (persistImmediately); a change during a write is written after it.
// Shutdown and Close wait for the write (a.saves).
func (a *App) saveExpandablesSoon() {
	x := &a.expd
	x.mu.Lock()
	x.dirty = true
	// not before the disk's copy is in (it would be lost: loadExpandables
	// writes then), nor over a file that couldn't be read
	if x.saving || !x.loaded || x.keep {
		x.mu.Unlock()
		return
	}
	x.saving = true
	x.mu.Unlock()
	a.saves.Add(1)
	go func() {
		defer a.saves.Done()
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
// images expand, and bytes is over 20 MB with obey_inline_img_limit on
// (on without the pref, as the store's Prefs and Preferences read it).
func (a *App) ImageTooLarge(key string, bytes int64) bool {
	if _, ok := a.Expanded(key); ok {
		return false
	}
	return !a.inlineImgsOff() && bytes > inlineMediaLimit && a.PrefBool("obey_inline_img_limit", true)
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
// in the viewer's enterprise, one of externalTeamIds) and not verified
// (z17y). A team core hasn't read yet isn't external (Slack's getTeamById
// finds none): it is read (team.info), and the parts lay out again when
// it arrives.
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
	// isTeamExternal's last test (V/NW Rr): the team is one of
	// externalTeamIds, the connected teams Slack's setConnectedTeamIds adds
	// from every conversation's connected_team_ids (j08h
	// addExternalTeamId). A team no conversation the client holds is
	// connected to isn't external (a former Slack Connect org in an old
	// DM), and isn't read either. (Slack keeps the set across restarts;
	// Clack reads it from the conversations it holds.)
	if !a.S.ConnectedTeam(team) {
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
// Calls run one at a time: a second sender allowed before the first's
// users.prefs.set answered is added to the list the first one wrote. The
// pref landing lays the previews out again (expandablesPrefsChanged).
func (a *App) AllowUnfurlsFrom(ctx context.Context, member string) error {
	a.expd.allowMu.Lock()
	defer a.expd.allowMu.Unlock()
	list := a.allowedUnfurlSenders()
	if slices.Contains(list, member) {
		return nil
	}
	return a.SetPref(ctx, "allowed_unfurl_senders", strings.Join(append(list, member), ","))
}

// expandablesPrefs are the prefs the expandables' defaults read.
var expandablesPrefs = []string{"allowed_unfurl_senders", "expand_inline_imgs", "obey_inline_img_limit", "expand_internal_inline_imgs"}

// expandablesPrefsDiffer reports whether old and raw disagree on one of
// expandablesPrefs.
func expandablesPrefsDiffer(old, raw map[string]json.RawMessage) bool {
	for _, k := range expandablesPrefs {
		if !bytes.Equal(old[k], raw[k]) {
			return true
		}
	}
	return false
}

// expandablesPrefsChanged lays the previews out again: a pref their
// defaults read changed (users.prefs, a pref_change, "Allow all previews
// from {memberName}"), as Slack's selectors re-render them.
func (a *App) expandablesPrefsChanged() {
	a.expd.gen.Add(1)
	a.emit(Event{Type: "expandables"})
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
// the next command doesn't take away); in a thread it says nothing. It
// returns that message's ts ("" for none).
func (a *App) ExpandCommand(ch, threadTS, viewContext string, on bool) string {
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
		return ""
	}
	text := "I’ve collapsed all files in this channel for you."
	if on {
		text = "I’ve expanded all files in this channel for you."
	}
	return a.CommandNote(ch, text)
}

// CommandNote is addEphemeralFeedback with temporary false: the
// channel's command feedback goes (removeEphemeralMsgsByType), then
// Slackbot's text stays in ch, untouched by later commands
// (addEphemeralBotMessage with no type; ephemeral_feedback.go).
func (a *App) CommandNote(ch, text string) string {
	return a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: ch, Text: text}, Lasting: true})
}
