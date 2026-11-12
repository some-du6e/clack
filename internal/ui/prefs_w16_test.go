package ui

import (
	"image"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// w16Hooks answers the methods fakeslack doesn't serve and logs every call.
type w16Hooks struct {
	mu   sync.Mutex
	outs map[string][]any // answers in turn; the last one repeats
	log  callLog
}

func newW16Hooks(fs *fakeslack.Server) *w16Hooks {
	h := &w16Hooks{outs: map[string][]any{}}
	fs.SetHook(func(m string, f url.Values) (any, bool) {
		h.log.add(m, f)
		h.mu.Lock()
		defer h.mu.Unlock()
		q := h.outs[m]
		if len(q) == 0 {
			return nil, false
		}
		out := q[0]
		if len(q) > 1 {
			h.outs[m] = q[1:]
		}
		return out, true
	})
	return h
}

func (h *w16Hooks) answer(m string, outs ...any) {
	h.mu.Lock()
	h.outs[m] = outs
	h.mu.Unlock()
}

func (h *w16Hooks) count(m string) int {
	h.log.mu.Lock()
	defer h.log.mu.Unlock()
	n := 0
	for _, c := range h.log.calls {
		if c == m {
			n++
		}
	}
	return n
}

// layoutTop draws the top overlay once (its buttons' clicks run).
func layoutTop(t *testing.T, a *App) *popups.Modal {
	t.Helper()
	md, ok := topModal(a)
	if !ok {
		t.Fatal("no modal on top")
	}
	md.Layout(ttContext(new(op.Ops), image.Pt(1200, 900)))
	return md
}

// drawnBy collects every string Text draws while f runs.
func drawnBy(f func()) []string {
	var out []string
	prev := theme.DrawHook
	theme.DrawHook = func(s string) { out = append(out, s) }
	defer func() { theme.DrawHook = prev }()
	f()
	return out
}

func hasText(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func gdriveAuths(tokens ...map[string]any) map[string]any {
	toks := make([]any, len(tokens))
	for i, t := range tokens {
		toks[i] = t
	}
	return map[string]any{"ok": true, "apps": map[string]any{
		"A0GDRIVE": map[string]any{"app_name": "Google Drive", "app_icon_url": "", "providers": map[string]any{
			"google": map[string]any{"provider_name": "Google", "valid_tokens": toks},
		}}}}
}

func tok(id, user, model string) map[string]any {
	return map[string]any{"external_token_id": id, "external_user_id": user, "auth_model": model}
}

// Connected accounts: every app row has Slack's Manage button; it opens
// the manage-accounts modal (H7Jb) listing the provider's accounts, and a
// Disconnect there confirms (j8Vt) and calls apps.auth.external.delete,
// then fetches the auths again. A failed delete keeps the confirmation
// open with Slack's error.
func TestPrefsConnectedManage(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	ca.S.Self.ID = fakeslack.SelfID
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	h := newW16Hooks(fs)
	h.answer("apps.auth.external.userAuths.get",
		gdriveAuths(tok("Et1", "me@example.com", "user_oauth"), tok("Et2", "it@example.com", "dwa_user")),
		gdriveAuths(tok("Et2", "it@example.com", "dwa_user")))
	pageRows(t, a, p, "connected_accounts")
	waitFor(t, a, "connected apps", func() bool { return p.lists.connDone })
	rows := pageRows(t, a, p, "connected_accounts")
	if !hasRow(rows, "list", "Google Drive · Manage") {
		t.Fatalf("connected accounts rows %s", dumpRows(rows))
	}
	ap, _ := p.connectedApp("A0GDRIVE")
	if len(ap.Providers) != 1 || ap.Providers[0].Name != "Google" || len(ap.Providers[0].Tokens) != 2 {
		t.Fatalf("providers %+v", ap.Providers)
	}

	n := len(a.overlays)
	p.btn("row.conn.A0GDRIVE").Click()
	pageRows(t, a, p, "connected_accounts")
	if len(a.overlays) != n+1 || p.manage == nil {
		t.Fatal("Manage didn't open the manage-accounts modal")
	}
	strs := drawnBy(func() { layoutTop(t, a) })
	for _, want := range []string{"Google Drive", "Connected Google Accounts", "it@example.com", "Added by your administrator", "me@example.com", "Disconnect", "Done"} {
		if !hasText(strs, want) {
			t.Errorf("manage modal didn't draw %q: %q", want, strs)
		}
	}

	// Disconnect me@example.com: the confirmation, then a failed delete
	h.answer("apps.auth.external.delete", map[string]any{"ok": false, "error": "internal_error"}, map[string]any{"ok": true})
	p.manage.btn("disconnect.Et1").Click()
	layoutTop(t, a)
	if len(a.overlays) != n+2 {
		t.Fatal("Disconnect didn't open the confirmation")
	}
	strs = drawnBy(func() { layoutTop(t, a) })
	for _, want := range []string{"Are you sure you want to disconnect this Google account?", "me@example.com", "What will change:",
		"If you’ve built a workflow using this account, those workflows will stop running.", "Note: This will not revoke existing tokens"} {
		if !hasText(strs, want) {
			t.Errorf("confirmation didn't draw %q: %q", want, strs)
		}
	}
	md, _ := topModal(a)
	md.PressDefault()
	waitFor(t, a, "the failed delete", func() bool { return h.count("apps.auth.external.delete") == 1 })
	waitFor(t, a, "the error", func() bool {
		return hasText(drawnBy(func() { layoutTop(t, a) }), "Something went wrong. Please try again.")
	})
	if len(a.overlays) != n+2 {
		t.Fatal("a failed delete closed the confirmation")
	}
	f, _ := h.log.form("apps.auth.external.delete")
	if f.Get("external_token_id") != "Et1" || f.Get("reason") != "delete-user-auth" {
		t.Fatalf("delete form %v", f)
	}

	// again: it goes through, the confirmation closes, the auths reload;
	// the manage modal stays (an administrator's account is left) and
	// lists only that one
	md, _ = topModal(a)
	md.PressDefault()
	waitFor(t, a, "the delete and the reload", func() bool {
		ap, ok := p.connectedApp("A0GDRIVE")
		return h.count("apps.auth.external.delete") == 2 && h.count("apps.auth.external.userAuths.get") >= 2 &&
			ok && len(ap.Providers) == 1 && len(ap.Providers[0].Tokens) == 1
	})
	waitFor(t, a, "the confirmation closed", func() bool { return len(a.overlays) == n+1 })
	strs = drawnBy(func() { layoutTop(t, a) })
	if hasText(strs, "me@example.com") || !hasText(strs, "it@example.com") {
		t.Fatalf("manage modal after the delete drew %q", strs)
	}
}

// H7Jb's sections: domain-wide rows first in a provider, providers with a
// domain-wide row first, empty providers dropped, response order kept.
func TestPrefsManageSections(t *testing.T) {
	ap := core.WorkflowAppAuth{Providers: []core.WorkflowProvider{
		{Key: "a", Name: "A", Tokens: []core.WorkflowToken{{ID: "1", AuthModel: "user_oauth"}}},
		{Key: "e", Name: "Empty"},
		{Key: "b", Name: "B", Tokens: []core.WorkflowToken{{ID: "2", AuthModel: "user_oauth"}, {ID: "3", AuthModel: "dwa_app"}}},
	}}
	var got []string
	for _, s := range manageSections(ap) {
		ids := s.name + ":"
		for _, r := range s.rows {
			ids += r.tok.ID
		}
		got = append(got, ids)
	}
	if strings.Join(got, " ") != "B:32 A:1" {
		t.Fatalf("sections %v, want [B:32 A:1]", got)
	}
	ap.Providers[2].Tokens[1].AuthModel = "user_oauth"
	got = got[:0]
	for _, s := range manageSections(ap) {
		got = append(got, s.name)
	}
	if strings.Join(got, " ") != "A B" {
		t.Fatalf("without domain-wide auths: %v, want [A B]", got)
	}
}

// cellsEnv draws VerifyEmailModal's body through a real router.
type cellsEnv struct {
	t   *testing.T
	v   *verifyEmail
	r   input.Router
	now time.Time
}

func (e *cellsEnv) frame() {
	ops := new(op.Ops)
	e.now = e.now.Add(time.Second / 60)
	gtx := layout.Context{Ops: ops, Now: e.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(464, 600)}, Source: e.r.Source()}
	e.v.body(gtx)
	e.r.Frame(ops)
}

func (e *cellsEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
	}
}

func (e *cellsEnv) typeIn(s string) {
	e.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: s}, key.SelectionEvent{Start: 1, End: 1})
	e.frames(3)
}

func (e *cellsEnv) focused() int {
	for i := range e.v.cells {
		if e.r.Source().Focused(&e.v.cells[i]) {
			return i
		}
	}
	return -1
}

// Send Verification Email: Slack's section (a primary button now), the
// code request (signup.confirmEmail), VerifyEmailModal's six cells (typing
// moves right and uppercases, ← and Backspace in an empty cell move left,
// a paste fills from the focused cell), signin.confirmCode with the code,
// an invalid code clearing the cells, and success listing the blocked
// invitations.
func TestPrefsVerifyEmailFlow(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	ca.S.Self.ID = fakeslack.SelfID
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	h := newW16Hooks(fs)
	h.answer("users.emailVerificationStatus.get", map[string]any{"ok": true, "is_verified": false})
	h.answer("signup.confirmEmail", map[string]any{"ok": true})
	h.answer("signin.confirmCode", map[string]any{"ok": false, "error": "failed"}, map[string]any{"ok": true})
	h.answer("users.listIgnoredUsers", map[string]any{"ok": true, "emails": []string{"spam@example.com"}})
	pageRows(t, a, p, "privacy_visibility")
	waitFor(t, a, "verification status", func() bool { return p.lists.blockDone })
	rows := pageRows(t, a, p, "privacy_visibility")
	var btn *prefsDrawn
	for i := range rows {
		if rows[i].Key == "button.verify_email" {
			btn = &rows[i]
		}
	}
	if btn == nil || !btn.Enabled || btn.Label != "Send Verification Email" {
		t.Fatalf("no enabled Send Verification Email: %s", dumpRows(rows))
	}
	if !hasRowPrefix(rows, "text", "To see whose invitations you’ve blocked, you’ll need to verify your email address", ".") {
		t.Fatalf("unverified copy missing: %s", dumpRows(rows))
	}

	n := len(a.overlays)
	p.sendVerificationEmail("me@example.com")
	waitFor(t, a, "the code request", func() bool { return h.count("signup.confirmEmail") == 1 && len(a.overlays) == n+1 })
	f, _ := h.log.form("signup.confirmEmail")
	if f.Get("email") != "me@example.com" || f.Get("reason") != "request-new-email-verification-code" || f.Get("locale") == "" ||
		f.Get("entry_point") != "use-email-verification-handlers" {
		t.Fatalf("signup.confirmEmail form %v", f)
	}
	strs := drawnBy(func() { layoutTop(t, a) })
	if !hasText(strs, "Enter the code we sent you") || !hasText(strs, "me@example.com") {
		t.Fatalf("modal drew %q", strs)
	}

	v := p.verify
	e := &cellsEnv{t: t, v: v, now: time.Now()}
	e.frames(4) // componentDidMount: the first cell takes the focus
	if e.focused() != 0 {
		t.Fatalf("focused cell %d, want 0", e.focused())
	}
	e.typeIn("a")
	if v.cells[0].Text() != "A" || e.focused() != 1 {
		t.Fatalf("after typing a: %q, focus %d", v.code(), e.focused())
	}
	e.typeIn("-") // not a letter or digit
	if v.cells[1].Text() != "" || e.focused() != 1 {
		t.Fatalf("a dash went in: %q", v.code())
	}
	e.r.Queue(key.Event{Name: key.NameDeleteBackward, State: key.Press})
	e.frames(3)
	if e.focused() != 0 {
		t.Fatalf("Backspace in an empty cell: focus %d, want 0", e.focused())
	}
	e.r.Queue(key.Event{Name: key.NameRightArrow, State: key.Press})
	e.frames(3)
	if e.focused() != 1 {
		t.Fatalf("→: focus %d, want 1", e.focused())
	}
	// a paste from the second cell fills the rest and submits
	e.r.Queue(key.Event{Name: "V", Modifiers: key.ModShortcut, State: key.Press})
	e.frames(2)
	if !e.r.ClipboardRequested() {
		t.Fatal("Ctrl+V in a cell didn't ask for the clipboard")
	}
	e.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader("b1-2 cd9")) }})
	e.frames(3)
	waitFor(t, a, "the first check", func() bool { return h.count("signin.confirmCode") == 1 })
	f, _ = h.log.form("signin.confirmCode")
	if f.Get("code") != "AB12CD" || f.Get("email") != "me@example.com" || f.Get("reason") != "verify email modal code confirmation" {
		t.Fatalf("signin.confirmCode form %v", f)
	}
	waitFor(t, a, "invalid code", func() bool { return v.status == codeError })
	e.frames(4)
	if v.code() != "" || e.focused() != 0 {
		t.Fatalf("after an invalid code: %q, focus %d", v.code(), e.focused())
	}
	if !hasText(drawnBy(func() { e.frame() }), "That code wasn't valid. Give it another go!") {
		t.Fatal("no invalid-code message")
	}

	for _, c := range "zz9yx8" {
		e.typeIn(string(c))
	}
	waitFor(t, a, "verified", func() bool { return v.verified })
	f, _ = h.log.form("signin.confirmCode")
	if f.Get("code") != "ZZ9YX8" {
		t.Fatalf("second code %q", f.Get("code"))
	}
	strs = drawnBy(func() { layoutTop(t, a) })
	if !hasText(strs, "Your email has been verified") || !hasText(strs, "You can now manage the Slack Connect invitations you’ve blocked.") {
		t.Fatalf("success drew %q", strs)
	}
	waitFor(t, a, "the blocked list", func() bool { return p.lists.blockDone && len(p.lists.blocked) == 1 })
	rows = pageRows(t, a, p, "privacy_visibility")
	if !hasRow(rows, "list", "spam@example.com · Unblock email") {
		t.Fatalf("after verifying: %s", dumpRows(rows))
	}
}

// VerifyEmailModal's error names (i0DY, wIr1.Mv) and CodeCheckStatus.
func TestPrefsVerifyEmailErrors(t *testing.T) {
	v := &verifyEmail{p: &prefsModal{a: &App{}}, focus: -1}
	for _, c := range []struct {
		code, status string
	}{
		{"failed", codeError}, {"invalid_code", codeError}, {"ratelimited", codeRateLimit},
		{"expired", codeNewCode}, {"ratelimited_blocked", codeBlocked}, {"something_else", codeError},
	} {
		v.cells[0].SetText("A")
		v.confirmed(&slack.Error{Method: "signin.confirmCode", Code: c.code})
		if v.status != c.status || v.code() != "" || v.disabled {
			t.Errorf("%s: status %q code %q disabled %v, want %q, cleared", c.code, v.status, v.code(), v.disabled, c.status)
		}
	}
}
