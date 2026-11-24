package blockkit

import (
	"encoding/json"
	"image"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/composer"
)

// ---- rich_text_input ----
//
// Slack's rich_text_input (RichTextInputElement, the lazy chunk
// sui-block-rich-text-input) is the message composer's box: Texty with
// useWysiwyg inside a WysiwygContainer with enableEmojiButton and
// enableComposerButton, so it has the formatting bar (shown or hidden
// with Aa and the user's msg_input_formatting_bar_state, like the message
// composer's), the markdown and keyboard shortcuts, members, channels and
// emoji completion, and the emoji and Aa buttons in its footer; at most 8
// lines (numMaxLines) before it scrolls; placeholder.text or "Write
// something". Enter adds a line (enterCreatesNewlineOverride). Its value
// is one rich_text block (rich_text_value), dispatched on each change
// with on_character_entered.

func (w *ew) richField() *composer.Composer {
	if w.rc != nil {
		return w.rc
	}
	v := w.value()
	init := v.Rich
	if len(init) == 0 && len(w.e.InitialValue) > 0 && w.e.InitialValue[0] == '{' {
		init = w.e.InitialValue
	}
	o := composer.FieldOpts{Placeholder: textOf(w.e.Placeholder), FocusOnLoad: w.e.FocusOnLoad}
	if len(init) > 0 {
		if c := w.o.host().Core(); c != nil {
			if d, err := composer.DocFromBlocksJSON(json.RawMessage("["+string(init)+"]"), c.S); err == nil && d != nil {
				o.Initial = d
			}
		}
	}
	o.OnChange = func() { w.richChanged() }
	w.rc = composer.NewField(w.o.host(), o)
	return w.rc
}

// richChanged is onTextChange: the value is the field's rich_text, and
// on_character_entered dispatches it.
func (w *ew) richChanged() {
	c, v := w.rc, w.value()
	if c.FieldEmpty() {
		v.Rich, v.Text = nil, ""
	} else {
		v.Rich, v.Text = mergeRichText(c.FieldDoc().RichTextBlocks()), ""
	}
	if s, ok := w.o.(*Surface); ok {
		s.clearErr(w.bid)
	}
	if w.dispatchOn("on_character_entered") {
		w.send(actionPayload(w.bid, w.e, v))
	}
	w.o.changed()
}

// richHeight is the field's height at maxW, from a measuring pass (no
// input) of the composer, so the input block's height is the box's.
func (w *ew) richHeight(gtx C, maxW int) int {
	c := w.richField()
	m := op.Record(gtx.Ops)
	g := gtx.Disabled()
	g.Constraints = layout.Constraints{Min: image.Pt(maxW, 0), Max: image.Pt(maxW, 1<<20)}
	d := c.Layout(g)
	m.Stop()
	return d.Size.Y
}

func (w *ew) layoutRich(gtx C, sz image.Point) {
	c := w.richField()
	c.Invalid = w.errored() // WysiwygContainer isInvalid: the error border
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X, max(sz.Y, 1<<20))}
	c.Layout(g)
	w.focusedD, w.box = c.Focused(), sz
}

// mergeRichText turns the composer's blocks (rich_text runs plus header
// and divider blocks) into the one rich_text block an input's value is.
func mergeRichText(raw json.RawMessage) json.RawMessage {
	var bs []struct {
		Type     string            `json:"type"`
		Elements []json.RawMessage `json:"elements"`
		Text     *struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	els := []json.RawMessage{}
	for _, b := range bs {
		switch b.Type {
		case "rich_text":
			els = append(els, b.Elements...)
		case "header":
			if b.Text != nil {
				sec, _ := json.Marshal(map[string]any{"type": "rich_text_section", "elements": []any{
					map[string]any{"type": "text", "text": b.Text.Text, "style": map[string]bool{"bold": true}}}})
				els = append(els, sec)
			}
		}
	}
	out, _ := json.Marshal(map[string]any{"type": "rich_text", "elements": els})
	return out
}
