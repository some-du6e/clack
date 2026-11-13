package popups

// The code channel card (code_card.go) and the channel card's rich topic
// over core on the in-repo fake Slack (synthetic data only).

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// codeEnv is chcEnv with the code channel fixtures, and the message rows'
// rich style for the topic.
func codeEnv(t *testing.T, dark bool) (*tzHost, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader, sz.CodeChannels = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID, fakeslack.CodeAgentID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(dark)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	prev := RichStyle
	t.Cleanup(func() { RichStyle = prev })
	st := richview.NewStyle(sh, nil, richview.LightPalette())
	if dark {
		st.SetPalette(richview.DarkPalette())
	}
	st.Face = theme.Typeface
	RichStyle = func(Host) *richview.Style { return st }
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs
}

// Slack's classic CodeChannelEntityCard for each agent status: the tile's
// icon (getIconName), the status tag (label and style), "Code channel",
// the summary from the stored summary_message with its attribution, the
// facepile (the agent first, then the channel's first members, which the
// card fetches) and "started … at …"; no Join/View button, no body items.
// A click goes to the channel and closes the card.
func TestChannelHoverCardCodeChannel(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, _ := codeEnv(t, dark)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if _, err := h.a.History(ctx, fakeslack.CodeDoneID, ""); err != nil { // the summary is read from the store only
			t.Fatal(err)
		}
		cancel()
		anchor := image.Rect(400, 500, 480, 522)
		cases := []struct {
			name, id, icon, tag, style string
			summary                    bool
		}{
			{"done", fakeslack.CodeDoneID, "branch", "Done", "success", true},
			{"working", fakeslack.CodeWorkingID, "pr-open", "Working", "informative", false},
			{"waiting", fakeslack.CodeWaitingID, "code", "Needs attention", "warning", false},
			{"idle", fakeslack.CodeIdleID, "pr-open", "Ready", "neutral", false},
			{"preview", fakeslack.CodePreviewID, "code", "Working", "informative", false}, // public, not joined
		}
		for _, c := range cases {
			p := NewChannelHoverCard(h, anchor, c.id, true, false)
			var st CodeCardState
			ops := chcFrames(t, p, func() bool {
				var ok bool
				st, ok = p.CodeCard(time.Now())
				return p.State() == ChannelCardCode && ok && len(st.Faces) > 1 && (!c.summary || st.Summary != "")
			})
			if st.Icon != c.icon || st.Tag != c.tag || st.TagStyle != c.style {
				t.Errorf("%s: icon %q tag %q (%s), want %q %q (%s)", c.name, st.Icon, st.Tag, st.TagStyle, c.icon, c.tag, c.style)
			}
			if st.Subtitle != "Code channel" {
				t.Errorf("%s: subtitle %q", c.name, st.Subtitle)
			}
			if st.Faces[0] != fakeslack.CodeAgentID {
				t.Errorf("%s: faces %q, want the agent first", c.name, st.Faces)
			}
			if !strings.HasPrefix(st.Started, "started ") || !strings.Contains(st.Started, " at ") {
				t.Errorf("%s: started %q", c.name, st.Started)
			}
			if !strings.Contains(st.Tip, "Codebot") || !strings.HasPrefix(st.Tip, "Codebot") {
				t.Errorf("%s: facepile tip %q, want the agent named first", c.name, st.Tip)
			}
			if c.summary {
				want := "Upgraded every dependency to its latest minor 🎉 and the suite passes."
				if st.Summary != want {
					t.Errorf("%s: summary %q, want %q", c.name, st.Summary, want)
				}
				if !strings.Contains(st.Attribution, " at ") || !strings.HasPrefix(st.Attribution, "Codebot") {
					t.Errorf("%s: attribution %q", c.name, st.Attribution)
				}
			} else if st.Summary != "" {
				t.Errorf("%s: summary %q without summary_message", c.name, st.Summary)
			}
			if b := p.Button(); b != "" || p.Items() != nil {
				t.Errorf("%s: button %q items %q on the code card", c.name, b, p.Items())
			}
			r := p.Rect()
			if r.Dx() != 340 {
				t.Errorf("%s: width %d", c.name, r.Dx())
			}
			// header 8 + 41 + 8, footer 1 + 8 + 24 (or the started time's
			// wrapped lines) + 8; the summary adds its lines, the attribution
			// and their gaps
			min := 57 + 41
			if r.Dy() < min || c.summary && r.Dy() < min+22+4+19+6 {
				t.Errorf("%s: height %d, want at least %d (+ the summary)", c.name, r.Dy(), min)
			}
			chcShot(t, ops, "popups-code-card-"+c.name+"-"+mode)
		}
		// the faces fetch: edge users/list for the first 20 people
		if ms, ok := h.a.ChannelTopMembers(fakeslack.CodeDoneID); !ok || len(ms) == 0 {
			t.Errorf("no membershipOrdered for the done channel: %q %v", ms, ok)
		}
	}
}

// A click anywhere on the code card goes to the channel and closes it.
func TestChannelHoverCardCodeClick(t *testing.T) {
	h, _ := codeEnv(t, false)
	anchor := image.Rect(400, 500, 480, 522)
	p := NewChannelHoverCard(h, anchor, fakeslack.CodeIdleID, true, false)
	chcFrames(t, p, func() bool { return p.State() == ChannelCardCode })
	p.codeBtn.Click()
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
	p.Layout(gtx)
	h.mu.Lock()
	nav := append([]string(nil), h.navs...)
	h.mu.Unlock()
	if !p.Dismissed() || len(nav) != 1 || nav[0] != "c/"+fakeslack.CodeIdleID {
		t.Errorf("after the click: dismissed %v, navigated %q", p.Dismissed(), nav)
	}
}

// The topic as mrkdwn: an emoji, a member mention and a link, clamped to
// two lines with an ellipsis; its mentions open no card of their own.
func TestChannelHoverCardRichTopic(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, fs := codeEnv(t, dark)
		fs.SetTopic(fakeslack.ThreadElsewhereID, "Partner notes :tada: kept by <@"+fakeslack.ProfileID+"> in <https://example.com/notes|the shared doc>, "+
			"with every decision we made this quarter written down so nobody has to ask twice, and then some more words to wrap well past two lines")
		anchor := image.Rect(400, 500, 480, 522)
		p := NewChannelHoverCard(h, anchor, fakeslack.ThreadElsewhereID, true, false)
		ops := chcFrames(t, p, func() bool {
			return p.State() == ChannelCardFull && p.topic != nil && len(p.Items()) == 2 && p.Items()[1] != ""
		})
		var user, link bool
		for _, r := range p.topic.LinkRects() {
			user = user || r.Kind == richview.HitUser && r.ID == fakeslack.ProfileID
			link = link || r.Kind == richview.HitLink && r.URL == "https://example.com/notes"
		}
		if !user || !link {
			t.Errorf("topic links: user %v, link %v (%+v)", user, link, p.topic.LinkRects())
		}
		if bl := p.topic.Baselines(); len(bl) != 2 {
			t.Errorf("topic lines %d (baselines %v), want the clamp's 2", len(bl), bl)
		}
		if o := p.topic.Options(); !o.NoHoverCards || !o.NoEmojiTips || o.MaxLines != 2 {
			t.Errorf("topic options %+v", o)
		}
		chcShot(t, ops, "popups-channel-card-rich-topic-"+mode)
	}
}

// The stale tile (minus, Inactive) needs client.counts' latest, so the
// router test (ui) covers it.

// The name row's right group: the huddle badge (ChannelHuddleBadge) 4px
// before the mention count; without a mention count it ends the row.
func TestChannelHoverCardHuddleBadgeSlot(t *testing.T) {
	h, _ := codeEnv(t, false)
	prev := ChannelHuddleBadge
	t.Cleanup(func() { ChannelHuddleBadge = prev })
	var gotRight, gotH int
	ChannelHuddleBadge = func(gtx layout.Context, th *theme.Theme, ch string, right, rowH int) int {
		gotRight, gotH = right, rowH
		return right - 80
	}
	anchor := image.Rect(400, 500, 480, 522)
	p := NewChannelHoverCard(h, anchor, fakeslack.ThreadSharedID, true, false)
	chcFrames(t, p, func() bool { return p.State() == ChannelCardFull && gotRight != 0 && p.HuddleBadge() })
	r := p.Rect()
	if gotRight != r.Dx()-16 || gotH != 23 {
		t.Errorf("the badge ends at %d (row %d), want the content's right edge %d in the 22.5px name row", gotRight, gotH, r.Dx()-16)
	}
}
