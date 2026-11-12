package views

// W13 (docs/GAPS-2026-11-11.md gap 14): channel details' Start a Call ▾
// and Send emails to this channel against internal/fakeslack (nothing
// is dialled or mailed). Slack's cites are in details_call.go,
// details_email.go and popups/channel_email.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
)

const (
	w13DM    = "D0W13DM001"
	w13Peer  = "U0W13PEER1"
	w13Phone = "+1 555 0100"
)

// w13Host is the test Host with the ui root's huddle rows, Post and
// OpenURL.
type w13Host struct {
	*kbHost
	huddles bool
	mu2     sync.Mutex
	posts   []func()
	urls    []string
}

func (h *w13Host) SupportsHuddles(string) bool { return h.huddles }
func (h *w13Host) HuddleCallMenu(string) []popups.MenuItem {
	return []popups.MenuItem{{Key: "huddle_button_menu_item", Label: "Start Huddle", Icon: "headphones", Do: func() {}},
		{Key: "copy-huddle-link", Label: "Copy huddle link", Icon: "link", Do: func() {}}}
}
func (h *w13Host) Post(f func()) {
	h.mu2.Lock()
	h.posts = append(h.posts, f)
	h.mu2.Unlock()
}
func (h *w13Host) OpenURL(u string) {
	h.mu2.Lock()
	h.urls = append(h.urls, u)
	h.mu2.Unlock()
}
func (h *w13Host) drain() {
	h.mu2.Lock()
	ps := h.posts
	h.posts = nil
	h.mu2.Unlock()
	for _, f := range ps {
		f()
	}
}
func (h *w13Host) opened() []string {
	h.mu2.Lock()
	defer h.mu2.Unlock()
	return append([]string(nil), h.urls...)
}

// w13Env boots a core on fakeslack (pre runs before boot), puts a DM with
// a person who has a phone number in the store, and lays the details of
// ch out with every open overlay over them.
type w13Env struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	h   *w13Host
	d   *ChannelDetails
	e   *vaEnv
}

func newW13Env(t *testing.T, ch string, pre func(fs *fakeslack.Server)) *w13Env {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if pre != nil {
		pre(fs)
	}
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.S.PutUsers([]slack.User{{ID: w13Peer, Name: "peer", RealName: "Peer Example", Profile: slack.Profile{Phone: w13Phone}}})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: w13DM, IsIM: true, User: w13Peer})
	h := &w13Host{kbHost: newKBHost(app, false)}
	env := &w13Env{t: t, fs: fs, app: app, h: h}
	env.d = NewChannelDetails(h, ch)
	env.e = &vaEnv{size: image.Pt(1440, 900)}
	env.e.draw = func(gtx layout.Context) {
		h.drain()
		env.d.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	return env
}

func (w *w13Env) prefs(js string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		w.t.Fatal(err)
	}
	w.app.SetTeamPrefs(m)
}

const w13Apps = `{"allow_calls":true,"calls_apps":{"video":[{"id":"A00","name":"Slack"},{"id":"A5GE9BMQC","name":"Zoom"},` +
	`{"id":"AMEET0001","name":"Meet","image":"https://x.example/meet.png"}],"audio":[{"id":"ADIAL0001","name":"Dialpad"}]}}`

// The header button: hidden with nothing to offer; "Huddle" (headphones,
// no chevron) with huddles and no call apps; "Start a Call" with the
// chevron when the team has more than one app; the tooltips.
func TestDetailsCallButtonLikeSlack(t *testing.T) {
	w := newW13Env(t, fakeslack.ViewsChannel, nil)
	w.e.frame()
	if vis, _, _, _ := w.d.CallButton(); vis {
		t.Fatal("no calls, no apps, no huddles: Slack draws no button")
	}
	w.h.huddles = true
	vis, label, caret, dis := w.d.CallButton()
	if !vis || label != "Huddle" || caret || dis {
		t.Fatalf("huddles only: %v %q caret %v disabled %v", vis, label, caret, dis)
	}
	if tip := strings.Join(w.d.CallTip(), " | "); tip != "Huddle in #views-fixtures" {
		t.Errorf("huddle-only tip %q", tip)
	}
	w.prefs(w13Apps)
	vis, label, caret, _ = w.d.CallButton()
	if !vis || label != "Start a Call" || !caret {
		t.Fatalf("three apps: %v %q caret %v", vis, label, caret)
	}
	if tip := strings.Join(w.d.CallTip(), " | "); tip != "Start a call | 3 calling options" {
		t.Errorf("apps tip %q", tip)
	}
	w.h.huddles = false
	w.prefs(`{"calls_apps":{"video":[{"id":"AMEET0001","name":"Meet"}]}}`)
	vis, label, caret, _ = w.d.CallButton()
	if !vis || label != "Start a Call" || caret {
		t.Fatalf("one app: %v %q caret %v", vis, label, caret)
	}
	if tip := strings.Join(w.d.CallTip(), " | "); tip != "Start a call | using Meet" {
		t.Errorf("one app tip %q", tip)
	}

	dm := newW13Env(t, w13DM, nil)
	dm.prefs(w13Apps)
	if tip := strings.Join(dm.d.CallTip(), " | "); tip != "Call Peer Example | 3 calling options" && tip != "Call peer | 3 calling options" {
		t.Errorf("DM tip %q", tip)
	}
}

// CallMenu's rows, the confirmation outside a DM and calls.request.
func TestDetailsCallMenuStartsCall(t *testing.T) {
	w := newW13Env(t, fakeslack.ViewsChannel, nil)
	w.h.huddles = true
	w.prefs(w13Apps)
	w.e.frame()
	w.d.PressCall()
	w.e.frame()
	m := w.d.CallMenu()
	if m == nil {
		t.Fatal("no call menu")
	}
	if got := strings.Join(m.Labels(), " | "); got != "Start Huddle | Copy huddle link | [Video meetings] | Zoom | Meet" {
		t.Fatalf("channel menu: %s (no phone apps in a channel)", got)
	}
	items := m.Items()
	if items[3].IconURL != "https://app.slack.com/marketing/img/homepage/icons/zoom-icon.png" || items[4].IconURL != "https://x.example/meet.png" {
		t.Errorf("app icons %q %q", items[3].IconURL, items[4].IconURL)
	}
	items[4].Do()
	w.e.frame()
	c := w.d.CallConfirm()
	if c == nil {
		t.Fatal("a channel call didn't ask first")
	}
	if n := len(w.fs.CallsEmailCalls()); n != 0 {
		t.Fatalf("calls.request before the confirmation: %d", n)
	}
	c.PressDefault() // Start Call
	w.e.until(t, "the call's URL", func() bool { return len(w.h.opened()) == 1 })
	calls := w.fs.CallsEmailCalls()
	if len(calls) != 1 || calls[0].Method != "calls.request" {
		t.Fatalf("calls %+v", calls)
	}
	if f := calls[0].Form; f.Get("channel") != fakeslack.ViewsChannel || f.Get("app") != "AMEET0001" || f.Get("type") != "video" || f.Has("phone_number") {
		t.Errorf("calls.request form %v", f)
	}
	if u := w.h.opened()[0]; !strings.Contains(u, "fake-call/AMEET0001") {
		t.Errorf("opened %q", u)
	}

	// a DM: the phone group, and no confirmation
	dm := newW13Env(t, w13DM, nil)
	dm.prefs(w13Apps)
	dm.e.frame()
	dm.d.PressCall()
	dm.e.frame()
	m = dm.d.CallMenu()
	if m == nil {
		t.Fatal("no DM call menu")
	}
	want := "[Video meetings] | Zoom | Meet | --- | [Phone " + w13Phone + "] | Dialpad"
	if got := strings.Join(m.Labels(), " | "); got != want {
		t.Fatalf("DM menu\n got %s\nwant %s", got, want)
	}
	items = m.Items()
	items[len(items)-1].Do()
	dm.e.frame()
	if dm.d.CallConfirm() != nil {
		t.Fatal("a DM call asked first")
	}
	dm.e.until(t, "the DM call", func() bool { return len(dm.h.opened()) == 1 })
	if f := dm.fs.CallsEmailCalls()[0].Form; f.Get("type") != "audio" || f.Get("phone_number") != w13Phone {
		t.Errorf("audio call form %v", f)
	}

	// one app and no huddle: the click starts it (after the channel's
	// confirmation), no menu
	one := newW13Env(t, fakeslack.ViewsChannel, nil)
	one.prefs(`{"calls_apps":{"video":[{"id":"AMEET0001","name":"Meet"}]}}`)
	one.e.frame()
	one.d.PressCall()
	one.e.frame()
	if one.d.CallMenu() != nil || one.d.CallConfirm() == nil {
		t.Fatalf("one app: menu %v, confirm %v", one.d.CallMenu() != nil, one.d.CallConfirm() != nil)
	}
}

func w13PNG(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 200))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// About's emails field and the modal's panes: get (a new address, Copy),
// customize (emoji, then an image through the crop pane), delete.
func TestDetailsSendEmailsLikeSlack(t *testing.T) {
	w := newW13Env(t, fakeslack.ViewsChannel, func(fs *fakeslack.Server) {
		fs.Permissions = map[string][]string{fakeslack.ViewsChannel: {"CAN_AT_CHANNEL", core.PermCreateChannelEmailAddresses}}
		fs.SetEnterpriseOrgPrefs(map[string]any{"channel_email_addresses_enabled": true})
	})
	w.e.until(t, "the emails field", func() bool {
		_ = w.app.ChannelGrant(fakeslack.ViewsChannel, core.PermCreateChannelEmailAddresses)
		return w.d.EmailsMode() == "button"
	})
	no := newW13Env(t, fakeslack.ViewsChannel, nil) // no grant: nothing
	time.Sleep(200 * time.Millisecond)
	no.e.frame()
	if m := no.d.EmailsMode(); m != "" {
		t.Errorf("without CREATE_CHANNEL_EMAIL_ADDRESSES: %q", m)
	}

	w.d.PressEmails()
	w.e.frame()
	em := w.d.EmailModal()
	if em == nil || em.Pane() != popups.CEGet {
		t.Fatal("the modal didn't open on get")
	}
	w.e.until(t, "info", func() bool { return em.Status() == "done" })
	if em.Email().Address != "" || em.Failed() {
		t.Fatalf("before Get Email Address: %+v failed %v", em.Email(), em.Failed())
	}
	evs, stop := w.app.Subscribe()
	defer stop()
	var mu sync.Mutex
	prompt := ""
	go func() {
		for ev := range evs {
			if ev.Type == "message" && ev.Channel == fakeslack.ViewsChannel && ev.Msg != nil && ev.UserID == "USLACKBOT" {
				mu.Lock()
				prompt = ev.Msg.Text
				mu.Unlock()
			}
		}
	}()
	em.GetAddress()
	w.e.until(t, "the new address", func() bool { return em.Email().Address != "" })
	addr := em.Email().Address
	if got := popups.CEShortAddress(addr); !strings.HasSuffix(got, "...@fakeclub.slack.example") || len(strings.Split(got, "...@")[0]) != 27 {
		t.Errorf("short address %q", got)
	}
	if got := em.CopyAddress(); got != `"#views-fixtures (Slack)" <`+addr+`>` {
		t.Errorf("copied %q", got)
	}
	if !slices.Contains(w.h.toasts(), "Email address copied to clipboard.") {
		t.Errorf("toasts %q", w.h.toasts())
	}
	// Slackbot's share prompt in the channel for a new address
	w.e.until(t, "the share prompt", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.HasPrefix(prompt, "Now that you have set an email address for #views-fixtures")
	})

	em.SwitchPane(popups.CEHelp)
	w.e.frame()
	em.SwitchPane(popups.CEUpdate)
	w.e.frame()
	u := em.Update()
	if u.Choice() != "defaultIcon" {
		t.Errorf("a new address's icon choice %q", u.Choice())
	}
	u.SetName("Alerts")
	u.SetEmoji("bell")
	u.Save()
	w.e.until(t, "the update", func() bool { return em.Dismissed() })
	if !slices.Contains(w.h.toasts(), "Updated email icon and name for #views-fixtures") {
		t.Errorf("toasts %q", w.h.toasts())
	}
	var upd url.Values
	for _, c := range w.fs.CallsEmailCalls() {
		if c.Method == "conversations.emailaddresses.update" {
			upd = c.Form
		}
	}
	if upd.Get("name") != "Alerts" || upd.Get("emoji") != ":bell:" || upd.Get("address") != addr {
		t.Errorf("update form %v", upd)
	}

	// an image: the file chooser, avatars.upload, the cropper, avatars.crop
	data := w13PNG(t)
	old := popups.PickFiles
	popups.PickFiles = func(string, bool) ([]core.UploadFile, error) {
		return []core.UploadFile{{Name: "icon.png", Data: data}}, nil
	}
	t.Cleanup(func() { popups.PickFiles = old })
	w.d.PressEmails()
	w.e.frame()
	em = w.d.EmailModal()
	w.e.until(t, "the kept address", func() bool { return em.Status() == "done" && em.Email().Address == addr })
	em.SwitchPane(popups.CEUpdate)
	w.e.frame()
	em.Update().Choose("image")
	w.e.until(t, "the crop pane", func() bool { return em.Pane() == popups.CECrop && em.Crop().Ready() })
	if box := em.Crop().CropBox(); box != "50,0,200" {
		t.Errorf("first square %q, want the centred 200px square", box)
	}
	em.Crop().Save()
	w.e.until(t, "back on update", func() bool { return em.Pane() == popups.CEUpdate })
	if em.Update().Choice() != "image" {
		t.Errorf("after the crop: %q", em.Update().Choice())
	}
	em.Update().Save()
	w.e.until(t, "the image update", func() bool { return em.Dismissed() })
	var crop, upd2 url.Values
	for _, c := range w.fs.CallsEmailCalls() {
		switch c.Method {
		case "avatars.crop":
			crop = c.Form
		case "conversations.emailaddresses.update":
			upd2 = c.Form
		}
	}
	if crop.Get("cropbox") != "50,0,200" || upd2.Get("avatar_id") == "" || upd2.Has("emoji") || upd2.Get("name") != "Alerts" {
		t.Errorf("crop %v, update %v", crop, upd2)
	}

	// delete
	w.d.PressEmails()
	w.e.frame()
	em = w.d.EmailModal()
	w.e.until(t, "the address", func() bool { return em.Status() == "done" && em.Email().Address != "" })
	em.SwitchPane(popups.CEDelete)
	w.e.frame()
	em.DeleteAddress()
	w.e.until(t, "revoked", func() bool { return slices.Contains(w.h.toasts(), "Email address revoked") })
	if _, ok := w.app.ChannelEmailCached(fakeslack.ViewsChannel); ok {
		t.Error("the deleted address is still kept")
	}
}

// The upgrade prompt on a free team, opening the plans page.
func TestDetailsSendEmailsUpgrade(t *testing.T) {
	w := newW13Env(t, fakeslack.ViewsChannel, nil)
	w.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "team.info" {
			return map[string]any{"ok": true, "team": map[string]any{"id": fakeslack.TeamID, "name": "Fake Club", "domain": "fakeclub"}}, true
		}
		return nil, false
	})
	cr := w.fs.Creds()
	cr.EnterpriseID, cr.EnterpriseToken, cr.EnterpriseURL = "", "", ""
	app := core.New(slack.New(cr), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true})
	h := &w13Host{kbHost: newKBHost(app, false)}
	d := NewChannelDetails(h, fakeslack.ViewsChannel)
	e := &vaEnv{size: image.Pt(1440, 900), draw: func(gtx layout.Context) { d.Layout(gtx) }}
	e.until(t, "the upgrade prompt", func() bool { _, ok, _ := d.info.get(); return ok && d.EmailsMode() == "upgrade" })
	e.frame()
	d.em.upgrade.Click()
	e.frame()
	e.frame()
	if got := h.opened(); len(got) != 1 || !strings.Contains(got[0], "/plans?entry_point=channel_details_modal_integrations_tab_email_to_channel") {
		t.Errorf("opened %q", got)
	}
}
