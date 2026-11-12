package messages

// Skin tones in the reaction bar (docs/MESSAGES-AUDIT.md "Skin tones"),
// against Slack's CDN build 4d0c78085232: getReactionGroups (hzIJ) puts
// every tone of an emoji in one pill, Reaction (9FoR) draws the group,
// ReactionTip (Qvn6) its tooltip, and a click is handleReactionClick
// (aHRx) → changeReactionFromUserAction (QOQU) with the group's name.
// Synthetic only: internal/fakeslack, nothing talks to Slack.

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tonedReactions: +1 in three tones (the viewer's the medium one) around
// a tada, as reactions.get lists them.
func tonedReactions() []core.Reaction {
	return []core.Reaction{
		{Name: "+1", Count: 2, Users: []string{"U1", "U2"}},
		{Name: "tada", Count: 1, Users: []string{"U3"}},
		{Name: "+1::skin-tone-4", Count: 2, Users: []string{"U2", "USELF"}, Self: true},
		{Name: "+1::skin-tone-6", Count: 1, Users: []string{"U4"}},
	}
}

func TestReactionGroupsLikeSlack(t *testing.T) {
	rs := tonedReactions()
	gs := groupReactions(nil, rs)
	// getReactionGroups: groups in the order their baseName first shows,
	// counts summed, users uniq'd over the group's reactions
	type got struct {
		name  string
		count int
		idx   []int
	}
	var out []got
	for _, g := range gs {
		out = append(out, got{g.name, g.count, append([]int(nil), g.idx...)})
	}
	want := []got{{"+1", 5, []int{0, 2, 3}}, {"tada", 1, []int{1}}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("groups %+v, want %+v", out, want)
	}
	if u := groupUsers(rs, &gs[0]); !reflect.DeepEqual(u, []string{"U1", "U2", "USELF", "U4"}) {
		t.Errorf("+1's users %v", u)
	}
	// the bar regroups every frame: no allocation once the buffers fit
	if n := testing.AllocsPerRun(20, func() { gs = groupReactions(gs, rs) }); n != 0 {
		t.Errorf("groupReactions allocates %.0f times a frame", n)
	}

	// changeReactionFromUserAction with actionSource reaction_toggle_button,
	// for the viewer (core's Self is theirs); others' reactions as the
	// viewer's when they are U2
	others := tonedReactions()
	others[2].Self = false
	for _, c := range []struct {
		why, base, self, tone string
		rs                    []core.Reaction
		add                   string
		remove                []string
	}{
		{"the viewer's tone comes off", "+1", "USELF", "3", rs, "", []string{"+1::skin-tone-4"}},
		{"every tone the viewer reacted with comes off", "+1", "U2", "", others, "", []string{"+1", "+1::skin-tone-4"}},
		{"a new reaction takes the preferred tone", "+1", "U9", "3", others, "+1::skin-tone-3", nil},
		{"no preferred tone: the plain emoji", "+1", "U9", "", others, "+1", nil},
		{"tone 1 is no tone", "+1", "U9", "1", others, "+1", nil},
		{"an emoji without skin tones stays plain", "tada", "U9", "5", others, "tada", nil},
		{"a custom emoji stays plain", "partyparrot", "U9", "5", others, "partyparrot", nil},
	} {
		add, remove := toggleTargets(c.rs, c.base, c.self, c.tone)
		if add != c.add || !reflect.DeepEqual(remove, c.remove) {
			t.Errorf("%s: add %q, remove %v; Slack adds %q, removes %v", c.why, add, remove, c.add, c.remove)
		}
	}
}

// The pill is the group: its emoji, the summed count, reacted with any
// tone, and Reaction's props are the group's.
func TestReactionGroupPill(t *testing.T) {
	e := entry{rev: 1, m: core.Message{TS: "1700000000.000100", Channel: "C1", User: "U1", Reactions: tonedReactions()}}
	r := newRow(&e)
	r.rc = &rowCtx{h: nilHost{}, selfID: "USELF"}
	if len(r.groups) != 2 || len(r.pills) < 2 {
		t.Fatalf("%d groups, %d pills; want 2", len(r.groups), len(r.pills))
	}
	if !r.pills[0].reacted() || r.pills[1].reacted() {
		t.Error("the +1 pill is the viewer's (they reacted with one of its tones), the tada pill isn't")
	}
	bp := r.pills[0].buttonProps()
	if bp.StrOr("className", "") != "c-button-unstyled c-reaction c-reaction--reacted" || bp.StrOr("aria-label", "") != "5 reactions, react with +1 emoji" {
		t.Errorf("button props %v", bp)
	}
	kids := bp.List("children")
	if len(kids) != 4 {
		t.Fatalf("%d children, want the three tones' emoji and the count", len(kids))
	}
	var emoji []string
	for _, k := range kids[:3] {
		if el, ok := k.(slackc.Slot); ok {
			emoji = append(emoji, el.Props.StrOr("emoji", ""))
		}
	}
	if !reflect.DeepEqual(emoji, []string{"+1", "+1::skin-tone-4", "+1::skin-tone-6"}) {
		t.Errorf("emoji children %v", emoji)
	}
	p := r.pills[0].props()
	if p.StrOr("name", "") != "+1" || p.IntOr("count", 0) != 5 || len(p.List("users")) != 4 || len(p.List("reactions")) != 3 {
		t.Errorf("Reaction props %v", p)
	}

	// past ten tones of one group Reaction draws ten and "…"
	var many []core.Reaction
	for i := 0; i < 12; i++ {
		many = append(many, core.Reaction{Name: "wave::skin-tone-" + strconv.Itoa(2+i%5) + strings.Repeat("x", i), Count: 1, Users: []string{"U" + strconv.Itoa(i)}})
	}
	e2 := entry{rev: 1, m: core.Message{TS: "1700000000.000200", Channel: "C1", User: "U1", Reactions: many}}
	r2 := newRow(&e2)
	r2.rc = &rowCtx{h: nilHost{}, selfID: "USELF"}
	if kids := r2.pills[0].buttonProps().List("children"); len(kids) != 12 {
		t.Errorf("%d children for 12 tones, want ten emoji, the truncation and the count", len(kids))
	}
}

// ReactionTip over a group: one getTip line per tone; a multi-skin-tone
// emoji reads as one line; past 50 in a group of several tones each line
// names ten.
func TestReactionGroupTipLikeSlack(t *testing.T) {
	e := entry{rev: 1, m: core.Message{TS: "1700000000.000100", Channel: "C1", User: "U1", Reactions: tonedReactions()}}
	r := newRow(&e)
	r.rc = &rowCtx{h: nilHost{}, selfID: "USELF"}
	gt := r.groupTip(nilHost{}, 0, "USELF")
	var lines []string
	for _, l := range gt.lines {
		lines = append(lines, plainTip(l))
	}
	want := []string{"U1 and U2 reacted with :+1:", "U2 and you reacted with :+1::skin-tone-4:", "U4 reacted with :+1::skin-tone-6:"}
	if !reflect.DeepEqual(lines, want) || len(gt.urls) != 3 || gt.wide {
		t.Errorf("tip %q (%d emoji, wide %v), want %q, 3 emoji, small", lines, len(gt.urls), gt.wide, want)
	}

	hs := []core.Reaction{{Name: "handshake", Count: 1, Users: []string{"U1"}}, {Name: "handshake::skin-tone-3", Count: 1, Users: []string{"USELF"}, Self: true}}
	e2 := entry{rev: 1, m: core.Message{TS: "1700000000.000200", Channel: "C1", User: "U1", Reactions: hs}}
	r2 := newRow(&e2)
	r2.rc = &rowCtx{h: nilHost{}, selfID: "USELF"}
	if gt := r2.groupTip(nilHost{}, 0, "USELF"); len(gt.lines) != 1 || plainTip(gt.lines[0]) != "U1 and you reacted with :handshake:" || len(gt.urls) != 2 {
		t.Errorf("handshake (multiSkinTone): %q, %d emoji", gt.lines, len(gt.urls))
	}

	ids := func(n int, p string) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, p+strconv.Itoa(i))
		}
		return out
	}
	big := []core.Reaction{{Name: "+1", Count: 40, Users: ids(40, "A")}, {Name: "+1::skin-tone-2", Count: 20, Users: ids(20, "B")},
		{Name: "+1::skin-tone-3", Count: 2, Users: ids(2, "C")}, {Name: "+1::skin-tone-4", Count: 1, Users: ids(1, "D")}, {Name: "+1::skin-tone-5", Count: 1, Users: ids(1, "E")}}
	e3 := entry{rev: 1, m: core.Message{TS: "1700000000.000300", Channel: "C1", User: "U1", Reactions: big}}
	r3 := newRow(&e3)
	r3.rc = &rowCtx{h: nilHost{}, selfID: "USELF"}
	gt = r3.groupTip(nilHost{}, 0, "USELF")
	if !gt.wide {
		t.Error("64 reactions in five tones: Slack's large tooltip")
	}
	if got, want := plainTip(gt.lines[0]), strings.Join(ids(10, "A"), ", ")+", and others reacted with :+1:"; got != want {
		t.Errorf("first line %q, want %q", got, want)
	}
	if got, want := plainTip(gt.lines[2]), "C0 and C1 reacted with :+1::skin-tone-3:"; got != want {
		t.Errorf("third line %q, want %q", got, want)
	}
}

// A pill's click goes through changeReactionFromUserAction with the
// group's name: the viewer's tone comes off; on a group they haven't
// reacted to, the emoji goes on in their preferred_skin_tone.
func TestReactionGroupClickLikeSlack(t *testing.T) {
	app, fs, ts := shotsApp(t)
	var mu sync.Mutex
	var calls []string
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "reactions.add" || method == "reactions.remove" {
			mu.Lock()
			calls = append(calls, method+" "+f.Get("name"))
			mu.Unlock()
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	sent := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
	app.S.SetPrefs(store.Prefs{SkinTone: "3"}, nil)
	b1, ok := app.S.Find(tShots, ts["b1"])
	if !ok {
		t.Fatal("no b1")
	}
	b1.Reactions = []slack.Reaction{{Name: "+1", Count: 1, Users: []string{"UANA00001"}},
		{Name: "+1::skin-tone-4", Count: 1, Users: []string{fakeslack.SelfID}}, {Name: "wave", Count: 1, Users: []string{"UBEN00001"}}}
	app.S.Upsert(tShots, b1)
	host := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(host, tShots)
	v.Preview = true
	f := frameFn(v)
	until(t, 5*time.Second, "b1's pills", f, func() bool { r := v.s.rows[ts["b1"]]; return r != nil && len(r.groups) == 2 })
	r := v.s.rows[ts["b1"]]
	r.rc.onReactGroup(r, "+1")
	until(t, 5*time.Second, "the remove", f, func() bool { return len(sent()) == 1 })
	if got := sent(); got[0] != "reactions.remove +1::skin-tone-4" {
		t.Errorf("click on the viewer's +1 pill sent %v, want the medium tone removed", got)
	}
	r.rc.onReactGroup(r, "wave")
	until(t, 5*time.Second, "the add", f, func() bool { return len(sent()) == 2 })
	if got := sent(); got[1] != "reactions.add wave::skin-tone-3" {
		t.Errorf("click on wave sent %v, want it added in the preferred tone (3)", got)
	}
	until(t, 5*time.Second, "the optimistic update", f, func() bool {
		r := v.s.rows[ts["b1"]]
		return r != nil && len(r.groups) == 2 && !r.pills[0].reacted() && r.pills[1].reacted()
	})
}
