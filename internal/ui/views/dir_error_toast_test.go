package views

import (
	"errors"
	"fmt"
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
)

type dirToastRec struct {
	parts []i18n.Part
	retry func()
	plain string
}

func (r *dirToastRec) Toast(s string) { r.plain = s }
func (r *dirToastRec) ToastRetry(p []i18n.Part, icon string, retry func()) {
	r.parts, r.retry = p, retry
}
func (r *dirToastRec) ToastParts(p []i18n.Part, icon string, c color.NRGBA, d time.Duration) {
	r.parts, r.retry = p, nil
}

// The channel list's join or leave failure is search's ErrorToast:
// "Couldn’t join #x." bold, the blip line and Try again, which runs the
// same action; a leave the org's policy refuses explains that, with no
// Try again. No error text either way.
func TestDirErrorToast(t *testing.T) {
	var r dirToastRec
	again := 0
	dirErrorToast(&r, true, "design", errors.New("core: Join: dial tcp: i/o timeout"), func() { again++ })
	if got := i18n.Plain(r.parts); got != "Couldn’t join #design. This could be a connection issue, or just a little blip.Try again" {
		t.Errorf("join: %q", got)
	}
	if r.parts[0].Tag != "strong" || r.parts[len(r.parts)-1].Tag != "link" || r.retry == nil {
		t.Fatalf("join: %+v, retry %v", r.parts, r.retry != nil)
	}
	r.retry()
	if again != 1 {
		t.Errorf("Try again ran the join %d times", again)
	}
	r = dirToastRec{}
	dirErrorToast(&r, false, "general", fmt.Errorf("core: Leave: %w", &slack.Error{Method: "conversations.leave", Code: "cant_leave_general"}), func() {})
	if got := i18n.Plain(r.parts); got != "Couldn’t leave #general. Your organization requires everyone to be in it, so no one misses important info." || r.retry != nil {
		t.Errorf("policy leave: %q (retry %v)", got, r.retry != nil)
	}
}
