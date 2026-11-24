package popups

// The Block Kit writes in flight (richview's writes.go): a share_prompt's
// Send, and on the cards that have them an event's RSVP and Copy to Your
// Calendar. Slack's buttons disable themselves while their call is out, so
// a second click, or another of the card's buttons, does nothing until it
// settles; richview draws them so (richview.Busy).

import (
	"sync"

	"github.com/some-du6e/clack/internal/ui/richview"
)

// writes maps a card's write key to the act that sent it.
var writes sync.Map

func init() {
	richview.Busy = func(key string) (string, bool) {
		v, ok := writes.Load(key)
		act, _ := v.(string)
		return act, ok
	}
}

// beginWrite marks key's write in flight, sent by act; false when one
// already is (the click drops, as on Slack's disabled button).
func beginWrite(key, act string) bool {
	_, busy := writes.LoadOrStore(key, act)
	return !busy
}

// endWrite clears key's write once its call settled.
func endWrite(key string) { writes.Delete(key) }
