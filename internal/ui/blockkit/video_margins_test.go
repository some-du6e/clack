package blockkit

// A video block's spacing is VideoBlock's own (.p-video_block__body{padding:
// 8px 0}), which richview's VideoBlock draws (TestVideoBlockLikeSlack): a
// modal, App Home or a message's surface adds none of its own around it, so
// the player sits 8dp from its neighbours, not 16 (review of expand-more).

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func TestSurfaceVideoBlockMargins(t *testing.T) {
	h, _ := visibleHost()
	for _, typ := range []string{"modal", "home", "code_channel_view"} {
		s := newSurface(h, &core.View{Type: typ})
		for _, first := range []bool{true, false} {
			b := Block{Type: "video"}
			if top, bottom, inner, innerBottom := s.blockMargins(&b, first); top != 0 || bottom != 0 || inner != 0 || innerBottom != 0 {
				t.Errorf("%s (first %v): a video block's margins %d %d, padding %d %d, want none", typ, first, top, bottom, inner, innerBottom)
			}
		}
	}
}
