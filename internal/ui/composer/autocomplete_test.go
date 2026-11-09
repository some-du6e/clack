package composer

import (
	"testing"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

func acDocOf(s string) *Doc {
	d := NewDoc()
	d.Insert(0, s, Attr{})
	return d
}

func TestDetectTrigger(t *testing.T) {
	cases := []struct {
		text  string
		caret int // -1: end
		ok    bool
		kind  acKind
		query string
		start int
	}{
		{"@", -1, true, acUser, "", 0},
		{"hi @kar", -1, true, acUser, "kar", 3},
		{"hi @kar", 5, true, acUser, "k", 3},
		{"mail a@b", -1, false, 0, "", 0},
		{"#gen", -1, true, acChannel, "gen", 0},
		{"see #proj-x", -1, true, acChannel, "proj-x", 4},
		{":s", -1, false, 0, "", 0},
		{":sm", -1, true, acEmoji, "sm", 0},
		{"ok :+1", -1, true, acEmoji, "+1", 3},
		{":smile:", -1, false, 0, "", 0},
		{"10:30", -1, false, 0, "", 0},
		{":-)", -1, false, 0, "", 0},
		{"/", -1, true, acCommand, "", 0},
		{"/rem", -1, true, acCommand, "rem", 0},
		{"x /rem", -1, false, 0, "", 0},
		{"/remind me", -1, false, 0, "", 0},
		{"line\n@a", -1, true, acUser, "a", 5},
		{"@a b", -1, true, acUser, "a b", 0}, // mentions may contain spaces ("Karim Hacker")
		{"@kar hac", -1, true, acUser, "kar hac", 0},
		{"@a  b", -1, false, 0, "", 0}, // double space ends the mention
		{"word @a b", -1, true, acUser, "a b", 5},
		{"@kar,", -1, false, 0, "", 0},
	}
	for _, c := range cases {
		d := acDocOf(c.text)
		caret := c.caret
		if caret < 0 {
			caret = d.Len()
		}
		got, ok := detectTrigger(d, caret)
		if ok != c.ok {
			t.Errorf("%q@%d: ok=%v want %v (%+v)", c.text, caret, ok, c.ok, got)
			continue
		}
		if !ok {
			continue
		}
		if got.Kind != c.kind || got.Query != c.query || got.Start != c.start || got.End != caret {
			t.Errorf("%q@%d: got %+v, want kind %d query %q start %d", c.text, caret, got, c.kind, c.query, c.start)
		}
	}
}

func TestDetectTriggerSkipsCodeAndAtoms(t *testing.T) {
	d := acDocOf("@kar")
	d.SetStyle(0, 4, Code, true)
	if _, ok := detectTrigger(d, 4); ok {
		t.Error("trigger inside inline code")
	}
	d = acDocOf("@kar")
	d.SetLineKind(0, 0, CodeBlock)
	if _, ok := detectTrigger(d, 4); ok {
		t.Error("trigger inside code block")
	}
	d = NewDoc()
	d.InsertAtom(0, &Atom{Kind: AtomUser, ID: "U1", Text: "@a"})
	if _, ok := detectTrigger(d, 1); ok {
		t.Error("trigger on an atom")
	}
	d.Insert(1, "#ge", Attr{})
	if tr, ok := detectTrigger(d, d.Len()); !ok || tr.Start != 1 || tr.Query != "ge" {
		t.Errorf("after atom: %+v %v", tr, ok)
	}
}

func TestAutocompleteKeysAndDismiss(t *testing.T) {
	ac := newAutocomplete(nil, "C1")
	d := acDocOf("/x")
	ac.Update(d, d.Len())
	if ac.Open() {
		t.Fatal("open without items")
	}
	// inject rows directly: no core in tests
	ac.commands = []core.Command{{Name: "/xa"}, {Name: "/xb"}}
	ac.gen++
	ac.Update(d, d.Len())
	if !ac.Open() || len(ac.items) != 2 {
		t.Fatalf("want 2 rows, got %d", len(ac.items))
	}
	if !ac.pseudo || ac.sel != 0 {
		t.Fatalf("opens pseudo-selected on row 0 (pseudo=%v sel=%d)", ac.pseudo, ac.sel)
	}
	if !ac.Key(key.NameDownArrow, 0) || ac.pseudo || ac.sel != 0 {
		t.Errorf("first down only selects row 0 for real (pseudo=%v sel=%d)", ac.pseudo, ac.sel)
	}
	if !ac.Key(key.NameUpArrow, 0) || ac.sel != 1 {
		t.Errorf("up should wrap to 1, sel=%d", ac.sel)
	}
	if !ac.Key(key.NameDownArrow, 0) || ac.sel != 0 {
		t.Errorf("down should wrap to 0, sel=%d", ac.sel)
	}
	ac.hover(1)
	if ac.sel != 1 || ac.pseudo {
		t.Errorf("hover selects: sel=%d", ac.sel)
	}
	if ac.Key("A", 0) {
		t.Error("letters must reach the editor")
	}
	if !ac.Key(key.NameEscape, 0) || ac.Open() {
		t.Fatal("escape should close")
	}
	ac.Update(d, d.Len())
	if ac.Open() {
		t.Error("reopened on the same token after escape")
	}
	if ac.Key(key.NameReturn, 0) {
		t.Error("closed popup must not eat Enter")
	}
	d.Insert(d.Len(), "a", Attr{})
	ac.Update(d, d.Len())
	if !ac.Open() {
		t.Fatal("token changed: should reopen")
	}
	if !ac.Key(key.NameTab, 0) {
		t.Fatal("tab should accept")
	}
	it, s, e, ok := ac.Accepted()
	if !ok || it.Name != "xa" || s != 0 || e != 3 {
		t.Errorf("accepted %+v [%d,%d) %v", it, s, e, ok)
	}
	if _, _, _, ok := ac.Accepted(); ok {
		t.Error("Accepted is one-shot")
	}
	ac.Update(d, d.Len())
	if ac.Open() {
		t.Error("popup reopened before the composer replaced the token")
	}
}

func acNames(items []acItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func acEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ranked runs the @ ranking on fixed sources (no core: the bundled
// model, no frecency).
func ranked(q string, users []core.User, members map[string]core.User, self string, groups []core.Usergroup, kws []string, kind string) []acItem {
	src := memberSources{users: users, members: members, selfID: self, groups: groups, keywords: kws, rk: rankCtx{convKind: kind}}
	return src.rank(q, false)
}

// Slack's @ rules (docs/AUTOCOMPLETE-RULES.md): real and display names
// match at word starts (the username doesn't), members of the
// conversation first, prefix before a word jump, bots and deactivated
// people down, usergroups and keywords mixed in by the model.
func TestRankUsers(t *testing.T) {
	users := []core.User{
		{ID: "U1", Name: "karim", DisplayName: "karim", RealName: "Karim Ahmed", Handle: "karim", DisplaySet: true},
		{ID: "U2", Name: "Akari", DisplayName: "Akari", RealName: "Akari Sato", Handle: "akari", DisplaySet: true},
		{ID: "U3", Name: "kara", DisplayName: "kara", RealName: "Kara Lee", Handle: "kl", DisplaySet: true},
		{ID: "U4", Name: "Bob", DisplayName: "Bob", RealName: "Robert Kar", Handle: "karbob", DisplaySet: true},
		{ID: "U5", Name: "karbot", RealName: "karbot", Handle: "karbot", IsBot: true},
		{ID: "U6", Name: "kardead", RealName: "kardead", Deleted: true},
		{ID: "U7", Name: "Zed", DisplayName: "Zed", RealName: "Kar Zed", Handle: "zed", DisplaySet: true},
		{ID: "U8", Name: "karl", DisplayName: "karl", RealName: "Karl M", Handle: "karl", DisplaySet: true},
		{ID: "U9", Name: "Nobody", DisplayName: "Nobody", RealName: "No One", Handle: "karnobody", DisplaySet: true},
	}
	got := acNames(ranked("kar", users, nil, "", nil, nil, "channel"))
	if idx := acIndex(got, "Nobody"); idx >= 0 {
		t.Errorf("the username matched: %v", got)
	}
	if acIndex(got, "Akari") >= 0 {
		t.Errorf("'kar' inside a word matched: %v", got)
	}
	for _, n := range []string{"karim", "kara", "karl"} {
		if acIndex(got, n) > acIndex(got, "Bob") || acIndex(got, n) > acIndex(got, "karbot") || acIndex(got, n) > acIndex(got, "kardead") {
			t.Errorf("active prefix matches first: %v", got)
		}
	}
	if acIndex(got, "Bob") < 0 || acIndex(got, "Zed") < 0 {
		t.Errorf("real-name word matches missing: %v", got)
	}

	members := map[string]core.User{"U8": users[7], "U4": users[3]}
	rs := ranked("kar", users, members, "U1", nil, nil, "channel")
	got = acNames(rs)
	// members of the conversation first (MEMBER_OF_CURRENT_CHANNEL);
	// the viewer is one too, but CURRENT_USER_TEXTY puts them after the
	// other member with an equal match
	if got[0] != "karl" || got[1] != "karim" || acIndex(got, "Bob") > acIndex(got, "kara") {
		t.Errorf("conversation members first: %v", got)
	}
	for _, it := range rs {
		wantNotIn := it.ID != "U8" && it.ID != "U4" && it.ID != "U1"
		if it.NotIn != wantNotIn || it.Self != (it.ID == "U1") {
			t.Errorf("%s: NotIn=%v Self=%v", it.Name, it.NotIn, it.Self)
		}
	}

	it := ranked("akari", users, nil, "", nil, nil, "channel")[0]
	if it.Atom == nil || it.Atom.Kind != AtomUser || it.Atom.ID != "U2" || it.Atom.Text != "@Akari" || it.Secondary != "Akari Sato" {
		t.Errorf("user item %+v atom %+v", it, it.Atom)
	}
	// an empty @ lists everyone the client knows
	if got := ranked("", users, nil, "", nil, nil, "channel"); len(got) != 9 {
		t.Errorf("empty query: %v (everyone; deactivated bots aside)", acNames(got))
	}
	dead := append(users, core.User{ID: "B9", Name: "oldbot", RealName: "oldbot", IsBot: true, Deleted: true})
	if got := acNames(ranked("old", dead, nil, "", nil, nil, "channel")); len(got) != 0 {
		t.Errorf("deactivated bot listed: %v", got)
	}
}

func acIndex(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func TestRankUsergroupsAndBroadcasts(t *testing.T) {
	groups := []core.Usergroup{{ID: "S1", Handle: "design", Name: "Design Team"}, {ID: "S2", Handle: "eng", Name: "Engineering"}}
	users := []core.User{{ID: "U1", Name: "dee", DisplayName: "dee", Handle: "dee", DisplaySet: true}}
	got := ranked("de", users, nil, "", groups, nil, "channel")
	if acIndex(acNames(got), "dee") < 0 || acIndex(acNames(got), "Design Team") < 0 || len(got) != 2 {
		t.Fatalf("got %v", acNames(got))
	}
	for _, g := range got {
		if g.IsGroup() && (g.Atom.Text != "@design" || g.Atom.ID != "S1" || g.Secondary != "design") {
			t.Errorf("group atom %+v", g.Atom)
		}
	}
	// usergroups match by name words too
	if got := acNames(ranked("team", nil, nil, "", groups, nil, "channel")); !acEq(got, []string{"Design Team"}) {
		t.Errorf("by name: %v", got)
	}

	b := rankBroadcasts("", "channel", false)
	if !acEq(acNames(b), []string{"here", "channel"}) && !acEq(acNames(b), []string{"channel", "here"}) {
		t.Errorf("broadcasts = %v", acNames(b))
	}
	b = rankBroadcasts("e", "channel", true)
	if !acEq(acNames(b), []string{"everyone"}) || !b[0].IsBroadcast() || b[0].Atom.Text != "@everyone" {
		t.Errorf("broadcasts(e) = %v", acNames(b))
	}
	if len(rankBroadcasts("", "im", true)) != 0 {
		t.Error("broadcasts in an IM")
	}
	// @channel also answers to "group"
	if got := acNames(rankBroadcasts("gro", "channel", false)); !acEq(got, []string{"channel"}) {
		t.Errorf("group: %v", got)
	}
}

func TestRankChannels(t *testing.T) {
	convs := []core.Conv{
		{ID: "C1", Name: "general", Kind: "channel", Priority: 1},
		{ID: "C2", Name: "proj-gen", Kind: "private"},
		{ID: "C3", Name: "gen", Kind: "channel"},
		{ID: "C4", Name: "old-gen", Kind: "channel", IsArchived: true},
		{ID: "D1", Name: "gen-dm", Kind: "im"},
		{ID: "C5", Name: "oxygen", Kind: "channel"},
	}
	hits := []core.ChannelHit{
		{ID: "C6", Name: "genetics", Kind: "channel"},
		{ID: "C1", Name: "general", Kind: "channel", IsMember: true},
	}
	src := channelSources{convs: convs, hits: hits}
	got := src.rank("gen")
	names := acNames(got)
	if names[0] != "gen" {
		t.Errorf("exact name first: %v", names)
	}
	if acIndex(names, "oxygen") >= 0 {
		t.Errorf("'gen' inside a word matched: %v", names)
	}
	if acIndex(names, "general") > acIndex(names, "genetics") {
		t.Errorf("member channel before a non-member at equal match: %v", names)
	}
	if acIndex(names, "proj-gen") < acIndex(names, "general") {
		t.Errorf("word match after prefix matches: %v", names)
	}
	for _, it := range got {
		switch it.Name {
		case "proj-gen":
			if !it.Private || it.Atom.Text != "#proj-gen" || it.Atom.Kind != AtomChannel {
				t.Errorf("private item %+v", it)
			}
		case "genetics":
			if it.Member {
				t.Error("search hit marked member")
			}
		case "old-gen":
			if !it.Archived {
				t.Error("archived channel not flagged")
			}
		}
	}
	if got := acNames(src.rank("")); len(got) != 6 {
		t.Errorf("empty = %v (every channel, archived too)", got)
	}
}

func TestRankCommands(t *testing.T) {
	cmds := []core.Command{{Name: "/remind", Desc: "Set a reminder"}, {Name: "/rem"}, {Name: "/giphy"}, {Name: "/feed", Desc: "Manage RSS feeds"}}
	got := rankCommands("rem", cmds, rankCtx{})
	names := acNames(got)
	if names[0] != "rem" || acIndex(names, "remind") != 1 {
		t.Errorf("exact command, then prefix: %v", names)
	}
	if got[1].Command == nil || got[1].Command.Desc != "Set a reminder" || got[1].Secondary != "Set a reminder" {
		t.Errorf("command item %+v", got[1])
	}
	if got := acNames(rankCommands("rss", cmds, rankCtx{})); !acEq(got, []string{"feed"}) {
		t.Errorf("descriptions match: %v", got)
	}
	if len(rankCommands("", cmds, rankCtx{})) != 4 {
		t.Error("bare / should list every command")
	}
}

const testStdJSON = `{"emoji":[
{"n":["smile"],"i":"1f604","u":"1F604"},
{"n":["smiley"],"i":"1f603","u":"1F603"},
{"n":["sweat_smile"],"i":"1f605","u":"1F605"},
{"n":["+1","thumbsup"],"i":"1f44d","u":"1F44D"}
]}`

func TestEmojiIndex(t *testing.T) {
	custom := map[string]string{
		"smiling-parrot": "https://emoji.example/parrot.gif",
		"sm":             "https://emoji.example/sm.png",
		"grinsmile":      "https://emoji.example/g.png",
		"happy":          "alias:smiling-parrot",
		"yay":            "alias:smile",
		"broken":         "alias:nope",
	}
	x := buildEmojiIndex([]byte(testStdJSON), custom)
	got := acNames(x.search("sm", 50))
	// exact, then prefix by length, then word prefix, then substring
	want := []string{"sm", "smile", "smiley", "smiling-parrot", "sweat_smile", "grinsmile"}
	if !acEq(got, want) {
		t.Errorf("search(sm) = %v, want %v", got, want)
	}
	if got := acNames(x.search("sm", 2)); !acEq(got, []string{"sm", "smile"}) {
		t.Errorf("limit: %v", got)
	}

	happy := x.search("happy", 5)
	if len(happy) != 1 || happy[0].Atom.URL != "https://emoji.example/parrot.gif" || happy[0].Atom.Text != ":happy:" {
		t.Errorf("custom alias: %+v", happy)
	}
	yay := x.search("yay", 5)
	if len(yay) != 1 || yay[0].Uni == "" || yay[0].Atom.Kind != AtomEmoji || yay[0].Atom.ID != "yay" {
		t.Errorf("alias to standard: %+v", yay)
	}
	if len(x.search("broken", 5)) != 0 {
		t.Error("dangling alias indexed")
	}
	thumbs := x.search("+1", 5)
	if len(thumbs) != 1 || thumbs[0].Atom.Uni != "\U0001F44D" {
		t.Errorf("+1: %+v", thumbs)
	}
}

func TestInsertShortest(t *testing.T) {
	ns := []string{"aaaa", "a", "aaa", "aa", "ab", "b"}
	var s []int
	for i := range ns {
		s = insertShortest(s, i, ns, 3)
	}
	got := make([]string, len(s))
	for i, j := range s {
		got[i] = ns[j]
	}
	if !acEq(got, []string{"a", "b", "aa"}) {
		t.Errorf("got %v", got)
	}
}
