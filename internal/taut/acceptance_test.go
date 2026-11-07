package taut

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Acceptance: every plugin in Taut's plugins/, built with Taut's own
// `npm run build:user-plugin` (the taut workflow does that and sets
// TAUT_PLUGINS_DIR), imported unmodified the way a user imports it, must
// start, register its hooks, do its thing against synthetic state, and
// leave nothing behind when it stops.

func pluginsDir(t *testing.T) string {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the taut workflow builds Taut's plugins and runs this")
	}
	return dir
}

// pluginCheck does a plugin's thing against the harness after it started.
type pluginCheck func(t *testing.T, h *harness)

// before runs before the plugin is imported (seed storage, fetches).
var before = map[string]func(h *harness){
	"Nicknames": func(h *harness) {
		h.tt.data.write("plugin:Nicknames:storage", "nicknames", `{"UALICE":"Ally Nick"}`)
	},
	"ClearURLs": func(h *harness) {
		h.fetchMu.Lock()
		h.fetches["https://raw.githubusercontent.com/ClearURLs/Rules/"] = `{"providers":{"globalRules":{"urlPattern":".*","rules":["utm_[a-z]+"],"rawRules":[],"exceptions":[]}}}`
		h.fetchMu.Unlock()
	},
}

var checks = map[string]pluginCheck{
	"HumanCount": func(t *testing.T, h *harness) {
		nodes := h.render(1, "BaseAvatarStack", map[string]any{"channelId": chGen, "memberCount": 5})
		n := findNative(nodes, "BaseAvatarStack")
		if n == nil {
			t.Fatalf("no native BaseAvatarStack in %v", nodes)
		}
		p, _ := n["p"].(map[string]any)
		if p["memberCount"] != float64(1234) {
			t.Errorf("memberCount = %v, want the people count 1234", p["memberCount"])
		}
	},
	"Censorship": func(t *testing.T, h *harness) {
		var text string
		eventually(t, 3*time.Second, "censored view", func() bool {
			m, ok := h.tt.MessageView(chGen, "1700000001.000100")
			text = m.Text
			return ok
		})
		if text != "the *** fair is today" {
			t.Errorf("censored text %q", text)
		}
	},
	"ClearURLs": func(t *testing.T, h *harness) {
		var out string
		eventually(t, 5*time.Second, "rules loaded", func() bool {
			out = h.tt.TransformLink(h.ctx(), "https://example.com/page?utm_source=news&id=7", "paste")
			return out != "https://example.com/page?utm_source=news&id=7"
		})
		if out != "https://example.com/page?id=7" {
			t.Errorf("cleaned %q", out)
		}
	},
	"InvisibleForward": func(t *testing.T, h *harness) {
		in := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"link","url":"https://hackclub.slack.com/archives/C1/p1700000000000100"},{"type":"text","text":" look at this"}]}]}]`)
		out := h.tt.TransformSendBlocks(h.ctx(), in)
		if !strings.Contains(string(out), `"text":"⁠"`) && !strings.Contains(string(out), "⁠") {
			t.Errorf("link not hidden: %s", out)
		}
		if !strings.Contains(string(out), `"text":"look at this"`) {
			t.Errorf("text not trimmed: %s", out)
		}
	},
	"RealMarkdown": func(t *testing.T, h *harness) {
		if !h.tt.HasInlineMarkup() {
			t.Fatal("inline markup not patched")
		}
		spans := h.tt.ParseInlineMarkup(h.ctx(), "say **hi** now")
		if len(spans) != 1 || string(spans[0].Format) != `"bold"` || spans[0].Start != 4 || spans[0].End != 10 {
			t.Errorf("spans %+v", spans)
		}
	},
	"SilentTyping": func(t *testing.T, h *harness) {
		if !h.tt.ComposerButtons() {
			t.Fatal("no composer button")
		}
		type args struct {
			ChannelID string `json:"channelId"`
		}
		calls := 0
		native := func(ctx context.Context, a args) (any, error) { calls++; return nil, nil }
		if _, err := Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", args{chGen}, native); err != nil || calls != 1 {
			t.Fatalf("typing before silence: %v calls=%d", err, calls)
		}
		nodes := h.render(2, "TautComposerButtons", map[string]any{"placement": "end", "context": map[string]any{"kind": "channel", "channelId": chGen}})
		btn := findNative(nodes, "ComposerButton")
		if btn == nil {
			t.Fatalf("no ComposerButton in %v", nodes)
		}
		p, _ := btn["p"].(map[string]any)
		click, _ := p["onClick"].(map[string]any)
		id, _ := click["$fn"].(float64)
		if id == 0 {
			t.Fatalf("button props %v", p)
		}
		h.tt.Bridge().Call(int64(id), []byte("[]"), nil)
		eventually(t, 3*time.Second, "silenced", func() bool {
			before := calls
			_, _ = Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", args{chGen}, native)
			return calls == before
		})
	},
	"BringBackSlackbot": func(t *testing.T, h *harness) {
		var g any
		eventually(t, 2*time.Second, "forced experiment", func() bool {
			h.jsInto(`return T.redux.getStore().getState().experiments?.reskin_custom_responses?.group ?? null`, &g)
			return g == "off"
		})
	},
	"NoTrack": func(t *testing.T, h *harness) {
		var r map[string]any
		h.jsInto(`
			const blocked = await fetch('https://slackb.com/beacon/x').then(() => false, () => true)
			const other = await fetch('https://example.org/x').then((r) => r.status, () => -1)
			return {blocked, other}`, &r)
		if r["blocked"] != true || r["other"] != float64(404) {
			t.Errorf("NoTrack fetch: %v", r)
		}
	},
	"AnonymizeFilenames": func(t *testing.T, h *harness) {
		type upload struct {
			File File `json:"file"`
		}
		var got File
		native := func(ctx context.Context, a upload) (any, error) { got = a.File; return map[string]any{}, nil }
		if _, err := Dispatch(h.ctx(), h.tt, "addAndUploadPendingFile", upload{File{Name: "my-secret-report.pdf", Type: "application/pdf", Data: []byte("%PDF synthetic")}}, native); err != nil {
			t.Fatal(err)
		}
		if got.Name == "" || got.Name == "my-secret-report.pdf" || !strings.HasSuffix(got.Name, ".pdf") || string(got.Data) != "%PDF synthetic" {
			t.Errorf("uploaded as %q (%q)", got.Name, got.Data)
		}
	},
	"QuickJoin": func(t *testing.T, h *harness) {
		type item struct {
			Key   string `json:"key"`
			Label any    `json:"label"`
			Type  string `json:"type"`
			Click JSFunc `json:"click"`
		}
		native := func(ctx context.Context, a map[string]string) ([]item, error) {
			return []item{{Key: "copy", Label: "Copy link"}}, nil
		}
		menu, err := Dispatch(h.ctx(), h.tt, "getContextMenuTemplate", map[string]string{"id": chOther}, native)
		if err != nil {
			t.Fatal(err)
		}
		if len(menu) != 3 || menu[2].Key != "taut-quick-join__join" || !menu[2].Click.Valid() {
			t.Fatalf("menu %+v", menu)
		}
		mine, _ := Dispatch(h.ctx(), h.tt, "getContextMenuTemplate", map[string]string{"id": chGen}, native)
		if len(mine) != 1 {
			t.Errorf("member channel got a join item: %+v", mine)
		}
		if _, err := h.tt.Call(h.ctx(), menu[2].Click); err != nil {
			t.Fatal(err)
		}
		eventually(t, 5*time.Second, "join call", func() bool { return len(h.api.called("conversations.join")) > 0 })
	},
	"Nicknames": func(t *testing.T, h *harness) {
		var mf MemberFields
		eventually(t, 3*time.Second, "nickname", func() bool {
			var ok bool
			mf, ok = h.tt.MemberView("UALICE")
			return ok && mf.Name == "Ally Nick"
		})
	},
	"RestrictedChannelWarning": func(t *testing.T, h *harness) {
		in := h.introspect()
		if !has(in.Slices, "readOnlyChannels") && !has(in.Slices, "threadOnlyChannels") && len(in.Components) == 0 {
			t.Errorf("no restriction patches: %+v", in)
		}
	},
	"OfficeHours": func(t *testing.T, h *harness) {
		if len(h.tt.PreferencesTabs()) == 0 && len(h.introspect().Components) == 0 && len(h.introspect().Tabs) == 0 {
			t.Log("office hours registered no UI with default config")
		}
	},
}

// wantHooks is what each plugin registers with Taut's default config plus
// enabled (checked against the plugins' sources): every listed name must be
// registered. "styles"/"resize"/… give minimum counts.
const wantHooks = `
AccountSwitcher | components=MenuFromTemplate
AdminBackend | components=MenuFromTemplate,RimetoMemberProfileOverflowMenu
AnonymizeFilenames | thunks=addAndUploadPendingFile,uploadFiles
BetterTypingIndicator | components=ThreadTypingIndicator,TypingNames styles=1
BringBackSlackbot | slices=experiments exp=reskin_custom_responses
BrowserPopouts |
Censorship | thunks=searchInline,showNotification slices=messages,searchResults
ClearURLs | linkTransforms=1
ClickToLoad | components=AttachmentMedia styles=1
CopyReacted | components=ReactionBar,ReactionAddButton styles=1
CustomNameRecording | components=EditAudioButton styles=1
Experiments | tabs=Experiments styles=1
FasterSlack | styles=1 resize=1
HaikuWarning | components=BaseContextBarV2 styles=1
HuddleActivity | components=HuddleMetaEvent,HuddleBaseEvent,HuddlesAlertBannerBase,CaptionsSubmenu,CaptionsItemsMenu thunks=displayAlertMessage,processAwsTranscription,startEventLog,stopEventLog,stopHuddle,enqueueHuddleToast rtm=sh_room_join,sh_room_leave,sh_room_update peers=1
HumanCount | components=BaseAvatarStack
IdvStatus | components=BaseMessageSender styles=1
InvisibleForward | sendTransforms=1
LastSeen | components=RimetoProfilePresence,MemberProfileHoverCard,BasePresence,LocalTime rtm=presence_change,user_typing,message,reaction_added,reaction_removed,pin_added,pin_removed,file_shared,sh_room_join styles=1
NativeAtChannel | thunks=autocompleteMembers,prepareAndSendMessage,clearDrafts,chatDeleteFetcher,commitMessageEdit rtm=message,view_opened actionable=1
Nicknames | components=MenuFromTemplate,RimetoMemberProfileOverflowMenu thunks=getMemberContextMenuTemplate slices=members styles=1
NoTrack |
OfficeHours |
Oneko |
OwlMode | components=InputContainer,MessageInput,WysiwygContainer,BaseContextBarV2,ShortcutsMenuTriggerButton,ReactionBar,MessageActions,MessageActionButtonReaction,EmojiPickerPopover,EmojiPicker,ViewBlocks,MessageBlocks thunks=prepareAndSendMessage,currentUserStartedTyping,syncDraftCreate,syncDraftUpdates,changeReactionFromUserAction,submitAddOrRemoveReaction rtm=view_opened buttons=taut-owl-mode styles=1 sendChecks=1
PrivateChannel | components=BaseMrkdwnChannel,ListChannelEntity,BaseMessageAttachmentSlackMessage,MessageAttachmentHuddleLink,HuddleLinkJoinGate thunks=fetchRawChannelsById,autocompleteChannels slices=channels
QuickJoin | thunks=getContextMenuTemplate
RealMarkdown | inlineMarkup=1
RestrictedChannelWarning | components=MessageInputRoadblock slices=readOnlyChannels,threadOnlyChannels styles=1
ShinigamiEyes |
ShowRealUser | components=Blocks,MessageWrapper,ThreadRootGeneric,ActivityItem,MessageListItem slices=messages
ShowSendingBot | components=ConnectedBaseAvatar,Blocks,MessageBackground,MessageAvatar,SearchResult,MessageAttachment,ActivityRowContentSenderAvatarLayout,MessageKitGutter,MessageSender,ThreadSenderAndTimestampGeneric styles=1
ShutUpSlackbot | thunks=showNotification rtm=message
SilentTyping | thunks=currentUserStartedTyping buttons=taut-silent-typing
SlackFixes | components=ThreadFlexpaneAdminTools styles=2
SlimMessageBox | components=TextyButtons buttons=taut-broadcast styles=3
StreamerMode | styles=1
ThreadJump | components=ThreadSearchButton slices=experiments styles=1 exp=find_in_thread
UserPronouns | components=BaseBroadcastPreamble,ThreadSenderAndTimestampGeneric styles=1
WhoReacted | components=Reaction,UnstyledButton styles=1
`

func checkHooks(t *testing.T, id string, in introspection) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(wantHooks), "\n") {
		name, spec, _ := strings.Cut(line, "|")
		if strings.TrimSpace(name) != id {
			continue
		}
		for _, f := range strings.Fields(spec) {
			k, v, _ := strings.Cut(f, "=")
			list := map[string][]string{"components": in.Components, "thunks": in.Thunks, "rtm": in.RTM, "buttons": in.Buttons, "slices": in.Slices, "tabs": in.Tabs}[k]
			counts := map[string]int{"styles": len(in.Styles), "resize": in.Resize, "peers": in.Peers, "sendChecks": in.SendChecks, "linkTransforms": in.LinkTransforms,
				"sendTransforms": in.SendTransforms, "actionable": in.Actionable}
			switch k {
			case "components", "thunks", "rtm", "buttons", "slices", "tabs":
				for _, want := range strings.Split(v, ",") {
					if !has(list, want) {
						t.Errorf("%s: %s %q not registered (have %v)", id, k, want, list)
					}
				}
			case "exp":
				for _, want := range strings.Split(v, ",") {
					if _, ok := in.Experiments[want]; !ok {
						t.Errorf("%s: experiment %q not forced (have %v)", id, want, in.Experiments)
					}
				}
			case "inlineMarkup":
				if !in.InlineMarkup {
					t.Errorf("%s: inline markup not patched", id)
				}
			default:
				var n int
				_, _ = fmt.Sscan(v, &n)
				if counts[k] < n {
					t.Errorf("%s: %s = %d, want at least %d", id, k, counts[k], n)
				}
			}
		}
		return
	}
	t.Errorf("%s: no expected hooks listed for this plugin", id)
}

func TestPlugins(t *testing.T) {
	dir := pluginsDir(t)
	files, _ := filepath.Glob(filepath.Join(dir, "*.js"))
	if len(files) < 40 {
		t.Fatalf("found %d built plugins in %s, want all 40", len(files), dir)
	}
	sort.Strings(files)
	report := map[string]string{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".js")
		ok := t.Run(name, func(t *testing.T) { runPlugin(t, f) })
		if ok {
			report[name] = "PASS"
		} else {
			report[name] = "FAIL"
		}
	}
	var lines []string
	for n, r := range report {
		lines = append(lines, r+" "+n)
	}
	sort.Strings(lines)
	t.Logf("acceptance:\n%s", strings.Join(lines, "\n"))
}

func runPlugin(t *testing.T, file string) {
	code, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	var meta Meta
	if err := h.rt().call(h.ctx(), "load", &meta, string(code)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if f := before[meta.ID]; f != nil {
		f(h)
	}
	if err := h.tt.cfg.edit(meta.ID, func(b map[string]json.RawMessage) { b["enabled"] = json.RawMessage("true") }); err != nil {
		t.Fatal(err)
	}
	id, err := h.tt.Import(h.ctx(), code, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	p := h.tt.get(id)
	if !p.running || p.err != "" {
		t.Fatalf("not running: %q", p.err)
	}
	// give async start work (awaited stores, waitForExport) a moment
	time.Sleep(150 * time.Millisecond)
	in := h.introspect()
	b, _ := json.Marshal(in)
	t.Logf("%s registered: %s", id, b)
	if !has(in.Running, id) {
		t.Errorf("JS doesn't list %s as running", id)
	}
	checkHooks(t, id, in)
	if c := checks[id]; c != nil {
		t.Run("behaviour", func(t *testing.T) { c(t, h) })
	}
	// settings list it with its options
	found := false
	for _, c := range h.tt.Settings().Categories {
		for _, pi := range c.Plugins {
			if pi.ID == id {
				found = true
				if pi.Name == "" || !pi.Running {
					t.Errorf("settings row %+v", pi)
				}
			}
		}
	}
	if !found {
		t.Error("not in settings")
	}
	// stopping undoes every registration
	if err := h.tt.SetEnabled(h.ctx(), id, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	after := h.introspect()
	if len(after.Running) != 0 || len(after.Components) != 0 || len(after.Thunks) != 0 || len(after.RTM) != 0 || len(after.Buttons) != 0 ||
		after.SendChecks != 0 || after.LinkTransforms != 0 || after.InlineMarkup || after.SendTransforms != 0 || len(after.Slices) != 0 ||
		after.StatePatches != 0 || len(after.Tabs) != 0 || len(after.Experiments) != 0 || after.Resize != 0 || after.Actionable != 0 {
		b, _ := json.Marshal(after)
		t.Errorf("left behind after stop: %s", b)
	}
	for k := range h.ui.styles {
		if strings.HasPrefix(k, "plugin:"+id+":") || strings.HasPrefix(k, "anon:") {
			t.Errorf("style %s left behind", k)
		}
	}
	// and it starts again (enable mid-session)
	if err := h.tt.SetEnabled(h.ctx(), id, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
}
