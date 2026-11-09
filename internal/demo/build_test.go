package demo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

const testBase = "http://127.0.0.1:41234/"

// buildNows are the clocks Build is checked against: a Monday morning
// (the first workday back is the previous Friday), a Saturday night and
// an early Wednesday, so the workday arithmetic and the clamp to "never
// in the future" are both exercised.
func buildNows() []time.Time {
	ny := location(branchTZ)
	return []time.Time{
		time.Date(2026, 11, 9, 10, 30, 0, 0, ny),
		time.Date(2026, 11, 14, 22, 5, 0, 0, ny),
		time.Date(2026, 7, 1, 6, 45, 0, 0, ny), // daylight saving time
	}
}

var (
	tsRE      = regexp.MustCompile(`^\d{10}\.\d{6}$`)
	userRefRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)
	chanRefRE = regexp.MustCompile(`<#([CDG][A-Z0-9]+)(?:\|[^>]*)?>`)
	emojiRE   = regexp.MustCompile(`:([a-z0-9_+'-]+):`)
)

func TestBuild(t *testing.T) {
	for _, now := range buildNows() {
		ws, err := Build(testBase, now)
		if err != nil {
			t.Fatalf("Build(%v): %v", now, err)
		}
		checkWorkspace(t, ws, now)
	}
}

func TestBuildDeterministic(t *testing.T) {
	now := buildNows()[0]
	a, err := Build(testBase, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Build(testBase, now)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !bytes.Equal(ja, jb) {
		t.Fatal("two Builds at the same now differ")
	}
}

func checkWorkspace(t *testing.T, ws *Workspace, now time.Time) {
	t.Helper()
	users := map[string]bool{}
	for _, u := range ws.Users {
		if users[u.ID] {
			t.Errorf("user %s twice", u.ID)
		}
		users[u.ID] = true
	}
	for _, id := range []string{SelfID, InkwellID, PulseID} {
		if !users[id] {
			t.Errorf("no user %s", id)
		}
	}
	if ws.Self.ID != SelfID {
		t.Errorf("Self is %q", ws.Self.ID)
	}
	convs := map[string]slack.Conversation{}
	for _, c := range ws.Convs {
		if _, dup := convs[c.ID]; dup {
			t.Errorf("conversation %s twice", c.ID)
		}
		convs[c.ID] = c
		if c.Created == 0 || c.ContextTeamID != TeamID {
			t.Errorf("%s: Created %d, ContextTeamID %q", c.ID, c.Created, c.ContextTeamID)
		}
		if c.IsIM {
			if !users[c.User] {
				t.Errorf("%s: IM with unknown %s", c.ID, c.User)
			}
			continue
		}
		if !users[c.Creator] {
			t.Errorf("%s: unknown creator %q", c.ID, c.Creator)
		}
		if c.NumMembers == 0 || !contains(c.Members, SelfID) {
			t.Errorf("%s: %d members, viewer included %v", c.ID, c.NumMembers, contains(c.Members, SelfID))
		}
		for _, m := range c.Members {
			if !users[m] {
				t.Errorf("%s: unknown member %s", c.ID, m)
			}
		}
		if c.Purpose == nil || c.Purpose.Value == "" {
			t.Errorf("%s: no purpose", c.ID)
		}
	}
	for _, id := range []string{GeneralID, RandomID, SalesID, PartyID, ITHelpID, WareID, LunchID, KudosID, SantaID, CodeID, GroupDMID} {
		if _, ok := convs[id]; !ok {
			t.Errorf("no conversation %s", id)
		}
	}
	if c := convs[CodeID]; len(c.Properties) == 0 || !json.Valid(c.Properties) {
		t.Error("the code channel has no properties")
	}

	seen := map[string]string{} // ts -> channel: unique across the workspace
	checkMsg := func(ch string, m slack.Message) {
		if !tsRE.MatchString(m.TS) {
			t.Errorf("%s: ts %q", ch, m.TS)
		}
		if prev, dup := seen[m.TS]; dup {
			t.Errorf("%s: ts %s also in %s", ch, m.TS, prev)
		}
		seen[m.TS] = ch
		if !tsLess(m.TS, fmt.Sprintf("%d.000000", now.Unix())) {
			t.Errorf("%s %s: in the future of %v", ch, m.TS, now)
		}
		if !users[m.User] {
			t.Errorf("%s %s: unknown author %q", ch, m.TS, m.User)
		}
		for _, r := range m.Reactions {
			if r.Count != len(r.Users) || r.Count == 0 {
				t.Errorf("%s %s: reaction %s count %d of %d", ch, m.TS, r.Name, r.Count, len(r.Users))
			}
			for _, u := range r.Users {
				if !users[u] {
					t.Errorf("%s %s: reaction by unknown %s", ch, m.TS, u)
				}
			}
		}
		for _, ref := range userRefRE.FindAllStringSubmatch(m.Text+string(m.Blocks), -1) {
			if !users[ref[1]] {
				t.Errorf("%s %s: mentions unknown %s", ch, m.TS, ref[1])
			}
		}
		for _, ref := range chanRefRE.FindAllStringSubmatch(m.Text, -1) {
			if _, ok := convs[ref[1]]; !ok {
				t.Errorf("%s %s: links unknown %s", ch, m.TS, ref[1])
			}
		}
		if strings.Contains(m.Text, "@") && regexp.MustCompile(`(^|\s)@[a-z]+\b`).MatchString(m.Text) {
			t.Errorf("%s %s: unresolved mention in %q", ch, m.TS, m.Text)
		}
		if len(m.Blocks) > 0 && !json.Valid(m.Blocks) {
			t.Errorf("%s %s: invalid blocks", ch, m.TS)
		}
		if m.BotID != "" && m.BotProfile == nil {
			t.Errorf("%s %s: a bot message without bot_profile", ch, m.TS)
		}
	}
	for _, c := range ws.Convs {
		h := ws.History[c.ID]
		if len(h) == 0 {
			t.Errorf("%s: no history", c.ID)
			continue
		}
		for i, m := range h {
			checkMsg(c.ID, m)
			if i > 0 && !tsLess(m.TS, h[i-1].TS) {
				t.Errorf("%s: history not newest first at %s", c.ID, m.TS)
			}
			if m.ThreadTS != "" && m.ThreadTS != m.TS {
				t.Errorf("%s %s: a reply in the history", c.ID, m.TS)
			}
			replies := ws.Replies[c.ID][m.TS]
			if m.ReplyCount != len(replies) {
				t.Errorf("%s %s: ReplyCount %d, %d replies", c.ID, m.TS, m.ReplyCount, len(replies))
			}
			if len(replies) == 0 {
				continue
			}
			if m.LatestReply != replies[len(replies)-1].TS {
				t.Errorf("%s %s: LatestReply %s", c.ID, m.TS, m.LatestReply)
			}
			authors := map[string]bool{}
			prev := m.TS
			for _, r := range replies {
				checkMsg(c.ID, r)
				authors[r.User] = true
				if r.ThreadTS != m.TS || !tsLess(prev, r.TS) {
					t.Errorf("%s %s: reply %s out of order or thread", c.ID, m.TS, r.TS)
				}
				prev = r.TS
			}
			if len(authors) != len(m.ReplyUsers) {
				t.Errorf("%s %s: ReplyUsers %v", c.ID, m.TS, m.ReplyUsers)
			}
		}
	}
	for ch, threads := range ws.Replies {
		for ts := range threads {
			found := false
			for _, m := range ws.History[ch] {
				found = found || m.TS == ts
			}
			if !found {
				t.Errorf("%s: replies to missing parent %s", ch, ts)
			}
		}
	}

	// pins, read states, sections
	for ch, pins := range ws.Pins {
		for _, ts := range pins {
			if _, ok := seen[ts]; !ok || seen[ts] != ch {
				t.Errorf("%s: pin of missing %s", ch, ts)
			}
		}
	}
	for _, c := range ws.Convs {
		u := ws.Unread[c.ID]
		h := ws.History[c.ID]
		var unread []slack.Message
		for _, m := range h {
			if u.LastRead != "" && tsLess(u.LastRead, m.TS) {
				unread = append(unread, m)
			}
		}
		mentions := 0
		for _, m := range unread {
			if m.User == SelfID {
				t.Errorf("%s: the viewer's own %s is unread", c.ID, m.TS)
			}
			if c.IsIM || strings.Contains(m.Text, "<@"+SelfID+">") || strings.Contains(m.Text, "<!channel>") || strings.Contains(m.Text, "<!here>") {
				mentions++
			}
		}
		if u.Mentions > mentions {
			t.Errorf("%s: %d mentions, only %d unread messages mention the viewer", c.ID, u.Mentions, mentions)
		}
	}
	if ws.Unread[GeneralID].Mentions == 0 || len(ws.History[GeneralID]) == 0 {
		t.Error("#general should start with an unread mention")
	}
	if len(ws.Sections) < 4 {
		t.Errorf("%d sections", len(ws.Sections))
	}
	secIDs := map[string]bool{}
	for _, s := range ws.Sections {
		if secIDs[s.ID] || !strings.HasPrefix(s.ID, "L0DEMO") {
			t.Errorf("section id %q", s.ID)
		}
		secIDs[s.ID] = true
		for _, ch := range s.Channels {
			if _, ok := convs[ch]; !ok {
				t.Errorf("section %s: unknown %s", s.ID, ch)
			}
		}
	}

	// files: canvases and lists shared are in the workspace
	canv, lst := map[string]bool{}, map[string]bool{}
	for _, c := range ws.Canvases {
		canv[c.ID] = len(c.Doc.Blocks) > 0
	}
	for _, l := range ws.Lists {
		lst[l.ID] = len(l.Records) > 0
	}
	for _, id := range []string{PartyCanvasID, PlanCanvasID} {
		if !canv[id] {
			t.Errorf("canvas %s missing or empty", id)
		}
	}
	if !lst[RestockListID] {
		t.Errorf("list %s missing or empty", RestockListID)
	}
	eachMsg(ws, func(ch string, m slack.Message) {
		for _, f := range m.Files {
			switch f.Mode {
			case "quip":
				if !canv[f.ID] || f.Title == "" {
					t.Errorf("%s: shared canvas %s unknown or untitled", ch, f.ID)
				}
			case "list":
				if !lst[f.ID] || f.Title == "" {
					t.Errorf("%s: shared list %s unknown or untitled", ch, f.ID)
				}
			}
		}
	})
	if len(ws.Scheduled) == 0 || !ws.Scheduled[0].At.After(now) || !json.Valid(ws.Scheduled[0].Blocks) {
		t.Errorf("scheduled: %+v", ws.Scheduled)
	}
	if len(ws.Bots) != 2 {
		t.Errorf("%d bots", len(ws.Bots))
	}
}

func eachMsg(ws *Workspace, f func(ch string, m slack.Message)) {
	for ch, h := range ws.History {
		for _, m := range h {
			f(ch, m)
		}
	}
	for ch, th := range ws.Replies {
		for _, rs := range th {
			for _, m := range rs {
				f(ch, m)
			}
		}
	}
}

// imageURLs is every image URL the workspace hands out.
func imageURLs(ws *Workspace) []string {
	set := map[string]bool{}
	add := func(ss ...string) {
		for _, s := range ss {
			if s != "" {
				set[s] = true
			}
		}
	}
	for _, u := range ws.Users {
		p := u.Profile
		add(p.Image24, p.Image32, p.Image48, p.Image72, p.Image192, p.Image512, p.ImageOriginal)
	}
	for _, b := range ws.Bots {
		for _, u := range b.Icons {
			add(u)
		}
	}
	for _, u := range ws.Emoji {
		if !strings.HasPrefix(u, "alias:") {
			add(u)
		}
	}
	eachMsg(ws, func(_ string, m slack.Message) {
		if bp := m.BotProfile; bp != nil {
			add(bp.Icons.Image36, bp.Icons.Image48, bp.Icons.Image72)
		}
		for _, f := range m.Files {
			if f.Mimetype == "image/png" {
				add(f.URLPrivate, f.Thumb360, f.Thumb480, f.Thumb720, f.Thumb1024)
			}
			add(f.ThumbPDF)
		}
	})
	for _, px := range []string{"34", "44", "68", "88", "102", "132", "230"} {
		add(testBase + "img/" + imgTeam + px + ".png")
	}
	var out []string
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

func TestBuildImages(t *testing.T) {
	ws, err := Build(testBase, buildNows()[0])
	if err != nil {
		t.Fatal(err)
	}
	urls := imageURLs(ws)
	var mu sync.Mutex
	var bad []string
	work := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range work {
				if !strings.HasPrefix(u, testBase+"img/") {
					mu.Lock()
					bad = append(bad, u+" (not on the fake)")
					mu.Unlock()
					continue
				}
				b, typ, ok := Image(strings.TrimPrefix(u, testBase+"img/"))
				if !ok || typ != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
					mu.Lock()
					bad = append(bad, u)
					mu.Unlock()
				}
			}
		}()
	}
	for _, u := range urls {
		work <- u
	}
	close(work)
	wg.Wait()
	for _, u := range bad {
		t.Errorf("not served: %s", u)
	}
	t.Logf("%d image URLs", len(urls))
}

// TestBuildEmoji checks every emoji the workspace uses is either custom or
// a standard one StandardEmoji draws.
func TestBuildEmoji(t *testing.T) {
	ws, err := Build(testBase, buildNows()[0])
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	scan := func(s string) {
		for _, m := range emojiRE.FindAllStringSubmatch(s, -1) {
			used[m[1]] = true
		}
	}
	eachMsg(ws, func(_ string, m slack.Message) {
		for _, r := range m.Reactions {
			used[r.Name] = true
		}
		scan(m.Text)
		var blocks any
		_ = json.Unmarshal(m.Blocks, &blocks)
		walkEmoji(blocks, used)
	})
	for _, p := range ws.People {
		scan(p.Status.Emoji)
		for _, s := range p.Statuses {
			scan(s.Emoji)
		}
	}
	for _, s := range ws.Sections {
		scan(s.Emoji)
	}
	drawn := map[string]bool{}
	for _, n := range standardDrawn {
		drawn[n] = true
	}
	var plain []string
	for name := range used {
		if strings.HasPrefix(name, "skin-tone-") {
			continue
		}
		if _, ok := ws.Emoji[name]; ok {
			continue
		}
		u, ok := emoji.StandardURL(name, "", "")
		if !ok {
			t.Errorf(":%s: is neither custom nor standard", name)
			continue
		}
		b, ok := StandardEmoji(u)
		if !ok || !bytes.HasPrefix(b, []byte("\x89PNG")) {
			t.Errorf(":%s: (%s) not drawn", name, u)
		}
		if _, _, ok := emoji.Standard(name); ok && !drawn[name] {
			plain = append(plain, name)
		}
	}
	sort.Strings(plain)
	if len(plain) > 0 {
		t.Logf("standard emoji drawn as the fallback face: %v", plain)
	}
	// a skin tone and an unknown codepoint still draw
	for _, raw := range []string{emoji.CDN + "apple-medium/1f44d-1f3fd.png", emoji.CDN + "apple-medium/1fae8.png"} {
		if b, ok := StandardEmoji(raw); !ok || len(b) == 0 {
			t.Errorf("StandardEmoji(%s) failed", raw)
		}
	}
}

func walkEmoji(v any, used map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if x["type"] == "emoji" {
			if n, ok := x["name"].(string); ok {
				used[n] = true
			}
		}
		for _, c := range x {
			walkEmoji(c, used)
		}
	case []any:
		for _, c := range x {
			walkEmoji(c, used)
		}
	case string: // mrkdwn in sections and context blocks
		for _, m := range emojiRE.FindAllStringSubmatch(x, -1) {
			used[m[1]] = true
		}
	}
}

// TestBuildOriginal keeps the demo its own: none of a certain sitcom paper
// company's names appear in the data.
func TestBuildOriginal(t *testing.T) {
	deny := []string{"dunder", "mifflin", "scranton", "schrute", "michael scott", "dwight", "jim halpert", "halpert",
		"pam beesly", "beesly", "sabre", "stamford", "kevin malone", "angela martin", "creed", "toby flenderson",
		"ryan howard", "andy bernard", "stanley hudson", "phyllis", "kelly kapoor", "oscar martinez", "meredith palmer",
		"darryl", "jan levinson", "bears, beets", "beet farm", "that's what she said", "dundie"}
	// every data file, the bots' scripts included
	data := os.DirFS("data")
	err := fs.WalkDir(data, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := fs.ReadFile(data, p)
		low := strings.ToLower(string(b))
		for _, w := range deny {
			if strings.Contains(low, w) {
				t.Errorf("%s mentions %q", p, w)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
