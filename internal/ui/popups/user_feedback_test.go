package popups

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// userFeedbackModal returns the last UserFeedbackModal the host showed.
func userFeedbackModal(h *tzHost) *UserFeedbackModal {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.shown) - 1; i >= 0; i-- {
		if m, ok := h.shown[i].(*UserFeedbackModal); ok {
			return m
		}
	}
	return nil
}

// TestUserFeedbackModal is /feedback's dialog as VhHZ has it: the select
// first; under Share product feedback the area, the feedback with its
// note and the box; Submit only for an area and a feedback that aren't
// only spaces, within 50 and 1,000 JavaScript characters, with the box
// ticked; feedback.submitUserFeedback with product_area, product_feedback
// and source slash_feedback, then the confirmation in its place.
func TestUserFeedbackModal(t *testing.T) {
	e := srNewEnv(t, false)
	e.h.a.SetExperimentGroup(ExpSlashFeedbackModalWarning, "on")
	m := NewUserFeedbackModal(e.h)
	if userFeedbackModal(e.h) != m {
		t.Fatal("the dialog didn't show")
	}
	d := &srDrv{size: image.Pt(1280, 800), w: m}
	x := d.scan()
	for _, want := range []string{ufTitle, ufAsk, ufPick, "Cancel", "Submit"} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	for _, not := range []string{ufVisit, ufAreaHint} {
		if srHas(x, not) {
			t.Errorf("%q drawn before an option is picked", not)
		}
	}
	if m.CanSubmit() {
		t.Error("Submit enabled with nothing filled in")
	}
	m.Pick(0)
	x = d.scan()
	for _, want := range []string{ufShare, ufAreaHint, ufText} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	// the long ones may wrap
	for _, want := range []string{"What area of the product", "Note: Please don’t include", "How does this impact",
		"I understand that this feedback goes"} {
		if !srHasPrefix(x, want) {
			t.Errorf("%q… not drawn: %q", want, x)
		}
	}
	for _, tc := range []struct {
		area, text string
		ack, ok    bool
	}{
		{"huddles", "The mute key is hard to find.", false, false},
		{"   ", "The mute key is hard to find.", true, false},
		{"huddles", " \n ", true, false},
		{strings.Repeat("a", 51), "x", true, false},
		{strings.Repeat("\U0001F600", 26), "x", true, false}, // 52 UTF-16 units
		{"huddles", strings.Repeat("b", 1001), true, false},
		{strings.Repeat("a", 50), strings.Repeat("b", 1000), true, true},
	} {
		m.Fill(tc.area, tc.text, tc.ack)
		if m.CanSubmit() != tc.ok {
			t.Errorf("area %d, feedback %d, box %v: Submit %v, want %v", len(tc.area), len(tc.text), tc.ack, m.CanSubmit(), tc.ok)
		}
	}
	// past the limit the inputs' Alert says so
	m.Fill(strings.Repeat("a", 51), "x", true)
	if x := d.scan(); !srHas(x, "You can’t enter more than 50 characters.") {
		t.Errorf("no limit alert: %q", x)
	}
	m.Fill("huddles", "The mute key is hard to find.", true)
	d.frame()
	m.Submit()
	slUntil(t, 10*time.Second, "the submission", d.frame, func() bool {
		return len(e.fs.SearchForms("feedback.submitUserFeedback")) == 1 && m.Dismissed()
	})
	f := e.fs.SearchForms("feedback.submitUserFeedback")[0]
	for k, v := range map[string]string{"product_area": "huddles", "product_feedback": "The mute key is hard to find.", "source": "slash_feedback"} {
		if f.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v)
		}
	}
	var thanks *Modal
	e.h.mu.Lock()
	if mm, ok := e.h.shown[len(e.h.shown)-1].(*Modal); ok && !mm.Dismissed() {
		thanks = mm
	}
	e.h.mu.Unlock()
	if thanks == nil {
		t.Fatal("no confirmation")
	}
	x = (&srDrv{size: image.Pt(1280, 800), w: thanks}).scan()
	if !srHas(x, "Close") || !srHasPrefix(x, "Your feedback goes to our product teams") || !srHasPrefix(x, "Thank you for helping us") {
		t.Errorf("confirmation %q", x)
	}
}

// TestUserFeedbackModalFailures: a rate limit is Slack's rate limit text,
// another failure its generic one, and the dialog stays with what was
// typed.
func TestUserFeedbackModalFailures(t *testing.T) {
	e := srNewEnv(t, false)
	m := NewUserFeedbackModal(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: m}
	m.Pick(0)
	m.Fill("canvas", "Tables should paste.", true)
	for i, tc := range []struct{ fault, want string }{
		{"ratelimited", ufLimited},
		{"invalid_arguments", ufFailed},
	} {
		// a new request each time: the client answers an identical retry
		// of a failed call with that failure (its backoff)
		m.Fill("canvas", "Tables should paste. "+strings.Repeat("Again. ", i), true)
		e.fs.SetFault("feedback.submitUserFeedback", fakeslack.Fault{Error: tc.fault})
		m.Submit()
		slUntil(t, 20*time.Second, tc.fault, d.frame, func() bool { return !m.busy && m.Err() == tc.want })
		if m.Dismissed() || !m.CanSubmit() {
			t.Errorf("%s: dismissed %v, Submit %v", tc.fault, m.Dismissed(), m.CanSubmit())
		}
		if x := d.scan(); !srHas(x, tc.want) {
			t.Errorf("%s: the Alert not drawn: %q", tc.fault, x)
		}
	}
}

// TestUserFeedbackHelpOption: Get help or support is the help text with
// the Help Center link (https://{team domain}/help) and Visit Help Center
// in place of Submit.
func TestUserFeedbackHelpOption(t *testing.T) {
	e := srNewEnv(t, false)
	m := NewUserFeedbackModal(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: m}
	m.Pick(1)
	x := d.scan()
	for _, want := range []string{ufHelp, ufVisit, "Cancel", "Find", "Help", "Center"} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	if !srHasPrefix(x, "We’re here to help") || srHas(x, "Submit") || srHas(x, ufAreaHint) {
		t.Errorf("help option drawn %q", x)
	}
	if want := strings.TrimSuffix(e.h.a.TeamURL(), "/") + "/help"; m.helpURL() != want || !strings.HasPrefix(want, "http") {
		t.Errorf("help link %q, want %q", m.helpURL(), want)
	}
}
