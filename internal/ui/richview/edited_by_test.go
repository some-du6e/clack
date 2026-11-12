package richview

import "testing"

// EditedLabel's tooltip with an editedByUserId (Y5SQ): the message_kit
// string "{dateTime} by {name}".
func TestEditedByTip(t *testing.T) {
	if got := editedByTip("Today at 9:12 AM", "ben"); got != "Today at 9:12 AM by ben" {
		t.Errorf("%q", got)
	}
}
