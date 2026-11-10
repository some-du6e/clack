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

// liveEl is a message's element host for one revision of its blocks.
type liveEl struct {
	sig uint64
	el  richview.Elements
}

// liveKey is a message of one host.
type liveKey struct {
	h      blockkit.Host
	ch, ts string
}

// liveEls caches liveElements per message (UI goroutine only), so a frame
// doesn't make blockkit copy every visible message's blocks again.
var liveEls = map[liveKey]liveEl{}

// liveElements is m's Block Kit element host: nil when the Host can't run
// blockkit or m has nothing interactive.
func (b *base) liveElements(m *core.Message) richview.Elements {
	h, ok := b.h.(blockkit.Host)
	if !ok || m == nil {
		return nil
	}
	k := liveKey{h: h, ch: m.Channel, ts: m.TS}
	sig := blocksSig(m)
	if e, ok := liveEls[k]; ok && e.sig == sig {
		return e.el
	}
	var el richview.Elements
	if me := blockkit.MessageElements(h, m); me != nil {
		el = me
	}
	if len(liveEls) > 2000 {
		clear(liveEls)
	}
	liveEls[k] = liveEl{sig: sig, el: el}
	return el
}

// blocksSig is FNV-1a over m's blocks and attachments (what the element
// host is made from).
func blocksSig(m *core.Message) uint64 {
	h := uint64(14695981039346656037)
	for _, bs := range [][]byte{m.Blocks, {0}, m.Attachments} {
		for _, c := range bs {
			h ^= uint64(c)
			h *= 1099511628211
		}
	}
	return h
}
