package kit

// RichTextInput: Taut's api.elements.RichTextInput (app/slack/
// richTextInput.tsx), Slack's WYSIWYG field drawn with the composer's
// rich text editor.

import (
	"encoding/json"
	"image"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

func init() {
	add(richTextInput, "RichTextInput")
}

type richInputState struct {
	ed      *composer.Editor
	lastVer uint64
	length  int
	focused bool
	block   int // input blocker while disabled
	style   composer.EditorStyle
	styleOK bool
	dark    bool
}

// atomResolver names mentions for DocFromBlocksJSON (nil: ids are used).
func atomResolver() composer.AtomResolver {
	if c := envOf().Core; c != nil && c.S != nil {
		return c.S
	}
	return nil
}

// blocksJSON encodes a rich_text blocks prop (initial value) as JSON.
func blocksJSON(v any) json.RawMessage {
	c, ok := jsonClean(v)
	if !ok || c == nil {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	return b
}

// decodeBlocks turns RichTextBlocks() JSON into plain values for JS
// (an empty array for an empty document).
func decodeBlocks(raw json.RawMessage) []any {
	out := []any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = []any{}
		}
	}
	return out
}

// editorStyle is the composer's editor look for th (as composer's own
// editorStyle), with the field's placeholder.
func editorStyle(th *theme.Theme) composer.EditorStyle {
	st := composer.DefaultEditorStyle(th.Shaper)
	p := &th.C
	st.Color = p.Fg
	st.Caret = p.Fg
	st.LinkColor = p.Link
	st.MentionColor = p.Mention
	st.MentionBg = p.MentionBg
	st.BroadcastColor = p.Mention
	st.BroadcastBg = p.SelfMention
	st.CodeColor = p.CodeFg
	st.CodeBg = p.CodeBg
	st.CodeBorder = p.CodeBorder
	st.PreBg = p.PreBg
	st.PreBorder = p.CodeBorder
	st.QuoteBar = p.QuoteBar
	st.Selection = p.Selection
	st.Placeholder = p.FgSubtle
	st.MaxHeight = 300
	st.Images = func(url string, px int) (paint.ImageOp, bool) {
		if c := envOf().Images; c != nil && url != "" {
			return c.Get(url, px)
		}
		return paint.ImageOp{}, false
	}
	return st
}

func richTextInput(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[richInputState](h)
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	disabled := p.Bool("disabled")
	maxLen := p.IntOr("maxLength", 0)
	minLines := p.IntOr("numMinLines", 3)
	if minLines < 1 {
		minLines = 1
	}

	if st.ed == nil {
		st.ed = composer.NewEditor()
		if raw := blocksJSON(p["value"]); len(raw) > 0 {
			if d, err := composer.DocFromBlocksJSON(raw, atomResolver()); err == nil && d != nil {
				st.ed.SetDoc(d)
			}
		}
		st.lastVer = st.ed.Version()
		st.length = utf8.RuneCountInString(st.ed.Doc().Text())
	}
	if !st.styleOK || st.style.Shaper != th.Shaper || st.dark != th.Dark {
		st.style, st.styleOK, st.dark = editorStyle(th), true, th.Dark
	}
	st.style.PlaceholderText = p.StrOr("placeholder", "")

	// Events before Layout: Layout drains whatever is left.
	props := p
	for {
		ev, ok := st.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case composer.EvChange:
			if v := st.ed.Version(); v != st.lastVer {
				st.lastVer = v
				st.length = utf8.RuneCountInString(st.ed.Doc().Text())
				props.Invoke("onChange", decodeBlocks(st.ed.Doc().RichTextBlocks()), st.length)
			}
		case composer.EvSubmit:
			// enterCreatesNewlineOverride: Enter is a newline here.
			if !disabled {
				st.ed.InsertText("\n")
			}
		case composer.EvFocus:
			st.focused = true
		case composer.EvBlur:
			st.focused = false
			props.Invoke("onBlur")
		}
	}

	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	innerW := w - 2*padX
	if innerW < 1 {
		innerW = 1
	}
	minH := dp(gtx, float32(22*minLines))
	eg := gtx
	eg.Constraints = layout.Constraints{Min: image.Pt(innerW, minH), Max: image.Pt(innerW, dp(gtx, 300))}
	call, ed := record(eg, func(gtx layout.Context) layout.Dimensions { return st.ed.Layout(gtx, st.style) })
	ih := ed.Size.Y
	if ih < minH {
		ih = minH
	}
	box := image.Rect(0, 0, w, ih+2*padY)
	rad := dp(gtx, 8)
	over := overLimit(maxLen, st.length)
	switch {
	case disabled:
		ring(gtx, box, rad, 1, cl.InputBorder, cl.Disabled)
	case over:
		ring(gtx, box, rad, 1, cl.Error, cl.Bg)
	case st.focused:
		theme.RRect(gtx, box.Inset(-dp(gtx, 3)), rad+dp(gtx, 3), cl.FocusRing)
		ring(gtx, box, rad, 1, cl.Focus, cl.Bg)
	default:
		ring(gtx, box, rad, 1, cl.InputBorder, cl.Bg)
	}
	callAt(gtx, call, image.Pt(padX, padY))
	if disabled {
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: &st.block, Kinds: pointer.Press | pointer.Release}); !ok {
				break
			}
		}
		bs := clip.Rect(box).Push(gtx.Ops)
		event.Op(gtx.Ops, &st.block)
		bs.Pop()
	}
	total := box.Max
	if over {
		ld := at(gtx, image.Pt(0, total.Y), func(gtx layout.Context) layout.Dimensions { return limitLine(gtx, maxLen, st.length) })
		total.Y += ld.Size.Y
	}
	return layout.Dimensions{Size: total}
}
