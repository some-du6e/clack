package core

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/applog"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Warm boot, the way Slack's desktop client does it (client-boot
// getStoredStateIfValid, boot-render setInitialState "rehydrating cached
// data during a warm boot"): the persisted store (store/bootcache.go)
// paints the first frame, then boot fetches as on a cold start and each
// answer replaces its part in one step:
//
//   - conversations, sections, sidebar.dms, experiments, stars: replaced
//     wholesale (a channel left elsewhere leaves the sidebar), kept when
//     the call fails, like a failed refresh in Slack;
//   - counts: the cached ones paint (Slack persists unreadCounts and
//     unreadChannelCounts), client.counts replaces them all at once
//     (bulkSetClientCounts). Until then CountsLive is false: the Dock badge
//     waits for live counts, since the OS shows it outside Clack;
//   - the last conversation's messages: shown from history.json, refetched
//     at once (conversations.history), then the UI re-reads them ("poll");
//   - presence, typing and connection state are never cached (Slack's
//     presence, typing and consistency stores have persistenceEnabled
//     false).

type bootCache struct {
	once   bool
	warm   bool
	hist   string    // the conversation history.json restored
	at     time.Time // when the snapshot was saved
	live   bool      // client.counts answered in this session
	booted bool      // Boot finished with the conversation list
	snap   *Snapshot // the sidebar as the boot snapshot left it
}

// LoadBootCache installs the boot snapshot and the last conversation's
// messages, once, and reports whether there was one (a warm start). It
// reads the disk, so call it before the window opens or off the UI
// goroutine; Boot calls it too.
func (a *App) LoadBootCache() bool {
	a.bcLoad.Lock() // held for the whole load; bcMu guards the fields
	defer a.bcLoad.Unlock()
	a.bcMu.Lock()
	if a.bc.once {
		a.bcMu.Unlock()
		return a.Warm()
	}
	a.bc.once = true
	a.bcMu.Unlock()
	t0 := time.Now()
	if noBootCache {
		applog.Mark("cache-read", "cold: CLACK_NO_BOOT_CACHE")
		return false
	}
	d := a.S.ReadBoot()
	if d == nil || (a.C.Creds.UserID != "" && d.SelfID != a.C.Creds.UserID) {
		applog.Mark("cache-read", "cold: no boot snapshot")
		return false
	}
	var prefs store.Prefs
	var muted map[string]bool
	switch {
	case d.RawPrefs != nil:
		prefs, muted = store.ParsePrefs(d.RawPrefs)
	case d.Prefs != nil:
		prefs, muted = *d.Prefs, map[string]bool{}
		for _, id := range prefs.Muted {
			muted[id] = true
		}
	default:
		prefs, muted = a.S.Prefs, map[string]bool{}
	}
	a.S.InstallBoot(d, prefs, muted)
	if len(d.Experiments) > 0 {
		a.setExperiments(d.Experiments)
	}
	a.mu.Lock()
	if d.DMsKnown {
		a.dms.known = true
		a.dms.open = d.DMsOpen
		if a.dms.open == nil {
			a.dms.open = map[string]bool{}
		}
		a.dms.closedAt = map[string]string{}
		now := nowTS()
		for id, open := range a.dms.open {
			if !open {
				a.dms.closedAt[id] = now
			}
		}
	}
	for _, id := range d.Starred {
		a.starred[id] = true
	}
	a.starsOK = d.StarsOK
	a.mu.Unlock()
	if d.DMsKnown { // closed DMs reopen on messages newer than their latest
		a.mu.Lock()
		closed := make([]string, 0, len(a.dms.closedAt))
		for id := range a.dms.closedAt {
			closed = append(closed, id)
		}
		a.mu.Unlock()
		now := nowTS()
		for _, id := range closed {
			m := a.reopenMark(id, now)
			a.mu.Lock()
			if a.dms.closedAt != nil {
				a.dms.closedAt[id] = m
			}
			a.mu.Unlock()
		}
	}
	nmsg, hist := 0, ""
	if h := a.S.ReadHistory(); h != nil {
		if a.S.SetCachedLatest(h.Channel, h.Msgs, h.HasMore) {
			hist, nmsg = h.Channel, len(h.Msgs)
		}
	}
	at := time.Unix(d.SavedAt, 0)
	a.bcMu.Lock()
	a.bc.warm, a.bc.at, a.bc.hist = true, at, hist
	a.bcMu.Unlock()
	// built here, off the UI goroutine (topics are parsed for every row)
	snap := a.snapshotNow()
	a.bcMu.Lock()
	a.bc.snap = &snap
	a.bcMu.Unlock()
	applog.Mark("cache-read", fmt.Sprintf("warm: %d convs, %d sections, %d users, %d emoji, %d msgs in %s, saved %s ago",
		len(d.Convs), len(d.Sections), len(d.Users), len(d.Emoji), nmsg, time.Since(t0).Round(time.Millisecond),
		time.Since(at).Round(time.Second)))
	return true
}

// Warm reports whether this session started from a boot snapshot.
func (a *App) Warm() bool {
	a.bcMu.Lock()
	defer a.bcMu.Unlock()
	return a.bc.warm
}

// CachedSnapshot is the sidebar from the boot snapshot, before Boot
// finished (Snapshot waits for it). ok is false on a cold start.
func (a *App) CachedSnapshot() (Snapshot, bool) {
	if !a.LoadBootCache() {
		return Snapshot{}, false
	}
	a.bcMu.Lock()
	sp := a.bc.snap
	a.bcMu.Unlock()
	if sp == nil {
		return Snapshot{}, false
	}
	return *sp, true
}

// CountsLive reports whether the unread counts came from client.counts in
// this session rather than from the boot snapshot.
func (a *App) CountsLive() bool {
	a.bcMu.Lock()
	defer a.bcMu.Unlock()
	return a.bc.live || !a.bc.warm
}

func (a *App) setCountsLive() {
	a.bcMu.Lock()
	a.bc.live = true
	a.bcMu.Unlock()
}

// booted reports whether Boot finished with the conversation list.
func (a *App) booted() bool {
	a.bcMu.Lock()
	defer a.bcMu.Unlock()
	return a.bc.booted
}

// RestoredHistory is the conversation whose messages came from
// history.json ("" when none).
func (a *App) RestoredHistory() string { return a.restoredHistory() }

func (a *App) restoredHistory() string {
	a.bcMu.Lock()
	defer a.bcMu.Unlock()
	return a.bc.hist
}

// noBootCache (CLACK_NO_BOOT_CACHE=1) starts without the boot snapshot,
// the way Clack started before it existed (the perf workflow's "before").
var noBootCache = os.Getenv("CLACK_NO_BOOT_CACHE") != ""

var (
	reUserRef  = regexp.MustCompile(`<@([UW][A-Z0-9]+)`)
	reChanRef  = regexp.MustCompile(`<#([CDG][A-Z0-9]+)`)
	reGroupRef = regexp.MustCompile(`subteam\^([A-Z0-9]+)`)
	reEmojiCol = regexp.MustCompile(`:([a-z0-9_+'\-]+):`)
	reEmojiEl  = regexp.MustCompile(`"name":"([a-z0-9_+'\-]+)"`)
	reUserEl   = regexp.MustCompile(`"user_id":"([UW][A-Z0-9]+)"`)
	reChanEl   = regexp.MustCompile(`"channel_id":"([CDG][A-Z0-9]+)"`)
)

// SaveBoot writes the boot snapshot and the open conversation's newest
// messages (store/bootcache.go). Only after a successful Boot, so a failed
// start never replaces a good snapshot; never after sign-out.
func (a *App) SaveBoot() error {
	if a.S.CacheDir() == "" || a.S.Dropped() || a.C.SignedOut() {
		return nil
	}
	a.bcMu.Lock()
	booted := a.bc.booted
	a.bcMu.Unlock()
	if !booted {
		return nil
	}
	t0 := time.Now()
	d := &store.BootSnapshot{SelfID: a.S.SelfID()}
	a.S.RLock()
	d.TeamID, d.TeamName, d.TeamDomain, d.TeamIcon = a.S.TeamID, a.S.TeamName, a.S.TeamDomain, a.S.TeamIcon
	d.RawPrefs = a.S.RawPrefs
	p := a.S.Prefs
	d.Prefs = &p
	d.Convs = make([]slack.Conversation, 0, len(a.S.Convs))
	for _, c := range a.S.Convs {
		d.Convs = append(d.Convs, *c)
	}
	d.Sections = append([]slack.Section(nil), a.S.Sections...)
	d.ThreadsUnread, d.ThreadMentions = a.S.ThreadsUnread, a.S.ThreadMentions
	a.S.RUnlock()
	sort.Slice(d.Convs, func(i, j int) bool { return d.Convs[i].ID < d.Convs[j].ID })
	d.Counts = a.S.CountsList()
	sort.Slice(d.Counts, func(i, j int) bool { return d.Counts[i].ID < d.Counts[j].ID })
	a.mu.Lock()
	d.DMsKnown = a.dms.known
	if a.dms.known {
		d.DMsOpen = make(map[string]bool, len(a.dms.open))
		for k, v := range a.dms.open {
			d.DMsOpen[k] = v
		}
	}
	if a.expGroups != nil {
		d.Experiments = make(map[string]string, len(a.expGroups))
		for k, v := range a.expGroups {
			d.Experiments[k] = v
		}
	}
	for id := range a.starred {
		d.Starred = append(d.Starred, id)
	}
	sort.Strings(d.Starred)
	d.StarsOK = a.starsOK
	sel := a.watchCh
	a.mu.Unlock()

	// the records the sidebar and the cached messages name
	users := []string{d.SelfID}
	var chans, groups, bots, emo []string
	for _, c := range d.Convs {
		if c.IsIM {
			users = append(users, c.User)
		}
		if c.IsMPIM {
			users = append(users, c.Members...)
		}
	}
	for _, s := range d.Sections {
		if s.Emoji != "" {
			emo = append(emo, trimColons(s.Emoji))
		}
	}
	var hist *store.HistorySnapshot
	if sel != "" {
		if t := a.S.Timeline(sel); t != nil && len(t.Msgs) > 0 {
			msgs := t.Msgs
			more := t.HasMore
			if len(msgs) > store.HistoryKeep {
				msgs, more = msgs[len(msgs)-store.HistoryKeep:], true
			}
			hist = &store.HistorySnapshot{Channel: sel, Msgs: msgs, HasMore: more}
			for i := range msgs {
				m := &msgs[i]
				users = append(users, m.User)
				users = append(users, m.ReplyUsers...)
				if m.BotID != "" {
					bots = append(bots, m.BotID)
				}
				for _, r := range m.Reactions {
					emo = append(emo, r.Name)
				}
			}
			if raw, err := json.Marshal(msgs); err == nil {
				for _, m := range reUserRef.FindAllSubmatch(raw, -1) {
					users = append(users, string(m[1]))
				}
				for _, m := range reUserEl.FindAllSubmatch(raw, -1) {
					users = append(users, string(m[1]))
				}
				for _, m := range reChanRef.FindAllSubmatch(raw, -1) {
					chans = append(chans, string(m[1]))
				}
				for _, m := range reChanEl.FindAllSubmatch(raw, -1) {
					chans = append(chans, string(m[1]))
				}
				for _, m := range reGroupRef.FindAllSubmatch(raw, -1) {
					groups = append(groups, string(m[1]))
				}
				for _, m := range reEmojiCol.FindAllSubmatch(raw, -1) {
					emo = append(emo, string(m[1]))
				}
				for _, m := range reEmojiEl.FindAllSubmatch(raw, -1) {
					emo = append(emo, string(m[1]))
				}
			}
		}
	}
	// the sidebar's DM rows show their status emoji
	for _, id := range users {
		if u, ok := a.S.User(id); ok && u.Profile.StatusEmoji != "" {
			emo = append(emo, trimColons(u.Profile.StatusEmoji))
		}
	}
	refs := a.S.CollectRefs(users, chans, groups, bots, emo)
	for i := range refs.Users {
		refs.Users[i].Presence = "" // never cached (Slack's presence store isn't persisted)
	}
	d.Users, d.Names, d.Usergroups, d.Bots, d.Emoji = refs.Users, refs.Names, refs.Usergroups, refs.Bots, refs.Emoji
	if err := a.S.WriteBoot(d); err != nil {
		return err
	}
	if err := a.S.WriteHistory(hist); err != nil {
		return err
	}
	applog.Debugf("cache: boot snapshot saved (%d convs, %d users, %d emoji, history %t) in %s",
		len(d.Convs), len(d.Users), len(d.Emoji), hist != nil, time.Since(t0).Round(time.Millisecond))
	return nil
}

func trimColons(s string) string {
	for len(s) > 0 && s[0] == ':' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ':' {
		s = s[:len(s)-1]
	}
	return s
}

// DropCache deletes this account's disk cache (sign-out): saves stop at
// once, and the files are removed in the background. Any goroutine.
func (a *App) DropCache() <-chan struct{} {
	a.S.StopSaving()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.S.DropDisk()
	}()
	return done
}
