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
		{"@a b", -1, false, 0, "", 0},
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

func TestRankUsers(t *testing.T) {
	users := []core.User{
		{ID: "U1", Name: "karim", RealName: "Karim Ahmed", Handle: "karim"},
		{ID: "U2", Name: "Akari", RealName: "Akari Sato", Handle: "akari"},
		{ID: "U3", Name: "kara", RealName: "Kara Lee", Handle: "kl"},
		{ID: "U4", Name: "Bob", RealName: "Robert Kar", Handle: "karbob"},
		{ID: "U5", Name: "karbot", Handle: "karbot", IsBot: true},
		{ID: "U6", Name: "kardead", Deleted: true},
		{ID: "U7", Name: "Zed", RealName: "Kar Zed", Handle: "zed"},
		{ID: "U8", Name: "karl", RealName: "Karl M", Handle: "karl"},
	}
	boost := map[string]bool{"U8": true}
	got := acNames(rankUsers("kar", users, boost, nil, "", nil, 50))
	// display-name prefix: people (boosted first, then shorter, then a-z),
	// then bots; handle prefix; real-name word prefix; substring.
	want := []string{"karl", "kara", "karim", "karbot", "Bob", "Zed", "Akari"}
	if !acEq(got, want) {
		t.Errorf("rankUsers(kar) = %v, want %v", got, want)
	}

	got = acNames(rankUsers("karim", users, nil, nil, "", nil, 50))
	if len(got) == 0 || got[0] != "karim" {
		t.Errorf("exact match not first: %v", got)
	}

	got = acNames(rankUsers("", users, map[string]bool{"U2": true, "U3": true}, nil, "U1", nil, 50))
	if !acEq(got, []string{"Akari", "kara", "karim"}) {
		t.Errorf("empty query = %v", got)
	}

	members := map[string]core.User{"U2": users[1]}
	ranked := rankUsers("kar", users, nil, members, "U1", nil, 50)
	if got := acNames(ranked); got[0] != "Akari" {
		t.Errorf("channel members first: %v", got)
	}
	for _, it := range ranked {
		wantNotIn := it.ID != "U2" && it.ID != "U1"
		if it.NotIn != wantNotIn || it.Self != (it.ID == "U1") {
			t.Errorf("%s: NotIn=%v Self=%v", it.Name, it.NotIn, it.Self)
		}
	}

	it := rankUsers("akari", users, nil, nil, "", nil, 1)[0]
	if it.Atom == nil || it.Atom.Kind != AtomUser || it.Atom.ID != "U2" || it.Atom.Text != "@Akari" || it.Secondary != "Akari Sato" {
		t.Errorf("user item %+v atom %+v", it, it.Atom)
	}
}

func TestRankUsergroupsAndBroadcasts(t *testing.T) {
	groups := []core.Usergroup{{ID: "S1", Handle: "design", Name: "Design Team"}, {ID: "S2", Handle: "eng", Name: "Engineering"}}
	users := []core.User{{ID: "U1", Name: "dee", Handle: "dee"}}
	got := rankUsers("de", users, nil, nil, "", groups, 50)
	if !acEq(acNames(got), []string{"dee", "Design Team"}) {
		t.Fatalf("got %v", acNames(got))
	}
	if !got[1].IsGroup() || got[1].Atom.Text != "@design" || got[1].Atom.ID != "S1" || got[1].Secondary != "design" {
		t.Errorf("group atom %+v", got[1].Atom)
	}
	if got := rankUsers("", nil, nil, nil, "", groups, 50); len(got) != 0 {
		t.Errorf("empty query lists usergroups: %v", acNames(got))
	}

	b := rankBroadcasts("", "channel", false)
	if !acEq(acNames(b), []string{"here", "channel"}) {
		t.Errorf("broadcasts = %v", acNames(b))
	}
	b = rankBroadcasts("e", "channel", true)
	if !acEq(acNames(b), []string{"everyone"}) || !b[0].IsBroadcast() || b[0].Atom.Text != "@everyone" {
		t.Errorf("broadcasts(e) = %v", acNames(b))
	}
	if len(rankBroadcasts("", "im", true)) != 0 {
		t.Error("broadcasts in an IM")
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
	got := rankChannels("gen", convs, hits, 50)
	want := []string{"gen", "general", "genetics", "proj-gen", "oxygen", "old-gen"}
	if !acEq(acNames(got), want) {
		t.Errorf("rankChannels(gen) = %v, want %v", acNames(got), want)
	}
	if !got[3].Private || got[3].Atom.Text != "#proj-gen" || got[3].Atom.Kind != AtomChannel {
		t.Errorf("private item %+v", got[3])
	}
	if got[2].Member {
		t.Error("search hit marked member")
	}
	if !got[5].Archived {
		t.Error("archived channel not flagged")
	}
	got = rankChannels("", convs, hits, 50)
	if !acEq(acNames(got), []string{"general", "gen", "oxygen", "proj-gen"}) {
		t.Errorf("empty = %v", acNames(got))
	}
}

func TestRankCommands(t *testing.T) {
	cmds := []core.Command{{Name: "/remind", Desc: "Set a reminder"}, {Name: "/rem"}, {Name: "/giphy"}, {Name: "/feedremind"}}
	got := rankCommands("rem", cmds, 50)
	if !acEq(acNames(got), []string{"rem", "remind", "feedremind"}) {
		t.Errorf("got %v", acNames(got))
	}
	if got[1].Command == nil || got[1].Command.Desc != "Set a reminder" || got[1].Secondary != "Set a reminder" {
		t.Errorf("command item %+v", got[1])
	}
	if len(rankCommands("", cmds, 50)) != 4 {
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
