package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The warm boot's files. Slack's client keeps its redux store in IndexedDB
// per team and user (client-boot getStoredStateIfValid) and, when one is
// there, renders the client from it before any boot call answers (core's
// warmboot.go has the details). Clack keeps the part of that store the
// first frame shows, in the account directory next to meta.json:
//
//	boot_data.json  the team, the sidebar's conversations (their latest
//	                message dropped), sections and unread counts, which
//	                DMs are open, the starred conversations and the
//	                Activity badge counts (BootData)
//	headers.json    the tab bars, member counts and facepiles of the
//	                channels opened most recently (core's header cache)
//
// Neither holds message content. Both name the account they belong to and
// are read only for it. Sign-out deletes them first (SignOutNow), a session
// that ended deletes them (RemoveWarmFiles), and a start finishes a
// sign-out its process didn't live through (SweepAccounts). Another
// signed-in account's stay, as Slack keeps a persisted store per team
// (the account switcher's saved accounts).

// BootDataFile and HeadersFile are the warm boot's files in the account
// directory.
const (
	BootDataFile = "boot_data.json"
	HeadersFile  = "headers.json"
)

// BootData is boot_data.json: the sidebar as the last session left it.
type BootData struct {
	V       int   `json:"v"`
	SavedAt int64 `json:"saved_at"` // unix seconds
	// Team and User are the session's credentials (Creds.Team, or the
	// org on Grid, and Creds.UserID): ReadBootData returns nothing for
	// another account.
	Team string `json:"team"`
	User string `json:"user"`

	TeamID     string `json:"team_id,omitempty"` // team.info's
	TeamName   string `json:"team_name,omitempty"`
	TeamDomain string `json:"team_domain,omitempty"`
	TeamIcon   string `json:"team_icon,omitempty"` // the rail icon's URL

	Convs    []slack.Conversation `json:"convs"`
	Sections []slack.Section      `json:"sections,omitempty"`
	// Counts are client.counts as last known (Slack persists
	// unreadCounts and unreadChannelCounts); client.counts replaces them
	// all at once when it answers.
	Counts         []slack.CountEntry `json:"counts,omitempty"`
	ThreadsUnread  bool               `json:"threads_unread,omitempty"`
	ThreadMentions int                `json:"thread_mentions,omitempty"`
	ThreadsByChan  map[string]int     `json:"threads_by_channel,omitempty"`

	// core's parts: sidebar.dms' open flags, stars.list and client.counts'
	// activity_v2 (each only when it had answered).
	DMsKnown       bool            `json:"dms_known,omitempty"`
	DMsOpen        map[string]bool `json:"dms_open,omitempty"`
	StarsKnown     bool            `json:"stars_known,omitempty"`
	Starred        []string        `json:"starred,omitempty"`
	ActivityBadges map[string]int  `json:"activity_badges,omitempty"`

	// Emoji are the custom emoji a launch's first frame draws (the
	// sections' icons, the sidebar's DM people's and the viewer's status
	// emoji, the cached tab bars' bookmark icons), name to value as in
	// the full map, aliases with their targets: meta.json's 65k come after
	// the first frame (LoadCacheBase), these before it (InstallBootEmoji).
	Emoji map[string]string `json:"emoji,omitempty"`
}

var errDropped = errors.New("store: the disk cache was dropped (signed out)")

// WriteFile writes one of the account's cache files (name in its
// directory, 0600, through a temp file renamed into place). After
// StopSaving or DropDisk (sign-out) it writes nothing. Disk I/O: call it
// off the UI goroutine.
func (s *Store) WriteFile(name string, b []byte) error {
	if s.cacheDir == "" {
		return nil
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if s.dropped.Load() || s.warmGone && (name == BootDataFile || name == HeadersFile) {
		return errDropped // signed out, or the session ended (RemoveWarmFiles): a save already past its own checks
	}
	return writeAtomic(s.cacheDir, name, b)
}

// ReadFile reads one of the account's cache files (os.ErrNotExist without
// a cache directory, or once the cache was dropped). Disk I/O: call it off
// the UI goroutine.
func (s *Store) ReadFile(name string) ([]byte, error) {
	if s.cacheDir == "" || s.dropped.Load() {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(s.cacheDir, name))
}

// ReadBootData reads boot_data.json for the account (team, user): nil when
// it's missing, unreadable, of another layout version or another account,
// or lists no conversation.
func (s *Store) ReadBootData(team, user string) *BootData {
	b, err := s.ReadFile(BootDataFile)
	if err != nil {
		return nil
	}
	var d BootData
	if json.Unmarshal(b, &d) != nil || d.V != CacheVersion || d.Team != team || d.User != user || len(d.Convs) == 0 {
		return nil
	}
	return &d
}

// WriteBootData saves boot_data.json (WriteFile). The conversations'
// latest messages are never written.
func (s *Store) WriteBootData(d *BootData) error {
	d.V, d.SavedAt = CacheVersion, time.Now().Unix()
	for i := range d.Convs {
		d.Convs[i].Latest = nil // a message (conversations.info's latest)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.WriteFile(BootDataFile, b)
}

// BootSidebar copies what boot_data.json keeps from the store: the team,
// the conversations (latest messages dropped), sections, counts and thread
// state.
func (s *Store) BootSidebar(d *BootData) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d.TeamID, d.TeamName, d.TeamDomain, d.TeamIcon = s.TeamID, s.TeamName, s.TeamDomain, s.TeamIcon
	d.Convs = make([]slack.Conversation, 0, len(s.Convs))
	for _, c := range s.Convs {
		cc := *c
		cc.Latest = nil
		d.Convs = append(d.Convs, cc)
	}
	d.Sections = append([]slack.Section(nil), s.Sections...)
	d.Counts = make([]slack.CountEntry, 0, len(s.Counts))
	for _, e := range s.Counts {
		d.Counts = append(d.Counts, *e)
	}
	d.ThreadsUnread, d.ThreadMentions = s.ThreadsUnread, s.ThreadMentions
	if len(s.ThreadsUnreadByChannel) > 0 {
		d.ThreadsByChan = make(map[string]int, len(s.ThreadsUnreadByChannel))
		for k, v := range s.ThreadsUnreadByChannel {
			d.ThreadsByChan[k] = v
		}
	}
}

// InstallBootEmoji installs the boot data's few custom emoji while the
// emoji map is still empty (meta.json's comes after the first frame and
// replaces them; a map from emoji.list is newer and stays).
func (s *Store) InstallBootEmoji(m map[string]string) {
	if len(m) == 0 || s.CustomEmoji.Len() > 0 {
		return
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	// partial before the map: a full map installed in between clears it
	// again (installCacheRest, SetEmojiPacked)
	s.emojiPartial.Store(true)
	if !s.CustomEmoji.SetIfEmpty(cp) {
		s.emojiPartial.Store(false)
	}
}

// EmojiPartial reports whether the emoji map is only the boot data's few
// (InstallBootEmoji): meta.json had none, so emoji.list is due.
func (s *Store) EmojiPartial() bool { return s.emojiPartial.Load() }

// BootEmoji is the custom emoji map's entries for names (":" trimmed),
// with each alias's targets: the boot data's few (BootData.Emoji).
func (s *Store) BootEmoji(names []string) map[string]string {
	m := s.CustomEmoji.Snapshot()
	if len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, n := range names {
		n = strings.Trim(n, ":")
		for i := 0; n != "" && i < 6; i++ {
			v, ok := m[n]
			if !ok {
				break
			}
			out[n] = v
			if !strings.HasPrefix(v, "alias:") {
				break
			}
			n = strings.TrimPrefix(v, "alias:")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// InstallBootData fills the store from boot data (a warm boot), keeping
// whatever is already there: the team (team.info answered), each
// conversation and each conversation's unread state (a mark made before
// the disk read finished keeps its own, every other cached count still
// installs), the sections, and the threads' state (client.counts
// answered, or an update_thread_state taken: SetThreadState). It returns
// the conversations it added, for DropBootConvs.
func (s *Store) InstallBootData(d *BootData) map[string]*slack.Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.TeamID == "" {
		s.TeamID, s.TeamName, s.TeamDomain, s.TeamIcon = d.TeamID, d.TeamName, d.TeamDomain, d.TeamIcon
	}
	added := make(map[string]*slack.Conversation, len(d.Convs))
	for i := range d.Convs {
		c := d.Convs[i]
		if _, ok := s.Convs[c.ID]; ok || c.ID == "" {
			continue
		}
		s.Convs[c.ID] = &c
		added[c.ID] = &c
	}
	if s.Sections == nil {
		s.Sections = append([]slack.Section(nil), d.Sections...)
	}
	for i := range d.Counts {
		e := d.Counts[i]
		if _, ok := s.Counts[e.ID]; !ok {
			s.Counts[e.ID] = &e
		}
	}
	if !s.countsLive && s.jr.threads == 0 { // live state is newer than the disk's
		s.ThreadsUnread, s.ThreadMentions = d.ThreadsUnread, d.ThreadMentions
		s.ThreadsUnreadByChannel = d.ThreadsByChan
	}
	if u, ok := s.Users[s.Self.ID]; ok && s.Self.Name == "" {
		s.Self = *u
	}
	return added
}

// DropBootConvs removes the conversations boot data added that the live
// list (users.conversations, fetched at journal position since) no longer
// has: a channel left or archived elsewhere since the boot data was
// written. The live list is what the viewer is in, so this holds even
// when a read refreshed the cached copy meanwhile (the restored channel's
// conversations.info at start); only a change this session made after
// since (a channel joined again) keeps one.
func (s *Store) DropBootConvs(added map[string]*slack.Conversation, live []slack.Conversation, since uint64) int {
	if len(added) == 0 {
		return 0
	}
	in := make(map[string]bool, len(live))
	for i := range live {
		in[live[i].ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id := range added {
		if in[id] || s.jr.convs[id] > since {
			continue
		}
		if _, ok := s.Convs[id]; ok {
			delete(s.Convs, id)
			n++
		}
	}
	return n
}
