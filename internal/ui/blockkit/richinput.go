package blockkit

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/theme"
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

// ---- file_input ----
//
// Slack's file_input: a drop zone ("Drag and drop or click to upload"),
// the accepted types and limit under it, and the chosen files as rows
// with a remove ×. Files upload right away (files.getUploadURL, the
// bytes, files.completeUpload; never shared) and the input's
// value is {"files": [Slack file objects]}.

// PickFiles opens the platform file chooser (set by the ui root); accept
// is a comma list of extensions, multiple allows several files.
var PickFiles func(accept string, multiple bool) ([]core.UploadFile, error)

type upFile struct {
	name     string
	raw      json.RawMessage // Slack's file object once uploaded
	sent, of int64
	err      string
	x        tap
}

func (w *ew) maxFiles() int {
	if w.e.MaxFiles > 0 {
		return w.e.MaxFiles
	}
	return 10
}

func (w *ew) fileHeight(gtx C) int {
	return dp(gtx, 72) + dp(gtx, 22) + len(w.files)*dp(gtx, 40)
}

func (w *ew) pickFiles() {
	if PickFiles == nil {
		w.o.host().Toast("No file chooser on this platform")
		return
	}
	h := w.o.host()
	accept := ""
	if len(w.e.FileTypes) > 0 {
		accept = "." + strings.Join(w.e.FileTypes, ",.")
	}
	multiple := w.maxFiles()-len(w.files) > 1
	go func() {
		fs, err := PickFiles(accept, multiple)
		if err != nil || len(fs) == 0 {
			return
		}
		post(h, func() {
			for _, f := range fs {
				if len(w.files) >= w.maxFiles() {
					h.Toast("You can upload up to " + itoa(w.maxFiles()) + " files")
					break
				}
				w.upload(f)
			}
		})
	}()
}

func (w *ew) upload(f core.UploadFile) {
	h := w.o.host()
	u := &upFile{name: f.Name, of: int64(len(f.Data))}
	w.files = append(w.files, u)
	w.o.changed()
	ix := h.Core().Interactive()
	run(h, 5*time.Minute, func(ctx context.Context) (json.RawMessage, error) {
		return ix.UploadForInput(ctx, f, func(sent, total int64) {
			post(h, func() { u.sent, u.of = sent, total })
		})
	}, func(raw json.RawMessage, err error) {
		if err != nil {
			u.err = "Upload failed"
		} else {
			u.raw = raw
		}
		w.syncFiles()
	})
}

// syncFiles puts the uploaded files into the input's value.
func (w *ew) syncFiles() {
	v := w.value()
	v.Files = v.Files[:0:0]
	for _, f := range w.files {
		if f.raw != nil {
			v.Files = append(v.Files, f.raw)
		}
	}
	if s, ok := w.o.(*Surface); ok {
		s.clearErr(w.bid)
	}
	w.o.changed()
}

func (w *ew) layoutFile(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	for i, f := range w.files {
		if f.x.clicked(gtx, h) {
			w.files = append(w.files[:i:i], w.files[i+1:]...)
			w.syncFiles()
			return
		}
	}
	if w.main.clicked(gtx, h) && len(w.files) < w.maxFiles() {
		w.pickFiles()
	}
	zone := image.Pt(sz.X, dp(gtx, 72))
	border := cl.SelectBorder
	if w.errored() {
		border = cl.ErrorText
	} else if w.main.g.Hovered() {
		border = cl.Focus
	}
	outline(gtx, image.Rectangle{Max: zone}, dp(gtx, 8), max(1, dp(gtx, 1)), border, cl.Bg)
	msg := "Drag and drop or click to upload"
	mw := measure(gtx, th, 15, theme.Regular, msg) + dp(gtx, 26)
	at(gtx, image.Pt((zone.X-mw)/2, 0), func(gtx C) D {
		return vcenter(gtx, zone.Y, func(gtx C) D {
			icon(gtx, "upload", 18, cl.Text2)
			at(gtx, image.Pt(dp(gtx, 26), 0), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text2, msg) })
			return D{Size: image.Pt(mw, dp(gtx, 22))}
		})
	})
	w.main.add(gtx, zone)
	y := zone.Y + dp(gtx, 4)
	hint := "Up to " + itoa(w.maxFiles()) + " files"
	if len(w.e.FileTypes) > 0 {
		hint = strings.ToUpper(strings.Join(w.e.FileTypes, ", ")) + " · " + hint
	}
	at(gtx, image.Pt(0, y), func(gtx C) D { return label(gtx, th, 13, theme.Regular, cl.Hint, hint) })
	y += dp(gtx, 18)
	for _, f := range w.files {
		f := f
		at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			row := image.Pt(sz.X, dp(gtx, 36))
			outline(gtx, image.Rectangle{Max: row}, dp(gtx, 8), max(1, dp(gtx, 1)), cl.BtnBorder, cl.Bg)
			at(gtx, image.Pt(dp(gtx, 10), (row.Y-dp(gtx, 18))/2), func(gtx C) D { return icon(gtx, "files", 18, cl.Text2) })
			status := ""
			switch {
			case f.err != "":
				status = f.err
			case f.raw == nil && f.of > 0:
				status = itoa(int(f.sent*100/max(f.of, 1))) + "%"
			}
			at(gtx, image.Pt(dp(gtx, 36), 0), func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(row.X-dp(gtx, 110), row.Y)}
				return vcenter(gtx, row.Y, func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.InputText, f.name) })
			})
			if status != "" {
				col := cl.Text2
				if f.err != "" {
					col = cl.ErrorText
				}
				sw := measure(gtx, th, 13, theme.Regular, status)
				at(gtx, image.Pt(row.X-dp(gtx, 40)-sw, 0), func(gtx C) D {
					return vcenter(gtx, row.Y, func(gtx C) D { return label(gtx, th, 13, theme.Regular, col, status) })
				})
			}
			xs := dp(gtx, 28)
			at(gtx, image.Pt(row.X-dp(gtx, 4)-xs, (row.Y-xs)/2), func(gtx C) D {
				if f.x.g.Hovered() {
					rrect(gtx, image.Rect(0, 0, xs, xs), dp(gtx, 6), cl.Hover)
				}
				at(gtx, image.Pt((xs-dp(gtx, 16))/2, (xs-dp(gtx, 16))/2), func(gtx C) D { return icon(gtx, "close-small", 16, cl.Text2) })
				f.x.add(gtx, image.Pt(xs, xs))
				return D{}
			})
			return D{}
		})
		y += dp(gtx, 40)
	}
}
