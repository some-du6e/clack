package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Slick IgnoreTime's whole stylesheet makes the timestamp's label
// unselectable, so a copy of several messages leaves the times out of
// their "Name  3:04 PM" header lines, as the browser's copy does in Slack.
func TestIgnoreTimeLeavesTimesOutOfCopies(t *testing.T) {
	if !timeSelectable() {
		t.Fatal("times aren't copied with no CSS")
	}
	slackc.SetStyle("plugin:IgnoreTime:ignore-time", `.c-timestamp__label { -webkit-user-select: none; user-select: none; }`)
	if timeSelectable() {
		t.Error("IgnoreTime's user-select: none didn't keep the time out of the copy")
	}
	slackc.RemoveStyle("plugin:IgnoreTime:ignore-time")
	if !timeSelectable() {
		t.Error("the time stays out of copies after IgnoreTime stopped")
	}
}
