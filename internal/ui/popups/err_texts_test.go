package popups

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// LeaveChannelError says what Slack's does for each code
// conversations.leave answers with; the default is Couldn't leave with
// Retry, and Retry is the default's alone.
func TestLeaveErrorText(t *testing.T) {
	for _, c := range []struct {
		code, title, body string
		steps, retry      bool
		admin, frontline  bool
	}{
		{code: "cant_leave_general", title: "Membership is required in #general", body: "Every workspace has one channel that contains all the members of this workspace — this is that channel for you."},
		{code: "cant_leave_mandatory_shared_channel", title: "Membership is required in #general", body: "Everyone in Fake Org is required to be a member of this channel."},
		{code: "last_member", title: "To leave this channel, it needs to be unshared", body: "Since you’re the last person from Fake Club, you’ll need an admin’s help to leave. It’s just a couple steps:", steps: true},
		{code: "last_member", admin: true, title: "To leave this channel, it needs to be unshared", body: "Since you’re the last person from Fake Club, you’ll need to stop sharing this channel before you can leave it."},
		{code: "user_is_restricted", title: "Couldn’t leave #general", body: "As a guest you can leave private channels, but you’ll need to ask an admin to remove you from any public channels."},
		{code: "user_is_frontline", title: "Couldn’t leave #general", body: ""},
		{code: "user_is_frontline", frontline: true, title: "Couldn’t leave #general", body: "As a frontline member you’ll need to ask an admin to remove you from channels."},
		{code: "user_is_ultra_restricted", title: "Couldn’t leave #general", body: "On a guest account, you need to be a member of at least one channel."},
		{code: "ratelimited", title: "Couldn’t leave #general", body: "Hmm, something went wrong. Try again?", retry: true},
		{code: "UNKNOWN", title: "Couldn’t leave #general", body: "Hmm, something went wrong. Try again?", retry: true},
	} {
		got := leaveErrorText(c.code, "#general", "Fake Club", "Fake Org", c.admin, c.frontline)
		if got.title != c.title || got.body != c.body || (len(got.steps) > 0) != c.steps || got.retry != c.retry {
			t.Errorf("%s (admin %v, frontline %v): %+v", c.code, c.admin, c.frontline, got)
		}
	}
}

// EditMessageErrorDialog's title and text by chat.update's code; any
// failure without one is the default.
func TestEditErrorText(t *testing.T) {
	for _, c := range []struct {
		code        string
		mins        int
		admin       bool
		title, body string
		more        bool
	}{
		{"edit_window_closed", 60, false, "Message editing failed", "Sorry, but messages can only be edited for 1 hour after posting.", false},
		{"edit_window_closed", 1440, true, "Message editing failed", "Sorry, but messages can only be edited for 1 day after posting.", true},
		{"msg_too_long", 0, false, "Message editing failed", "Sorry, your message is too long. Please shorten it and try again.", false},
		{"file_attach_after_send_disabled", 0, false, "Message attachment editing is disabled", "At the request of your administrator, message editing is limited to text only.", false},
		{"blocked_file_type", 0, false, "Upload failed", "According to your organization’s policy, you can’t share this type of file.", false},
		{"", 0, false, "Message editing failed", "Something went wrong with editing your message. Try again in a moment.", false},
	} {
		title, body, more := editErrorText(c.code, c.mins, c.admin)
		if title != c.title || body != c.body || (more != "") != c.more {
			t.Errorf("%q: %q / %q / %q", c.code, title, body, more)
		}
	}
	if got := editWindow(90); got != "90 minutes" {
		t.Errorf("editWindow(90) = %q", got)
	}
}

// APICode is an answer's code only: a connection's error has none.
func TestAPICode(t *testing.T) {
	if got := APICode(fmt.Errorf("core: Leave: %w", &slack.Error{Method: "conversations.leave", Code: "last_member"})); got != "last_member" {
		t.Errorf("wrapped slack.Error: %q", got)
	}
	if got := APICode(errors.New("dial tcp: i/o timeout")); got != "" {
		t.Errorf("a transport error: %q", got)
	}
}

type toastRec struct{ got []string }

func (r *toastRec) Toast(s string) { r.got = append(r.got, s) }

// FailedToast shows Slack's generic text, never the error.
func TestFailedToast(t *testing.T) {
	var r toastRec
	FailedToast(&r, "test: write", errors.New("core: Fetch: file over 64 MB: slack: response too large"))
	if len(r.got) != 1 || r.got[0] != "Something went wrong. Please try again." || strings.Contains(r.got[0], "core:") {
		t.Errorf("toasts %q", r.got)
	}
}

// The add people modal's SUBMIT_ERROR: getInviteErrorText's words for a
// code it knows, plural by the invitees that failed, else its fallback.
func TestInviteFailedText(t *testing.T) {
	for _, c := range []struct {
		code string
		n    int
		want string
	}{
		{"already_in_channel", 1, "User is already in this channel."},
		{"already_in_channel", 3, "Users are already in this channel."},
		{"user_is_restricted", 1, "User is a Multi-Channel Guest. Only your Workspace Admin can add them to a channel."},
		{"restricted_action", 1, "Your organization’s settings prevent you from using Slack Connect to invite people outside of Fake Club."},
		{"too_many_members", 2, "This channel is only allowed a certain number of members, so you can’t add more people right now. Reach out to your admins if you have questions."},
		{"ratelimited", 1, "Couldn’t add 1 person to the channel. Please try again."},
		{"", 4, "Couldn’t add 4 people to the channel. Please try again."},
	} {
		if got := InviteFailedText(c.code, c.n, "Fake Club"); got != c.want {
			t.Errorf("%q ×%d: %q, want %q", c.code, c.n, got, c.want)
		}
	}
}
