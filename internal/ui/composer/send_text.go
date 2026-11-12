package composer

import (
	"context"

	"github.com/some-du6e/clack/internal/core"
)

// SendText sends text the way Slack sends a reply typed into a desktop
// notification (boot-render delegate_replyToNotification): a Delta with
// only that text inserted, through the composer's send (the send
// transforms and the sendMessage thunk), in threadTS's thread when it is
// set.
func SendText(ctx context.Context, a *core.App, ch, threadTS, text string) error {
	d := NewDoc()
	d.Insert(0, text, Attr{})
	if d.Empty() {
		return nil
	}
	return postMessage(ctx, coreAPI{a}, ch, threadTS, d.Mrkdwn(), d.RichTextBlocks(), false)
}
