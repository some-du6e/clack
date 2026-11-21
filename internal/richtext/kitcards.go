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
	return Block{Kind: CalloutBlock, BlockID: b.BlockID, Tone: tone, Children: c.children(b.ChildBlocks, true)}
}

// containerWidths are ContainerBlock's max-width classes.
var containerWidths = map[string]bool{"narrow": true, "standard": true, "wide": true, "full": true}

// containerBlock converts a container block: the header (its
// rich_text_title drawn inline, else its plain title; the mrkdwn or plain
// subtitle; the icon) and its child blocks.
func (c *builder) containerBlock(b *bk) Block {
	ci := &Container{Collapsible: b.IsCollapsible, DefaultCollapsed: b.IsCollapsible && b.DefaultCollapsed,
		HeaderDivider: b.HasHeaderDivider && !b.IsCollapsible, Key: b.BlockID}
	if ci.Key == "" {
		ci.Key = "#" + strconv.Itoa(c.containers)
	}
	c.containers++
	// width's "standard" is a default parameter: it fills only an absent
	// field; any other value (null, a class Slack lacks) adds no max-width
	// class, so the box is the column's (width:100%)
	_ = json.Unmarshal(b.Width, &ci.Width)
	switch {
	case len(b.Width) == 0:
		ci.Width = "standard"
	case !containerWidths[ci.Width]:
		ci.Width = "full"
	}
	if rt := b.RichTextTitle; rt != nil && rt.Type == "rich_text" {
		ci.Title = flattenSpans(c.richText(rt.Elements))
	} else if t := b.Title; t != nil {
		ci.Title = plainSpans(c.r, t.Text)
	}
	if st := b.Subtitle; st != nil && st.Text != "" {
		ci.Subtitle = c.textSpans(st)
	}
	if len(b.Icon) > 0 && b.Icon[0] == '{' {
		var ic bk
		if json.Unmarshal(b.Icon, &ic) == nil {
			if img := c.imageOf(&ic); img != nil {
				ci.Icon = img.URL
			}
		}
	}
	return Block{Kind: ContainerBlock, BlockID: b.BlockID, Container: ci, Children: c.children(b.ChildBlocks, false)}
}
