package composer

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
)

// nilTextyHost has no core: mentions show their ids, no emoji resolve.
type nilTextyHost struct{}

func (nilTextyHost) Core() *core.App         { return nil }
func (nilTextyHost) Images() *imgcache.Cache { return nil }

// newTestTexty is a topic input (maxLength 250) over text.
func newTestTexty(text string) *TextyInput { return NewTextyInput(nilTextyHost{}, text, "", 250) }

// textyOver is the input's TextyLengthWarning count.
func textyOver(t *TextyInput) int { return t.OverLimit() }
