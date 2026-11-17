package messages

// A broadcast's preamble checks every frame whether a name its root's
// preview drew reads otherwise now (a rename, a plugin's nickname). That
// read went through the mentions' member read, which counts as drawing
// the member, so the hub took every status or huddle change of a member
// named only in a root preview as one a list drew, and redrew every list
// (the users revision) for it. Synthetic data.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// storeMemberHooks installs the member read and its peek over app's
// store, as the ui root's lazy_users does.
func storeMemberHooks(t *testing.T, app *core.App) {
	storeMemberRead(t, app)
	richview.SetMemberPeek(func(id string) (string, richview.MemberMiss) {
		if u, ok := app.S.RawUser(id); ok && u != nil {
			return store.DisplayName(u), richview.MemberLoaded
		}
		return "", richview.MemberLoading
	})
	t.Cleanup(func() { richview.SetMemberPeek(nil) })
}

func TestPreviewMemberStatusRedrawsNothing(t *testing.T) {
	app, _, _ := shotsApp(t)
	storeMemberHooks(t, app)
	const ch, cara = "C0PREVPEEK", "UCARA0004"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 9})
	app.S.PutUsers([]slack.User{{ID: cara, Name: "cara", Profile: slack.Profile{DisplayName: "cara", RealName: "Cara Lee"}}})
	base := time.Now().Add(-2 * time.Hour).Unix()
	rootTS, bTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000300", base+600)
	mention := json.RawMessage(`[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"user","user_id":"` + cara + `"},{"type":"text","text":" can you review?"}]}]}]`)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "<@" + cara + "> can you review?", Blocks: mention}
	root.ThreadTS, root.ReplyCount, root.LatestReply = rootTS, 1, bTS
	bcast := slack.Message{Type: "message", Subtype: "thread_broadcast", TS: bTS, ThreadTS: rootTS, User: "UBEN00001",
		Text: "on it", Blocks: shotRich(shotSection("on it"))}
	rootCopy := root
	bcast.Root = &rootCopy
	app.S.SetLatest(ch, []slack.Message{bcast}, false) // only the thread has the root: nothing else on screen names Cara
	app.S.SetThread(ch, rootTS, []slack.Message{root, bcast})
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame := frameFn(v)
	until(t, 5*time.Second, "the broadcast's preview naming Cara", frame, func() bool {
		r := v.s.rows[bTS]
		return r != nil && strings.Contains(r.rootPrev, "@cara")
	})
	drawWhile(frame, 300*time.Millisecond, func() bool { return false }) // frame after frame, the preview checking its names
	rv0 := v.s.hb.usersRev()
	// her status changes: nothing a preview shows
	u := map[string]any{"id": cara, "name": "cara", "profile": map[string]any{"display_name": "cara", "real_name": "Cara Lee",
		"status_text": "in a meeting", "status_emoji": ":spiral_calendar_pad:"}}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
	app.Inject(slack.Event{Type: "user_change", Raw: raw})
	drawWhile(frame, 300*time.Millisecond, func() bool { return false })
	if rv := v.s.hb.usersRev(); rv != rv0 {
		t.Errorf("a status change of a member only a root preview names redrew every list (users revision %d → %d)", rv0, rv)
	}
	// a rename still shows (the peek reads her name)
	u["profile"] = map[string]any{"display_name": "kit.l", "real_name": "Cara Lee"}
	raw, _ = json.Marshal(map[string]any{"type": "user_change", "user": u})
	app.Inject(slack.Event{Type: "user_change", Raw: raw})
	if !drawWhile(frame, 5*time.Second, func() bool { r := v.s.rows[bTS]; return r != nil && strings.Contains(r.rootPrev, "@kit.l") }) {
		t.Errorf("the preview still reads %q after Cara's rename to kit.l", v.s.rows[bTS].rootPrev)
	}
}
