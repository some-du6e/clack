package fakeslack

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Emoji packs (Slack's emoji collections), shaped as Hack Club's
// emoji.collections.list answered on 2026-11-22 (read-only, names and
// counts only): {ok, installed: [...], available: [...]}, each pack
// {id "Ec…", name, author, team_id, locale "en-US", date_create, emoji
// {name: "https://emoji.slack-edge.com/collection/…png"}, is_draft}.
// There, Remote Work (14 emoji), Alphabet (60), Hybrid Work (12),
// Gratitude (13) and Slackbot (9) are available and installed, and
// Halloween (24) is installed only; an installed pack's emoji are the
// workspace's custom emoji in emoji.list, under the pack's names and
// team URLs. The fake's packs have those names, authors, sizes and dates,
// with made-up emoji names, and start with none installed.
//
// emoji.addCollection {collection_id} adds a pack's emoji to the
// workspace's (emoji.list), sends emoji_changed adds for them and then
// emoji_collection_added {collection: {collection_id, emoji}};
// emoji.removeCollection undoes that with an emoji_changed remove and
// emoji_collection_removed {collection_id}. A pack that's in already
// answers already_installed (Slack's code, wv1J); an unknown id
// invalid_collection and removing one that isn't in not_installed (the
// fake's: no write to Slack confirmed them).

// EmojiPack is a fake emoji pack.
type EmojiPack struct {
	ID, Name, Author string
	DateCreate       int64
	Emoji            []string // names, in the answer's order
	// InstalledOnly lists the pack under installed alone (Halloween out
	// of season), never available
	InstalledOnly bool
}

// packTeamID is the team the fake's packs come from (Slack's own).
const packTeamID = "T0PACKS0001"

// The fake's packs' ids.
const (
	PackRemoteWorkID = "Ec014Q4X4YJU"
	PackAlphabetID   = "Ec01EZCUAB0V"
	PackHybridWorkID = "Ec023U3HFPHQ"
	PackGratitudeID  = "Ec02MXLBSQ3V"
	PackSlackbotID   = "Ec0ADQR7189M"
	PackHalloweenID  = "Ec0CBCTN0GNR"
)

// DefaultEmojiPacks is Hack Club's set: five available, Halloween
// installed only.
func DefaultEmojiPacks() []EmojiPack {
	mk := func(id, name, slug string, n int, date string, only bool) EmojiPack {
		t, _ := time.Parse("2006-01-02", date)
		p := EmojiPack{ID: id, Name: name, Author: "Slack", DateCreate: t.Unix() + 61200, InstalledOnly: only}
		for i := 1; i <= n; i++ {
			p.Emoji = append(p.Emoji, fmt.Sprintf("%s-%02d", slug, i))
		}
		return p
	}
	return []EmojiPack{
		mk(PackRemoteWorkID, "Remote Work", "remote", 14, "2020-05-20", false),
		mk(PackAlphabetID, "Alphabet", "alpha", 60, "2020-11-19", false),
		mk(PackHybridWorkID, "Hybrid Work", "hybrid", 12, "2021-06-02", false),
		mk(PackGratitudeID, "Gratitude", "thanks", 13, "2021-11-19", false),
		mk(PackSlackbotID, "Slackbot", "slackbot", 9, "2026-02-10", false),
		mk(PackHalloweenID, "Halloween", "spooky", 24, "2026-10-23", true),
	}
}

type packStore struct {
	mu        sync.Mutex
	set       bool // packs replaced by SetEmojiPacks
	packs     []EmojiPack
	installed map[string]bool
	added     map[string]string // the installed packs' emoji in emoji.list: name → team URL
}

var packStores = newPerServer() // *Server → *packStore

func (s *Server) packStore() *packStore {
	if v, ok := packStores.Load(s); ok {
		return v.(*packStore)
	}
	v, _ := packStores.LoadOrStore(s, &packStore{installed: map[string]bool{}, added: map[string]string{}})
	return v.(*packStore)
}

// SetEmojiPacks replaces the fake's packs (nil: none at all) and installs
// the ones named in installed, their emoji in emoji.list. No events.
func (s *Server) SetEmojiPacks(packs []EmojiPack, installed ...string) {
	ps := s.packStore()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.set, ps.packs = true, append([]EmojiPack(nil), packs...)
	ps.installed, ps.added = map[string]bool{}, map[string]string{}
	for _, p := range ps.packs {
		if p.InstalledOnly {
			ps.install(s, p)
		}
	}
	for _, id := range installed {
		if p, ok := ps.find(s, id); ok {
			ps.install(s, p)
		}
	}
}

// EmojiPackInstalled reports whether pack id is in the workspace (tests).
func (s *Server) EmojiPackInstalled(id string) bool {
	ps := s.packStore()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.installed[id]
}

// list is the fake's packs (the default set until SetEmojiPacks, its
// installed-only packs in the workspace).
func (ps *packStore) list(s *Server) []EmojiPack {
	if !ps.set {
		ps.set, ps.packs = true, DefaultEmojiPacks()
		for _, p := range ps.packs {
			if p.InstalledOnly {
				ps.install(s, p)
			}
		}
	}
	return ps.packs
}

func (ps *packStore) find(s *Server, id string) (EmojiPack, bool) {
	for _, p := range ps.list(s) {
		if p.ID == id {
			return p, true
		}
	}
	return EmojiPack{}, false
}

// install puts p's emoji in the workspace's and returns them (name →
// team URL).
func (ps *packStore) install(s *Server, p EmojiPack) map[string]string {
	ps.installed[p.ID] = true
	out := make(map[string]string, len(p.Emoji))
	for i, n := range p.Emoji {
		u := s.URL + "img/64x64/" + TeamID + "/" + url.PathEscape(n) + "/" + fmt.Sprintf("%016x", uint64(p.DateCreate)<<8|uint64(i)) + ".png"
		ps.added[n], out[n] = u, u
	}
	return out
}

// collectionURL is a pack emoji's image as emoji.collections.list gives
// it (https://emoji.slack-edge.com/collection/…).
func (s *Server) collectionURL(p EmojiPack, name string) string {
	return s.URL + "img/64x64/collection/" + p.ID + "/" + url.PathEscape(name) + ".png"
}

func (s *Server) packJSON(p EmojiPack) map[string]any {
	emoji := orderedEmoji{}
	for _, n := range p.Emoji {
		emoji = append(emoji, [2]string{n, s.collectionURL(p, n)})
	}
	return map[string]any{"id": p.ID, "name": p.Name, "author": p.Author, "team_id": packTeamID,
		"locale": "en-US", "date_create": p.DateCreate, "emoji": emoji, "is_draft": false}
}

// orderedEmoji encodes as a JSON object in its own order (Slack's emoji
// maps keep the pack's order).
type orderedEmoji [][2]string

func (o orderedEmoji) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%q", kv[0], kv[1])
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// withPackEmoji is emoji.list's map with the installed packs' emoji (the
// base map itself when there are none: it never changes after Start).
func (s *Server) withPackEmoji(base map[string]string) map[string]string {
	ps := s.packStore()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.added) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(ps.added))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range ps.added {
		out[k] = v
	}
	return out
}

// handleEmojiPacks answers the emoji collection methods.
func (s *Server) handleEmojiPacks(method string, f url.Values) (any, bool) {
	switch method {
	case "emoji.collections.list", "emoji.addCollection", "emoji.removeCollection":
	default:
		return nil, false
	}
	ps := s.packStore()
	ps.mu.Lock()
	switch method {
	case "emoji.collections.list":
		inst, avail := []any{}, []any{}
		for _, p := range ps.list(s) {
			if ps.installed[p.ID] || p.InstalledOnly {
				inst = append(inst, s.packJSON(p))
			}
			if !p.InstalledOnly && f.Get("installed_only") != "true" {
				avail = append(avail, s.packJSON(p))
			}
		}
		ps.mu.Unlock()
		return ok("installed", inst, "available", avail), true
	case "emoji.addCollection":
		id := f.Get("collection_id")
		p, found := ps.find(s, id)
		switch {
		case !found:
			ps.mu.Unlock()
			return fail("invalid_collection"), true
		case ps.installed[id]:
			ps.mu.Unlock()
			return fail("already_installed"), true
		}
		added := ps.install(s, p)
		ps.mu.Unlock()
		now := time.Now().Unix()
		for _, n := range p.Emoji {
			s.emit(map[string]any{"type": "emoji_changed", "subtype": "add", "name": n, "value": added[n], "event_ts": fmt.Sprintf("%d.000100", now)})
		}
		emoji := orderedEmoji{}
		for _, n := range p.Emoji {
			emoji = append(emoji, [2]string{n, s.collectionURL(p, n)})
		}
		s.emit(map[string]any{"type": "emoji_collection_added", "collection": map[string]any{"collection_id": p.ID, "emoji": emoji}, "event_ts": fmt.Sprintf("%d.000200", now)})
		return ok(), true
	default: // emoji.removeCollection
		id := f.Get("collection_id")
		p, found := ps.find(s, id)
		if !found || !ps.installed[id] {
			ps.mu.Unlock()
			return fail("not_installed"), true
		}
		delete(ps.installed, id)
		for _, n := range p.Emoji {
			delete(ps.added, n)
		}
		ps.mu.Unlock()
		now := time.Now().Unix()
		s.emit(map[string]any{"type": "emoji_changed", "subtype": "remove", "names": p.Emoji, "event_ts": fmt.Sprintf("%d.000100", now)})
		s.emit(map[string]any{"type": "emoji_collection_removed", "collection_id": p.ID, "event_ts": fmt.Sprintf("%d.000200", now)})
		return ok(), true
	}
}
