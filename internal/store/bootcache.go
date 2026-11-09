package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The boot snapshot: what Slack's desktop client keeps in its persisted
// redux store (IndexedDB "persist:<app>-<team>-<user>", client-boot's
// getStoredStateFromIDB) so a warm start paints before the network
// answers. Clack keeps the same pieces, per workspace and per user, under
// the user cache directory:
//
//	<cache root>/v1/<team>-<user>/
//	  boot.json     team, self, prefs (ia_theme, window gradients…),
//	                sidebar conversations and sections, unread counts,
//	                which DMs are open, experiments, starred, the users
//	                and custom emoji those rows and the last messages show
//	  history.json  the last open conversation's newest messages
//	  meta.json     the user directory, custom emoji, usergroups (Store.SaveCache)
//	  img/          avatars, emoji and message thumbnails (imgcache)
//
// Every file is 0600 and every directory 0700. Message text is written
// nowhere else, and the whole account directory goes on sign-out
// (DropDisk). A file whose "v" isn't CacheVersion is ignored and
// rewritten, and older vN directories are purged (PurgeOldVersions), as
// Slack drops a store older than its minimum data version
// (isStoreOutOfDate, coldBootReason "store_out_of_date").

// CacheVersion is the disk layout version: the vN directory and every
// file's "v". Bump it when a change makes old snapshots wrong.
const CacheVersion = 1

// AccountDir is the cache directory of one user in one workspace under
// root. team and user are Slack ids; anything else is reduced to letters
// and digits so it can't leave root.
func AccountDir(root, team, user string) string {
	return filepath.Join(root, "v"+strconv.Itoa(CacheVersion), safeID(team)+"-"+safeID(user))
}

func safeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "none"
	}
	return b.String()
}

// PurgeOldVersions removes root's vN directories other than the current
// version's. Slow on a big image cache: call it off the UI goroutine.
func PurgeOldVersions(root string) {
	if root == "" {
		return
	}
	es, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cur := "v" + strconv.Itoa(CacheVersion)
	for _, e := range es {
		n := e.Name()
		if !e.IsDir() || n == cur || len(n) < 2 || n[0] != 'v' {
			continue
		}
		if _, err := strconv.Atoi(n[1:]); err != nil {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, n))
	}
}

// BootSnapshot is boot.json.
type BootSnapshot struct {
	V          int    `json:"v"`
	SavedAt    int64  `json:"saved_at"` // unix seconds
	TeamID     string `json:"team_id"`
	TeamName   string `json:"team_name,omitempty"`
	TeamDomain string `json:"team_domain,omitempty"`
	TeamIcon   string `json:"team_icon,omitempty"` // the rail icon's URL
	SelfID     string `json:"self_id"`
	// RawPrefs is users.prefs.get's map as Slack sent it (Prefs is parsed
	// from it on load, the way the network answer is).
	RawPrefs map[string]json.RawMessage `json:"raw_prefs,omitempty"`
	Prefs    *Prefs                     `json:"prefs,omitempty"`
	Convs    []slack.Conversation       `json:"convs"`
	Sections []slack.Section            `json:"sections,omitempty"`
	// Counts are client.counts as last known. They paint the first
	// frame (Slack persists unreadChannelCounts too) and are replaced in
	// one step when client.counts answers (bulkSetClientCounts).
	Counts         []slack.CountEntry `json:"counts,omitempty"`
	ThreadsUnread  bool               `json:"threads_unread,omitempty"`
	ThreadMentions int                `json:"thread_mentions,omitempty"`
	// CountsAt is when the counts were last fetched (countsLastFetched).
	CountsAt int64 `json:"counts_at,omitempty"`
	// Users the sidebar and the cached messages show (self, DM partners,
	// group DM members, senders).
	Users []slack.User `json:"users,omitempty"`
	// Emoji is the slice of the custom emoji map those rows and messages
	// use (aliases with their targets), so they draw as images before
	// meta.json's full map is read.
	Emoji map[string]string `json:"emoji,omitempty"`
	// DMsKnown/DMsOpen: sidebar.dms' open flags (core's dmState).
	DMsKnown    bool              `json:"dms_known,omitempty"`
	DMsOpen     map[string]bool   `json:"dms_open,omitempty"`
	Experiments map[string]string `json:"experiments,omitempty"`
	Starred     []string          `json:"starred,omitempty"`
	StarsOK     bool              `json:"stars_ok,omitempty"`
	TeamPrefs   json.RawMessage   `json:"team_prefs,omitempty"`
	// What the cached messages mention: channel names outside the
	// sidebar, usergroup handles and bots, so mentions draw as they will
	// once meta.json and the network answer (no width change).
	Names      map[string]string  `json:"names,omitempty"`
	Usergroups map[string]string  `json:"usergroups,omitempty"`
	Bots       map[string]BotInfo `json:"bots,omitempty"`
}

// HistorySnapshot is history.json: the newest messages of the
// conversation open at the last save.
type HistorySnapshot struct {
	V       int             `json:"v"`
	SavedAt int64           `json:"saved_at"`
	Channel string          `json:"channel"`
	Msgs    []slack.Message `json:"msgs"`
	HasMore bool            `json:"has_more,omitempty"`
}

// HistoryKeep is how many of the last conversation's newest messages
// history.json keeps (one screen and a bit; the first page is 50).
const HistoryKeep = 50

var errDropped = errors.New("store: the disk cache was dropped (signed out)")

// ReadBoot reads boot.json (nil when missing, unreadable or of another
// version). Any goroutine; the UI calls it before the window opens.
func (s *Store) ReadBoot() *BootSnapshot {
	if s.cacheDir == "" || s.dropped.Load() {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.cacheDir, "boot.json"))
	if err != nil {
		return nil
	}
	var d BootSnapshot
	if json.Unmarshal(b, &d) != nil || d.V != CacheVersion {
		return nil
	}
	return &d
}

// ReadHistory reads history.json (nil when missing or of another version).
func (s *Store) ReadHistory() *HistorySnapshot {
	if s.cacheDir == "" || s.dropped.Load() {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.cacheDir, "history.json"))
	if err != nil {
		return nil
	}
	var h HistorySnapshot
	if json.Unmarshal(b, &h) != nil || h.V != CacheVersion || h.Channel == "" {
		return nil
	}
	return &h
}

// WriteBoot saves boot.json (atomically, 0600).
func (s *Store) WriteBoot(d *BootSnapshot) error {
	d.V, d.SavedAt = CacheVersion, time.Now().Unix()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.writeFile("boot.json", b)
}

// WriteHistory saves history.json (atomically, 0600); a nil h removes it.
func (s *Store) WriteHistory(h *HistorySnapshot) error {
	if h == nil {
		s.saveMu.Lock()
		defer s.saveMu.Unlock()
		if s.cacheDir == "" || s.dropped.Load() {
			return nil
		}
		err := os.Remove(filepath.Join(s.cacheDir, "history.json"))
		if os.IsNotExist(err) {
			err = nil
		}
		return err
	}
	h.V, h.SavedAt = CacheVersion, time.Now().Unix()
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return s.writeFile("history.json", b)
}

// writeFile writes name in the cache dir through a fresh 0600 temp file
// renamed into place (never through a symlink left at the target).
func (s *Store) writeFile(name string, b []byte) error {
	if s.cacheDir == "" {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.dropped.Load() {
		return errDropped
	}
	return writeAtomic(s.cacheDir, name, b)
}

func writeAtomic(dir, name string, b []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+"-*.tmp") // 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// CacheDir is the account's cache directory ("" without a disk cache).
func (s *Store) CacheDir() string { return s.cacheDir }

// cacheFiles are the files DropDisk removes from the account directory.
var cacheFiles = []string{"boot.json", "history.json", "meta.json", "meta.json.tmp", "sidebar.json"}

// StopSaving makes every later save a no-op (sign-out), at once and
// without touching the disk; DropDisk then deletes the files.
func (s *Store) StopSaving() { s.dropped.Store(true) }

// DropDisk deletes the account's disk cache (sign-out) and stops every
// later save from writing it again. It removes only Clack's own files and
// the img directory, then the directory itself if that left it empty.
// It touches the disk: call it off the UI goroutine.
func (s *Store) DropDisk() {
	s.dropped.Store(true)
	s.saveMu.Lock() // waits out a save already writing
	defer s.saveMu.Unlock()
	dir := s.cacheDir
	if dir == "" {
		return
	}
	for _, n := range cacheFiles {
		_ = os.Remove(filepath.Join(dir, n))
	}
	if es, err := os.ReadDir(dir); err == nil {
		for _, e := range es {
			if n := e.Name(); strings.HasSuffix(n, ".tmp") && strings.HasPrefix(n, ".") {
				_ = os.Remove(filepath.Join(dir, n))
			}
		}
	}
	_ = os.RemoveAll(filepath.Join(dir, "img"))
	_ = os.Remove(dir) // only when empty
}

// Dropped reports whether DropDisk ran.
func (s *Store) Dropped() bool { return s.dropped.Load() }

// InstallBoot fills an empty store from a boot snapshot (core's warm
// boot). The network's answers replace all of it later.
func (s *Store) InstallBoot(d *BootSnapshot, prefs Prefs, muted map[string]bool) {
	s.PutUsers(d.Users)
	if len(d.Emoji) > 0 && s.CustomEmoji.Len() == 0 {
		s.CustomEmoji.Set(d.Emoji)
	}
	s.mu.Lock()
	if s.names == nil {
		s.names = map[string]string{}
	}
	for k, v := range d.Names {
		s.names[k] = v
	}
	for k, v := range d.Usergroups {
		s.Usergroups[k] = v
	}
	if s.bots == nil {
		s.bots = map[string]BotInfo{}
	}
	for k, v := range d.Bots {
		s.bots[k] = v
	}
	s.TeamID, s.TeamName, s.TeamDomain, s.TeamIcon = d.TeamID, d.TeamName, d.TeamDomain, d.TeamIcon
	if u, ok := s.Users[d.SelfID]; ok {
		s.Self = *u
	}
	for i := range d.Convs {
		c := d.Convs[i]
		s.Convs[c.ID] = &c
	}
	s.Sections = d.Sections
	s.Counts = make(map[string]*slack.CountEntry, len(d.Counts))
	for i := range d.Counts {
		e := d.Counts[i]
		s.Counts[e.ID] = &e
	}
	s.ThreadsUnread, s.ThreadMentions = d.ThreadsUnread, d.ThreadMentions
	if d.RawPrefs != nil {
		s.RawPrefs = d.RawPrefs
	}
	s.mu.Unlock()
	s.SetPrefs(prefs, muted)
	s.mu.Lock()
	s.prefsCached = true
	s.mu.Unlock()
}

// ReplaceConvs installs the network's sidebar conversations, dropping
// cached ones Slack no longer lists (left, archived elsewhere).
func (s *Store) ReplaceConvs(cs []slack.Conversation) {
	s.mu.Lock()
	old := s.Convs
	s.Convs = make(map[string]*slack.Conversation, len(cs))
	for i := range cs {
		c := cs[i]
		if o, ok := old[c.ID]; ok {
			if c.NumMembers == 0 {
				c.NumMembers = o.NumMembers
			}
			if c.LastRead == "" {
				c.LastRead = o.LastRead
			}
		}
		s.Convs[c.ID] = &c
	}
	s.mu.Unlock()
}

// CountsList returns every conversation's counts (for boot.json).
func (s *Store) CountsList() []slack.CountEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]slack.CountEntry, 0, len(s.Counts))
	for _, e := range s.Counts {
		out = append(out, *e)
	}
	return out
}

// SetCachedLatest installs a cached first page from history.json. Unlike
// SetLatest it never replaces a timeline the network already filled.
func (s *Store) SetCachedLatest(ch string, msgs []slack.Message, hasMore bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timelines[ch]; t != nil && t.Loaded {
		return false
	}
	s.timelines[ch] = &Timeline{Msgs: dedupe(append([]slack.Message(nil), msgs...)), HasMore: hasMore, Loaded: true}
	return true
}

// Refs is what a boot snapshot keeps besides the sidebar: the records
// the given ids name, from what the store holds.
type Refs struct {
	Users      []slack.User
	Names      map[string]string
	Usergroups map[string]string
	Bots       map[string]BotInfo
	Emoji      map[string]string
}

// CollectRefs returns the known users, channel names (outside the
// sidebar), usergroup handles, bots and custom emoji (with alias targets)
// among the ids and names given.
func (s *Store) CollectRefs(users, chans, groups, bots, emoji []string) Refs {
	r := Refs{Names: map[string]string{}, Usergroups: map[string]string{}, Bots: map[string]BotInfo{}, Emoji: map[string]string{}}
	s.mu.RLock()
	seen := map[string]bool{}
	for _, id := range users {
		if u, ok := s.Users[id]; ok && !seen[id] {
			seen[id] = true
			r.Users = append(r.Users, *u)
		}
	}
	for _, id := range chans {
		if _, ok := s.Convs[id]; ok {
			continue
		}
		if n, ok := s.names[id]; ok {
			r.Names[id] = n
		} else if c, ok := s.ext[id]; ok && c.Name != "" {
			r.Names[id] = c.Name
		}
	}
	for _, id := range groups {
		if h, ok := s.Usergroups[id]; ok {
			r.Usergroups[id] = h
		}
	}
	for _, id := range bots {
		if b, ok := s.bots[id]; ok {
			r.Bots[id] = b
		}
	}
	s.mu.RUnlock()
	m := s.CustomEmoji.Snapshot()
	for _, n := range emoji {
		for i := 0; i < 5; i++ { // the name, then what its alias points at
			v, ok := m[n]
			if !ok {
				break
			}
			r.Emoji[n] = v
			t, alias := strings.CutPrefix(v, "alias:")
			if !alias {
				break
			}
			n = t
		}
	}
	return r
}

// EmojiFull reports whether the custom emoji map is the whole workspace's
// (meta.json or emoji.list), not just a boot snapshot's slice.
func (s *Store) EmojiFull() bool { return s.emojiFull.Load() }
