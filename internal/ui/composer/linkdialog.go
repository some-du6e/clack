package composer

import (
	"gioui.org/io/event"
	"image"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's "Add link" dialog (Ctrl+Shift+U or the link button): a 520 modal
// with labelled "Text" and "Link" inputs (15/700 labels, 36px inputs with
// radius 8, #7c7a7f border, blue 1px + 4px focus ring) and Cancel / Save.
// Save is disabled while the link is empty; Enter in a field saves.

type crLinkDlg struct {
	h              Host
	text, link     widget.Editor
	modal          *popups.Modal
	focused, saved bool
	onSave         func(text, url string)
	text0, url0    string        // the prefill (didTextChange, props)
	saveJS         slackc.JSFunc // a plugin's saveLink override
}

// openLinkDialog shows the dialog. text and url prefill the fields (url
// when editing an existing link); onSave gets the text and the normalized
// URL, onCancel runs when the dialog closes without saving.
func openLinkDialog(h Host, text, url string, onSave func(text, url string), onCancel func()) {
	d := &crLinkDlg{h: h, onSave: onSave, text0: text, url0: url}
	d.text = widget.Editor{SingleLine: true, Submit: true}
	d.link = widget.Editor{SingleLine: true, Submit: true}
	if text != "" {
		d.text.SetText(text)
		n := d.text.Len()
		d.text.SetCaret(n, n)
	}
	if url != "" {
		d.link.SetText(url)
		n := d.link.Len()
		d.link.SetCaret(n, n)
	}
	d.modal = popups.NewModal(h, popups.ModalOpts{
		Title:        "Add link",
		Body:         d.slackBody,
		PadX:         24, // Slack's link dialog: 24px padding, 68px header → 472px fields
		HeadH:        68,
		EnterSubmits: true,
		FocusStops:   func() []event.Tag { return []event.Tag{&d.text, &d.link} },
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			{Label: "Save", Kind: popups.BtnPrimary, Enabled: d.canSave, Do: func() bool { d.save(); return true }},
		},
		OnClose: func() {
			if !d.saved && onCancel != nil {
				onCancel()
			}
		},
	})
	h.ShowOverlay(d.modal)
}

func (d *crLinkDlg) canSave() bool { return strings.TrimSpace(d.link.Text()) != "" }

// save goes through saveLink({didTextChange, text, url}), Slack's
// ComposerLinkModal method: a plugin's override when one came back in
// the props, else the composer's own, which runs LinkTransforms.
func (d *crLinkDlg) save() {
	if d.saved || !d.canSave() {
		return
	}
	d.saved = true
	text, url := d.text.Text(), crNormalizeURL(d.link.Text())
	if d.saveJS.ID != 0 {
		d.saveJS.Call(slackc.Props{"didTextChange": text != d.text0, "text": text, "url": url})
		return
	}
	d.saveLink(text, url)
}

func (d *crLinkDlg) saveLink(text, url string) {
	d.saved = true
	if d.onSave != nil {
		linkSource = "link-dialog" // addLinkTransform's ctx.source
		u := transformLink(url)
		linkSource = "paste"
		d.onSave(text, u)
	}
}

// ComposerLinkModal (row 43). The props carry the prefill and the
// dialog's methods; saveLink({didTextChange, text, url}) saves.
func (d *crLinkDlg) slackBody(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "ComposerLinkModal", d, (*crLinkDlg).props, (*crLinkDlg).layoutBody)
}

func (d *crLinkDlg) props() slackc.Props {
	return slackc.Props{
		"url":       d.url0,
		"text":      d.text0,
		"isEditing": d.url0 != "",
		"saveLink": func(a slackc.Props) {
			d.saveLink(a.StrOr("text", d.text.Text()), crNormalizeURL(a.StrOr("url", d.link.Text())))
		},
		"onSave":  func() { d.save(); d.modal.Dismiss() },
		"onClose": func() { d.modal.Dismiss() },
	}
}

func (d *crLinkDlg) layoutBody(gtx layout.Context, p slackc.Props) layout.Dimensions {
	d.saveJS = slackc.JSFunc{}
	if p != nil {
		if f, ok := p["saveLink"].(slackc.JSFunc); ok {
			d.saveJS = f
		}
	}
	return d.body(gtx)
}

// body draws the two fields. The modal calls it twice a frame (measure,
// then draw); events are handled in whichever pass comes first.
func (d *crLinkDlg) body(gtx layout.Context) layout.Dimensions {
	if !d.focused {
		d.focused = true
		tag := &d.text
		if d.text.Len() > 0 {
			tag = &d.link
		}
		gtx.Execute(key.FocusCmd{Tag: tag})
	}
	for _, ed := range [...]*widget.Editor{&d.text, &d.link} {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); !ok {
				continue
			}
			switch {
			case d.canSave():
				d.save()
				d.modal.Dismiss()
			case ed == &d.text:
				gtx.Execute(key.FocusCmd{Tag: &d.link})
			}
		}
	}
	th := d.h.Theme()
	w := gtx.Constraints.Max.X
	y := 0
	for _, f := range [...]struct {
		label string
		ed    *widget.Editor
	}{{"Text", &d.text}, {"Link", &d.link}} {
		crText(gtx, th.Shaper, 0, y, w, textStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg, MaxLines: 1}, f.label)
		y += gtx.Dp(22 + 8)
		t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		crLinkField(gtx, th, f.ed, w)
		t.Pop()
		y += gtx.Dp(36 + 20)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// crLinkField draws a Slack c-input_text: 36 tall, radius 8, padding 4 12.
func crLinkField(gtx layout.Context, th *theme.Theme, ed *widget.Editor, w int) {
	hh := gtx.Dp(36)
	r := image.Rect(0, 0, w, hh)
	rad := gtx.Dp(8)
	bg := th.C.BgRaised
	if gtx.Focused(ed) {
		// border transparent + 0 0 0 1px #1264a3, 0 0 0 5px rgba(18,100,163,.3)
		halo := gtx.Dp(5)
		ring := max(gtx.Dp(1), 1)
		fillRRect(gtx, r.Inset(-halo), rad+halo, crFade(th.C.Link, bg, .3))
		fillRRect(gtx, r.Inset(-ring), rad+ring, th.C.Link)
		fillRRect(gtx, r, rad, bg)
	} else {
		border := rgb(0x7c7a7f)
		if th.Dark {
			border = th.C.BorderStrong
		}
		b := max(gtx.Dp(1), 1)
		fillRRect(gtx, r, rad, border)
		fillRRect(gtx, r.Inset(b), max(rad-b, 0), bg)
	}
	padX := gtx.Dp(12)
	tw := max(w-2*padX, 1)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, hh)}
	m := op.Record(gtx.Ops)
	dims := th.Editor(g, ed, 15, th.C.Fg, th.C.Selection)
	call := m.Stop()
	t := op.Offset(image.Pt(padX, (hh-dims.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	// The whole field takes text-cursor clicks; a press on the padding
	// focuses the editor too (it sits under the editor's own area).
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
}

// crNormalizeURL trims s and adds https:// when it has no scheme
// ("example.com", "localhost:8080/x"); mailto: and tel: are kept.
func crNormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		return s
	}
	l := strings.ToLower(s)
	for _, p := range [...]string{"mailto:", "tel:", "sms:"} {
		if strings.HasPrefix(l, p) {
			return s
		}
	}
	return "https://" + strings.TrimLeft(s, "/")
}
