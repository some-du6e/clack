package messages

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A message's double clicks take Chromium's words (richview's wordRange,
// Gio's ClackWordRange: ICU's word breaks with Chromium's rules): a period
// between letters and a colon end a word, a period between digits doesn't.
// They used to join, by UAX #29's defaults: "fmt.Println", "TODO:fix".
// Synthetic text only.
func TestSelectionWordsLikeChromium(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	m := slack.Message{Type: "message", TS: tsSelClicks, User: "UALICE", Text: "call fmt.Println then TODO:fix at 3.14 now"}
	e := newCtxEnvIn(t, tGen, []slack.Message{m})
	for _, c := range []struct {
		sub  string
		off  int
		want string
	}{{"Println", 2, "Println"}, {"fmt", 1, "fmt"}, {"fix", 2, "fix"} /* past the "fi" ligature, one glyph */, {"TODO", 1, "TODO"}, {"3.14", 0, "3.14"}} {
		e.selEvents(selClicks(e.selAt(c.sub, c.off), 2, 100*time.Millisecond)...)
		if got := e.selected(); got != c.want {
			t.Errorf("a double click in %q selected %q, want %q", c.sub, got, c.want)
		}
	}
}
