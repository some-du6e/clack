package fakeslack

// #pins-gallery: the channel Pins tab's and the bookmark folders'
// fixtures (Size.PinsFolders). Synthetic only.
//
//   - Tabs: Pins (the built-in "bookmarks" tab), the "Design docs" folder
//     (data.folder_bookmark_id PinsFolderID), the channel's loose
//     "Bookmarks" folder (no label, no folder id) and the empty "Later"
//     folder.
//   - Five messages, three of them pinned (pins.list newest pin first),
//     one with a reaction; a pinned file (PinsFileID) after them.
//   - Bookmarks: the two folders, four bookmarks in Design docs (an emoji
//     link, an icon link, an untitled link, a file) and two loose links.
//   - bookmarks.list / add / edit / remove / reorder answer from that
//     state and push bookmark_added / bookmark_updated / bookmark_removed
//     in the gateway's shape; conversations.updateTab and removeTab
//     change the tabs. Each call is recorded (PinsFoldersForms).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	PinsChannelID      = "C0PINS00001"
	PinsFolderTabID    = "Ct0PINSFLD1" // "Design docs"
	PinsBookmarksTabID = "Ct0PINSBKS1" // the loose "Bookmarks" folder
	PinsEmptyTabID     = "Ct0PINSEMP1" // "Later" (no bookmarks)
	PinsFolderID       = "Bk0PINSFLD1"
	PinsEmptyFolderID  = "Bk0PINSEMP1"
	PinsFileID         = "F0PINSFILE1" // the pinned file
	PinsDocFileID      = "F0PINSFILE2" // the file in Design docs
	pinsBaseTS         = 1762700000
)

// PinsTS is #pins-gallery's i-th message (0 oldest).
func PinsTS(i int) string { return strconv.Itoa(pinsBaseTS+i*120) + ".000500" }

// PinnedTS are the pinned messages, newest pin first (pins.list's order).
var PinnedTS = []string{PinsTS(4), PinsTS(2), PinsTS(0)}

type pfState struct {
	mu    sync.Mutex
	bms   []map[string]any
	forms map[string][]url.Values
	seq   int
}

var pfStates sync.Map // *Server -> *pfState

func (s *Server) pf() *pfState {
	v, _ := pfStates.LoadOrStore(s, &pfState{forms: map[string][]url.Values{}})
	return v.(*pfState)
}

// PinsFoldersForms returns the calls of method #pins-gallery's handler
// answered.
func (s *Server) PinsFoldersForms(method string) []url.Values {
	st := s.pf()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.forms[method]...)
}

func (s *Server) pinsFile(id string) slack.File {
	switch id {
	case PinsFileID:
		return slack.File{ID: PinsFileID, Name: "q3-metrics.csv", Title: "Q3 metrics", Mimetype: "text/csv", Filetype: "csv",
			PrettyType: "CSV", Size: 18_204, Mode: "hosted", User: s.users[2%len(s.users)].ID, Created: pinsBaseTS - 3600,
			URLPrivate: s.URL + "files-pri/" + PinsFileID + "/q3-metrics.csv",
			Permalink:  s.URL + "files/" + PinsFileID}
	case PinsDocFileID:
		return slack.File{ID: PinsDocFileID, Name: "launch-plan.pdf", Title: "Launch plan", Mimetype: "application/pdf",
			Filetype: "pdf", PrettyType: "PDF", Size: 402_118, Mode: "hosted", User: s.users[1%len(s.users)].ID,
			Created: pinsBaseTS - 7200, URLPrivate: s.URL + "files-pri/" + PinsDocFileID + "/launch-plan.pdf",
			Permalink: s.URL + "files/" + PinsDocFileID}
	}
	return slack.File{}
}

func (s *Server) addPinsChannel() {
	tab := func(id, typ, label, folder string) map[string]any {
		t := map[string]any{"id": id, "type": typ, "label": label}
		if folder != "" {
			t["data"] = map[string]any{"folder_bookmark_id": folder}
		}
		return t
	}
	tabs := []map[string]any{
		tab("bookmarks", "bookmarks", "", ""),
		tab(PinsFolderTabID, "folder", "Design docs", PinsFolderID),
		tab(PinsBookmarksTabID, "folder", "", ""),
		tab(PinsEmptyTabID, "folder", "Later", PinsEmptyFolderID),
	}
	props, _ := json.Marshal(map[string]any{"tabs": tabs, "tabz": tabs})
	s.convs = append(s.convs, slack.Conversation{ID: PinsChannelID, Name: "pins-gallery", IsChannel: true, IsMember: true,
		NumMembers: 4, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Pins and folders fixtures."}, Properties: props})
	u := func(i int) string { return s.users[i%len(s.users)].ID }
	texts := []string{
		"Kickoff notes are in the Design docs folder.",
		"Reminder: the review moves to Thursday.",
		"The *launch checklist* is final, please don't edit it without asking.",
		"Has anyone seen the new icons?",
		"Office hours are every Tuesday at 3pm in the lounge.",
	}
	msgs := make([]slack.Message, len(texts))
	for i, t := range texts {
		msgs[i] = slack.Message{Type: "message", TS: PinsTS(i), User: u(i + 1), Text: t}
	}
	msgs[2].Reactions = []slack.Reaction{{Name: "white_check_mark", Count: 2, Users: []string{u(1), u(3)}}}
	if s.pins == nil {
		s.pins = map[string][]fakePin{}
	}
	for i := len(PinnedTS) - 1; i >= 0; i-- { // oldest pin first: pins.list answers newest first
		ts := PinnedTS[i]
		at := int64(pinsBaseTS + 3600 + (len(PinnedTS)-i)*60)
		s.pins[PinsChannelID] = append(s.pins[PinsChannelID], fakePin{ts: ts, by: u(1), at: at})
		for j := range msgs {
			if msgs[j].TS == ts {
				msgs[j].PinnedTo = []string{PinsChannelID}
				msgs[j].PinnedInfo = &slack.PinnedInfo{Channel: PinsChannelID, PinnedBy: u(1), PinnedTS: at}
			}
		}
	}
	slices.Reverse(msgs) // history is newest first
	s.hist[PinsChannelID] = msgs

	bm := func(id, title, link, typ, rank, parent string) map[string]any {
		return map[string]any{"id": id, "channel_id": PinsChannelID, "title": title, "link": link, "type": typ,
			"rank": rank, "parent_id": parent, "date_created": pinsBaseTS, "emoji": "", "icon_url": "", "entity_id": ""}
	}
	st := s.pf()
	st.mu.Lock()
	st.bms = []map[string]any{
		bm(PinsFolderID, "Design docs", "", "local_folder", "a", ""),
		bm(PinsEmptyFolderID, "Later", "", "local_folder", "b", ""),
		bm("Bk0PINSL1", "Style guide", "https://design.example.com/style", "link", "a", PinsFolderID),
		bm("Bk0PINSL2", "Roadmap", "https://roadmap.example.com/q4", "link", "b", PinsFolderID),
		bm("Bk0PINSL3", "", "https://wiki.example.org/handbook", "link", "c", PinsFolderID),
		bm("Bk0PINSF1", "Launch plan", s.URL+"files/"+PinsDocFileID, "file", "d", PinsFolderID),
		bm("Bk0PINSL4", "Team calendar", "https://calendar.example.com/team", "link", "c", ""),
		bm("Bk0PINSL5", "Status page", "https://status.example.com", "link", "d", ""),
	}
	st.bms[2]["emoji"] = ":art:"
	st.bms[3]["icon_url"] = s.URL + "img/32x32/roadmap-favicon.png"
	st.bms[5]["entity_id"] = PinsDocFileID
	st.mu.Unlock()
}

// bookmarkEvent is a bookmark_* event in the gateway's shape
// (extractBookmark's fields).
func bookmarkEvent(typ string, b map[string]any) map[string]any {
	return map[string]any{"type": typ, "channel": b["channel_id"], "bookmark": b["id"], "title": b["title"], "link": b["link"],
		"emoji": b["emoji"], "image_url": b["icon_url"], "entity_id": b["entity_id"], "rank": b["rank"],
		"bookmark_type": b["type"], "parent_id": b["parent_id"], "date_create": b["date_created"]}
}

func (st *pfState) find(id string) int {
	return slices.IndexFunc(st.bms, func(b map[string]any) bool { return b["id"] == id })
}

func (s *Server) handlePinsFolders(method string, f url.Values) (any, bool) {
	if !s.Size.PinsFolders {
		return nil, false
	}
	ch := f.Get("channel")
	if ch == "" {
		ch = f.Get("channel_id")
	}
	if method == "files.info" {
		if fl := s.pinsFile(f.Get("file")); fl.ID != "" {
			return ok("file", fl, "comments", []any{}), true
		}
		return nil, false
	}
	if ch != PinsChannelID {
		return nil, false
	}
	st := s.pf()
	record := func() {
		st.mu.Lock()
		st.forms[method] = append(st.forms[method], f)
		st.mu.Unlock()
	}
	switch method {
	case "pins.add", "pins.remove": // recorded; pins.go answers
		record()
		return nil, false
	case "pins.list":
		record()
		out, _ := s.handlePinsList(ch)
		items := out["items"].([]map[string]any)
		items = append(items, map[string]any{"type": "file", "created": pinsBaseTS + 3000, "created_by": s.users[2%len(s.users)].ID,
			"file": s.pinsFile(PinsFileID)})
		out["items"] = items
		return out, true
	case "bookmarks.list":
		record()
		st.mu.Lock()
		out := make([]map[string]any, 0, len(st.bms))
		for _, b := range st.bms {
			c := map[string]any{}
			for k, v := range b {
				c[k] = v
			}
			out = append(out, c)
		}
		st.mu.Unlock()
		return ok("bookmarks", out), true
	case "bookmarks.add":
		record()
		st.mu.Lock()
		st.seq++
		b := map[string]any{"id": fmt.Sprintf("Bk0PINSNEW%d", st.seq), "channel_id": ch, "title": f.Get("title"),
			"link": f.Get("link"), "type": f.Get("type"), "parent_id": f.Get("parent_id"), "rank": "z" + strconv.Itoa(st.seq),
			"date_created": pinsBaseTS + 7200, "emoji": "", "icon_url": "", "entity_id": f.Get("entity_id")}
		st.bms = append(st.bms, b)
		st.mu.Unlock()
		s.Push(bookmarkEvent("bookmark_added", b))
		return ok("bookmark", b), true
	case "bookmarks.edit":
		record()
		st.mu.Lock()
		i := st.find(f.Get("bookmark"))
		if i < 0 {
			st.mu.Unlock()
			return map[string]any{"ok": false, "error": "not_found"}, true
		}
		b := st.bms[i]
		b["title"], b["link"] = f.Get("title"), f.Get("link")
		if e := f.Get("emoji"); e != "" {
			b["emoji"] = e
		}
		st.mu.Unlock()
		s.Push(bookmarkEvent("bookmark_updated", b))
		return ok("bookmark", b), true
	case "bookmarks.remove":
		record()
		st.mu.Lock()
		i := st.find(f.Get("bookmark"))
		if i < 0 {
			st.mu.Unlock()
			return map[string]any{"ok": false, "error": "not_found"}, true
		}
		st.bms = slices.Delete(st.bms, i, i+1)
		st.mu.Unlock()
		s.Push(map[string]any{"type": "bookmark_removed", "channel": ch, "bookmark": f.Get("bookmark")})
		return ok(), true
	case "bookmarks.reorder":
		record()
		return s.pfReorder(f), true
	case "conversations.updateTab", "conversations.removeTab":
		record()
		s.pfTabs(method, f)
		return ok(), true
	}
	return nil, false
}

// pfReorder gives center a rank between left's and right's (the
// neighbours it was dropped between), as Slack's server does, leaving the
// rest as they are; it pushes bookmark_updated for it and answers with it.
func (s *Server) pfReorder(f url.Values) any {
	st := s.pf()
	st.mu.Lock()
	ci := st.find(f.Get("center"))
	if ci < 0 {
		st.mu.Unlock()
		return map[string]any{"ok": false, "error": "not_found"}
	}
	rank := func(id string) string {
		if i := st.find(id); i >= 0 && id != "" {
			r, _ := st.bms[i]["rank"].(string)
			return r
		}
		return ""
	}
	center := st.bms[ci]
	center["rank"] = rankBetween(rank(f.Get("left")), rank(f.Get("right")))
	if p := f.Get("parent_id"); p != "" {
		center["parent_id"] = p
	}
	out := map[string]any{}
	for k, v := range center {
		out[k] = v
	}
	st.mu.Unlock()
	s.Push(bookmarkEvent("bookmark_updated", out))
	return ok("bookmark", out)
}

const rankDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

// rankBetween is a rank that sorts after lo and before hi ("" for
// either: no bound).
func rankBetween(lo, hi string) string {
	var out []byte
	for i := 0; ; i++ {
		a := 0
		if i < len(lo) {
			a = strings.IndexByte(rankDigits, lo[i])
		}
		b := len(rankDigits)
		if hi != "" && i < len(hi) {
			b = strings.IndexByte(rankDigits, hi[i])
		}
		if a == b {
			out = append(out, rankDigits[a])
			continue
		}
		if b-a > 1 {
			return string(append(out, rankDigits[(a+b)/2]))
		}
		out = append(out, rankDigits[a])
		hi = "" // past this digit only lo bounds it
	}
}

// pfTabs applies conversations.updateTab (a label) or removeTab to the
// channel's tabs.
func (s *Server) pfTabs(method string, f url.Values) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for i := range s.convs {
		if s.convs[i].ID != PinsChannelID {
			continue
		}
		var p map[string][]map[string]any
		if json.Unmarshal(s.convs[i].Properties, &p) != nil {
			return
		}
		for _, k := range []string{"tabs", "tabz"} {
			ts := p[k]
			for j := 0; j < len(ts); j++ {
				if ts[j]["id"] != f.Get("id") {
					continue
				}
				if method == "conversations.removeTab" {
					ts = slices.Delete(ts, j, j+1)
					j--
					continue
				}
				ts[j]["label"] = f.Get("label")
			}
			p[k] = ts
		}
		s.convs[i].Properties, _ = json.Marshal(p)
	}
}
