package richtext

// Block Kit's boxed blocks, as Slack's web client registers them for
// messages (build 846030e1ce97, boot-shared): alert (MyNk
// AlertRenderable: Slack Kit's Alert, client-boot MlDl), callout (zMv1,
// Xhau Callout) and loading (KSQB: a jumbo blue InfiniteSpinner).

import (
	"encoding/json"
	"strconv"
)

// bkLevel is a block's "level": a header's number (1–4) or an alert's
// severity; either form decodes without failing the block.
type bkLevel struct {
	N int
	S string
}

func (l *bkLevel) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &l.S); err != nil {
			return err
		}
		l.N, _ = strconv.Atoi(l.S)
		return nil
	}
	var f float64
	if json.Unmarshal(b, &f) == nil {
		l.N = int(f)
	}
	return nil
}

// alertLevels are the levels Alert draws (any other is "default").
var alertLevels = map[string]bool{"default": true, "info": true, "success": true, "warning": true, "error": true}

// alertBlock converts an alert block: Alert with the block's level
// (default when unset), its text slot drawn by the text object's
// renderable (mrkdwn or plain_text).
func (c *builder) alertBlock(b *bk) Block {
	level := b.Level.S
	if !alertLevels[level] {
		level = "default"
	}
	blk := Block{Kind: AlertBlock, BlockID: b.BlockID, Tone: level}
	if t := b.textObj(); t != nil {
		blk.Children = c.textBlocks(t)
	}
	return blk
}

// calloutColors are Callout's background classes; any other colour is
// green.
var calloutColors = map[string]bool{"indigo": true, "blue": true, "jade": true, "pink": true, "gray": true,
	"purple": true, "orange": true, "brown": true, "green": true}

// calloutBlock converts a callout block: its child blocks inside the
// tinted box (renderBlocks with the callout as their parent).
func (c *builder) calloutBlock(b *bk) Block {
	tone := b.BackgroundColor
	if !calloutColors[tone] {
		tone = "green"
	}
	return Block{Kind: CalloutBlock, BlockID: b.BlockID, Tone: tone, Children: c.children(b.ChildBlocks)}
}
