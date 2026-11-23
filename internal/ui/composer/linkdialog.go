package composer

import (
	"encoding/json"
	"gioui.org/io/event"
	"image"
	"strings"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"

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

// Slack's "Add link" dialog, ComposerLinkModal (boot-shared jRNC, build
// 83aff18a1847): a 520 modal with labelled "Text" and "Link" inputs
// (15/700 labels, 36px inputs with radius 8, #7c7a7f border, blue 1px +
// 4px focus ring) and Cancel / Save. Its _FormDialog (qph5) disables Save
// until a field changed (goButtonIsDisabled: !inputIsDirty) and while the
// link is javascript: or about: (isLinkUrlValid); Enter in a field
// submits the form whenever the link is valid, changed or not. onSubmit:
//
//   - an empty Link (normalizeLinkUrl) with a new text makes the range that
//     text, plain (insertPlainText); otherwise it takes the link off
//     (removeLink), a slug being edited back to its label;
//   - else the text (the URL when it's blank) is linked to the normalized
//     URL (saveLink): in place when the text didn't change, as new text
//     when it did.
//
// The Text field starts empty for a link whose text is its URL (an
// autolink, a slug being edited), and the focus goes to Link when the
// link has text, to Text when it has none.

// linkForm is what openTextyLinkFormModal hands ComposerLinkModal.
type linkForm struct {
	lo, hi      int    // range
	text, url   string // initialText, initialUrl
	editable    bool   // textIsEditable
	editingSlug bool   // isEditingSlug
	preview     *richview.View
	// alive, set by the composer, reports that the range is still there
	// (edRange): the dialog closes, with nothing saved, once it isn't
	alive func() bool
}

// linkResult is how the dialog ended: what onSubmit did.
type linkResult struct {
	kind          linkResultKind
	text, url     string
	didTextChange bool
}

type linkResultKind uint8

const (
	linkSaved   linkResultKind = iota // saveLink
	linkPlain                         // insertPlainText
	linkRemoved                       // removeLink
)

type crLinkDlg struct {
	h          Host
	text, link widget.Editor
	modal      *popups.Modal
	focused    bool
	saved      bool
	form       linkForm
	onDone     func(linkResult)
	text0      string // initialText
	url0       string // initialUrl
	dirty      bool   // inputIsDirty
	lastText   string // the fields as onChange last saw them
	lastURL    string
	urlOK      bool // isValid of lastURL
	saveBtn    *popups.ModalButton
	saveJS     slackc.JSFunc // a plugin's saveLink override
	// preview is ComposerLinkModal's textIsEditable false: the selection
	// holds an emoji or a mention (an embed, "\uE000" in its text), or a
	// line break when making a new link, so the Text field is the
	// selection's rich text (initialBlocks' first block, Xjhw RichText) in
	// "margin_bottom_125 half_opacity" under the Text label, and Save
	// links the selection where it is.
	preview *richview.View
}

// openLinkDialog shows the dialog for f; done gets how it was submitted,
// onCancel runs when it closes without being.
func openLinkDialog(h Host, f linkForm, done func(linkResult), onCancel func()) {
	d := &crLinkDlg{h: h, form: f, onDone: done, text0: f.text, url0: f.url, preview: f.preview}
	d.text = widget.Editor{SingleLine: true, Submit: true}
	d.link = widget.Editor{SingleLine: true, Submit: true}
	// state.text: the initial text unless it's the URL of an editable link
	if t := f.text; t != "" && (!f.editable || f.text != f.url) {
		d.text.SetText(t)
		n := d.text.Len()
		d.text.SetCaret(n, n)
	}
	if f.url != "" {
		d.link.SetText(f.url)
		n := d.link.Len()
		d.link.SetCaret(n, n)
	}
	d.lastText, d.lastURL = d.text.Text(), d.link.Text()
	d.saveBtn = &popups.ModalButton{Label: messageInputNS.T("Save"), Kind: popups.BtnPrimary, Enabled: d.canSave, Do: func() bool { d.save(); return true }}
	d.urlOK = isLinkURLValid(d.lastURL)
	// ComposerLinkModal (+Hdp, the message-input I18n): initialUrl ? "Edit
	// link" : "Add link"
	title := messageInputNS.T("Add link")
	if f.url != "" {
		title = messageInputNS.T("Edit link")
	}
	d.modal = popups.NewModal(h, popups.ModalOpts{
		Title:        title,
		Body:         d.slackBody,
		PadX:         24, // Slack's link dialog: 24px padding, 68px header → 472px fields
		HeadH:        68,
		EnterSubmits: true,
		FocusStops: func() []event.Tag {
			if d.preview != nil {
				return []event.Tag{&d.link}
			}
			return []event.Tag{&d.text, &d.link}
		},
		Buttons: []*popups.ModalButton{
			{Label: messageInputNS.T("Cancel"), Kind: popups.BtnOutline},
			d.saveBtn,
		},
		OnClose: func() {
			if !d.saved && onCancel != nil {
				onCancel()
			}
		},
	})
	if linkDialogTap != nil {
		linkDialogTap(d)
	}
	h.ShowOverlay(d.modal)
}

// linkDialogTap sees each dialog as it opens (tests).
var linkDialogTap func(*crLinkDlg)

// valid is isValid: no javascript: or about: link.
func (d *crLinkDlg) valid() bool { return isLinkURLValid(d.link.Text()) }

// canSave is the Save button: valid, and a field changed (as onChange
// last left them: the button asks every frame).
func (d *crLinkDlg) canSave() bool { return d.urlOK && d.dirty }

// noteEdits is onChange, run on a field's ChangeEvent: inputIsDirty is
// whether either now differs from the initial text and URL
// (hasUnsavedEdits), and isValid is the URL's.
func (d *crLinkDlg) noteEdits() {
	if t, u := d.text.Text(), d.link.Text(); t != d.lastText || u != d.lastURL {
		d.lastText, d.lastURL = t, u
		d.dirty = t != d.text0 || u != d.url0
		d.urlOK = isLinkURLValid(u)
	}
}

// linkTextBlank is /^\s+$/.
func linkTextBlank(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !jsSpace(r) })
}

// save is _FormDialog.save, onGo: onSubmit when the link is valid.
func (d *crLinkDlg) save() {
	if d.saved || !d.valid() {
		return
	}
	d.saved = true
	d.submit(d.text.Text(), d.link.Text())
}

// submit is ComposerLinkModal.onSubmit over the fields' text and url.
func (d *crLinkDlg) submit(text, rawURL string) {
	f := d.form
	l := normalizeLinkURL(rawURL)
	if d.preview != nil {
		text = d.text0 // the text can't change: c === e, didTextChange false
	}
	if l == "" {
		was := d.text0
		if f.editable && d.text0 == d.url0 {
			was = ""
		}
		if text != "" && !linkTextBlank(text) && text != was {
			d.done(linkResult{kind: linkPlain, text: text})
		} else {
			d.done(linkResult{kind: linkRemoved})
		}
		return
	}
	o := text
	if text == "" || linkTextBlank(text) {
		o = rawURL
	}
	t := o
	if f.editingSlug && o == rawURL {
		t = l
	}
	d.saveLink(t, l)
}

// saveLink goes through saveLink({didTextChange, text, url}), Slack's
// ComposerLinkModal method: a plugin's override when one came back in
// the props, else the composer's own, which runs LinkTransforms.
func (d *crLinkDlg) saveLink(text, url string) {
	d.saved = true
	if d.saveJS.ID != 0 {
		d.saveJS.Call(slackc.Props{"didTextChange": text != d.text0, "text": text, "url": url})
		return
	}
	d.saveLinkNative(text, url)
}

// saveLinkNative is the composer's own saveLink, the one ComposerLinkModal's
// props hand a plugin: a wrapper that calls it falls through to the save,
// not back into itself.
func (d *crLinkDlg) saveLinkNative(text, url string) {
	d.saved = true
	linkSource = "link-dialog" // addLinkTransform's ctx.source
	u := transformLink(url)
	linkSource = "paste"
	d.done(linkResult{kind: linkSaved, text: text, url: u, didTextChange: text != d.text0})
}

func (d *crLinkDlg) done(r linkResult) {
	d.saved = true
	if d.onDone != nil {
		d.onDone(r)
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
		// textIsEditable (ComposerLinkModal's prop)
		"textIsEditable": d.preview == nil,
		"saveLink": func(a slackc.Props) {
			d.saveLinkNative(a.StrOr("text", d.text.Text()), normalizeLinkURL(a.StrOr("url", d.link.Text())))
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
// then draw); events are handled in whichever pass comes first. The first
// focus waits for a pass that delivers input: under a plugin patching
// ComposerLinkModal slackc measures this native with gtx.Disabled() first,
// where the FocusCmd went nowhere and neither field ever got the caret.
func (d *crLinkDlg) body(gtx layout.Context) layout.Dimensions {
	if f := d.form.alive; f != nil && !d.saved && !f() {
		d.modal.Dismiss() // its target went away: nothing to save into (onCancel refocuses the editor)
		return layout.Dimensions{}
	}
	if !d.focused && gtx.Source.Enabled() {
		d.focused = true
		tag := &d.text // focusTextOnMount: no initial text
		if d.text0 != "" {
			tag = &d.link // focusUrlOnMount
		}
		gtx.Execute(key.FocusCmd{Tag: tag})
	}
	for _, ed := range [...]*widget.Editor{&d.text, &d.link} {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.ChangeEvent); ok {
				d.noteEdits()
				continue
			}
			if _, ok := ev.(widget.SubmitEvent); !ok {
				continue
			}
			// the form's submit (Enter in either field): _FormDialog.save,
			// which asks isValid only
			if d.valid() {
				d.save()
				d.modal.Dismiss()
			}
		}
	}
	th := d.h.Theme()
	w := gtx.Constraints.Max.X
	y := 0
	for _, f := range [...]struct {
		label string
		ed    *widget.Editor
	}{{messageInputNS.T("Text"), &d.text}, {messageInputNS.T("Link"), &d.link}} {
		crText(gtx, th.Shaper, 0, y, w, textStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg, MaxLines: 1}, f.label)
		y += gtx.Dp(22 + 8)
		if f.ed == &d.text && d.preview != nil {
			y += d.layoutPreview(gtx, y, w)
			continue
		}
		t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		crLinkField(gtx, th, f.ed, w)
		t.Pop()
		y += gtx.Dp(36 + 20)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// layoutPreview draws the selection's rich text at y, at half opacity
// (.half_opacity: opacity .5, an sRGB blend; Gio's layers blend in
// linear light, so the layer's alpha is LinearAlpha's), and returns the
// height it takes with margin_bottom_125 (1.25rem, 20px).
func (d *crLinkDlg) layoutPreview(gtx layout.Context, y, w int) int {
	th := d.h.Theme()
	st := warningStyle(th, d.h.Images())
	bg := popups.BasePryOf(th)
	alpha := float32(theme.LinearAlpha(theme.WithAlpha(th.C.Fg, 0x80), bg).A) / 255
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
	m := op.Record(gtx.Ops)
	dims := d.preview.Layout(g, st, warningLinks{})
	call := m.Stop()
	t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	o := paint.PushOpacity(gtx.Ops, alpha)
	call.Add(gtx.Ops)
	o.Pop()
	t.Pop()
	return dims.Size.Y + gtx.Dp(20)
}

// linkPreview builds the dialog's rich preview of d (the selection) the
// way initialBlocks is made: the selection's delta as message blocks,
// the first rich_text block drawn by RichText.
func linkPreview(r richtext.Resolver, d *Doc) *richview.View {
	raw := d.RichTextBlocks()
	var bs []json.RawMessage
	if json.Unmarshal(raw, &bs) == nil && len(bs) > 1 {
		raw, _ = json.Marshal(bs[:1]) // t.blocks[0]
	}
	if r == nil {
		r = linkPreviewNames{}
	}
	doc := richtext.FromMessage(r, &slack.Message{Type: "message", Blocks: raw})
	return richview.New(doc, richview.Options{})
}

// linkPreviewNames resolves nothing (no workspace): ids stay as they are.
type linkPreviewNames struct{}

func (linkPreviewNames) UserName(id string) string                { return id }
func (linkPreviewNames) ChannelName(id string) string             { return id }
func (linkPreviewNames) UsergroupName(id string) string           { return id }
func (linkPreviewNames) Emoji(string) (url, text string, ok bool) { return "", "", false }
func (linkPreviewNames) SelfID() string                           { return "" }

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
