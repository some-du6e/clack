package views

// Block Kit in the views. Slack keeps a message's interactive elements
// live in its all_threads (Threads), all_unreads (Unreads) and
// starred_message (Later) containers and sends blocks.actions from them
// as the channel does. It disables them only in delete_dialog,
// share_dialog, search, app_actions, shared_message, activity,
// expired_agent_message and composer_preview (modern.vendor Nc1H.DD, the
// elements' isDisabled). So msgBody hands richview the channel row's
// element host (blockkit.MessageElements), and Activity's bodies
// (msgBodyMuted) get none.

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// liveElements is m's Block Kit element host: nil when the Host can't run
// blockkit or m has nothing interactive.
func (b *base) liveElements(m *core.Message) richview.Elements {
	h, ok := b.h.(blockkit.Host)
	if !ok || m == nil {
		return nil
	}
	if el := blockkit.MessageElements(h, m); el != nil {
		return el
	}
	return nil
}
