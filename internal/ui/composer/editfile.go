package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's EditFileDialog (boot-shared e4hN) for files attached to the
// composer, from its two entry points:
//
//   - the pending file viewer's "Edit file details" (W9QH FileViewer with
//     isPendingView, dialogEntrypoint media_viewer_button): an attached
//     image clicked in the composer opens the viewer;
//   - the alt text speed bump (6Lt3 maybeOpenEditFileDialogForImages,
//     alt_text_speedbump): with alt text reminders on (CX15 Ow: the
//     team's alt_text_reminders, or the a11y_show_alt_text_reminders user
//     pref) a send with attached images that have no alt text opens the
//     dialog for them first, "Edit alt text" (altTextOnlyVariant) with
//     "Send" ("Schedule message" for a scheduled send) as its submit; the
//     message goes once it's submitted (includeMissingImageAttachmentsAltTextSpeedBump off).
//
// The dialog is "Edit file details" (or "Edit alt text"), with, when the
// team asks for alt text (alt_text_reminders), "Your organization requests
// that you add image descriptions (alt text)…" under the title. Each file
// is a page (FileFieldset): an image's preview beside its fields
// (p-edit_file_dialog--file-contents-with-preview: 2fr and 1fr, 16px
// apart, the dialog up to 900 wide), "File name" (required, at most 255;
// placeholder big-sur-trip-aerial.png) and, for an image, "Description"
// (the alt text, at most 10,000; Slack's subtext and Bixby Bridge
// placeholder). Several images page with Next, the submit button on the
// last page and, before it, an outline submit button too (the speed
// bump's Send). Submitting is useFilesToEdit's onSubmit: an uploaded file
// gets editFileApi (files.edit {file, title, alt_txt}), one still
// uploading keeps the values (updatePendingFile) for when it lands; then
// maybeOpenAltTextRemindersModal. Closing with changes asks "Would you
// like to save your work?" (Discard / Keep Editing).
//
// Not drawn: Slack AI's alt text generation (CX15 SR: the
// ai_image_alt_text experiment and the Slack AI permission, which the
// account lacks), the validation messages and the page control.

// The dialog's strings are EditFileDialog's (file-details); the speed
// bump's submit labels prepareAndSendMessage's (client); the reminders
// modal's prefs'.
var (
	nsFileDetails = i18n.NS("file-details")
	nsClient      = i18n.NS("client")
	nsPrefs       = i18n.NS("prefs")
)

// editFileOpts are openEditFileDialog's arguments.
type editFileOpts struct {
	altOnly     bool   // altTextOnlyVariant
	submitLabel string // "" is Done
	onSubmit    func() // after the values are saved
	entry       string // dialogEntrypoint
}

type editFileValues struct {
	title, alt string
	image      bool
}

type editFileDialog struct {
	a     *attachments
	chips []*attChip
	opts  editFileOpts
	cur   int
	init  []editFileValues

	titles, alts []widget.Editor
	m            *popups.Modal
	focused      int // the page whose field took the focus last
	submitted    bool
	previews     []string // localimg URLs of the images' bytes (dropped on close)
}

// lastEditFile is the dialog opened last (tests).
var lastEditFile *editFileDialog

// altTextRemindersOn is CX15 Ow: the team's alt_text_reminders (V2, the
// org asks for alt text) or the user's a11y_show_alt_text_reminders.
func altTextRemindersOn(a *core.App) bool {
	return a != nil && (altTextTeamRequested(a) || a.PrefBool("a11y_show_alt_text_reminders", false))
}

// altTextTeamRequested is isAltTextV2EnabledForTeam: alt_text_reminders.
func altTextTeamRequested(a *core.App) bool {
	if a == nil {
		return false
	}
	v, ok := a.TeamPref("alt_text_reminders")
	if !ok {
		return false
	}
	var on bool
	if json.Unmarshal(v, &on) == nil {
		return on
	}
	var s string
	return json.Unmarshal(v, &s) == nil && (s == "true" || s == "1")
}

// openEditFile opens EditFileDialog for chips, on page start.
func (a *attachments) openEditFile(chips []*attChip, start int, o editFileOpts) *editFileDialog {
	if len(chips) == 0 {
		return nil
	}
	d := &editFileDialog{a: a, chips: chips, opts: o, cur: min(max(start, 0), len(chips)-1), focused: -1}
	d.titles = make([]widget.Editor, len(chips))
	d.alts = make([]widget.Editor, len(chips))
	d.previews = make([]string, len(chips))
	a.mu.Lock()
	for i, c := range chips {
		v := editFileValues{title: c.title, alt: c.alt, image: c.kind.Image && !c.thumbBad}
		d.init = append(d.init, v)
		d.titles[i].SingleLine, d.titles[i].Submit = true, true
		d.titles[i].SetText(v.title)
		d.alts[i].SetText(v.alt)
		if v.image && len(c.data) > 0 {
			d.previews[i] = localimg.Put(c.data)
		}
	}
	a.mu.Unlock()

	title := nsFileDetails.T("Edit file details")
	if o.altOnly {
		title = nsFileDetails.T("Edit alt text")
	}
	submit := o.submitLabel
	if submit == "" {
		submit = nsFileDetails.T("Done")
	}
	last := func() bool { return d.cur == len(d.chips)-1 }
	extra := &popups.ModalButton{Label: o.submitLabel, Kind: popups.BtnOutline, Enabled: d.valid,
		Hidden: func() bool { return last() || o.submitLabel == "" }, Do: func() bool { return d.submit() }}
	next := &popups.ModalButton{Label: nsFileDetails.T("Next"), Kind: popups.BtnPrimary,
		Hidden: last, Do: func() bool { d.cur = min(d.cur+1, len(d.chips)-1); d.a.h.Invalidate(); return false }}
	done := &popups.ModalButton{Label: submit, Kind: popups.BtnPrimary, Enabled: d.valid,
		Hidden: func() bool { return !last() }, Do: func() bool { return d.submit() }}
	width := float32(520)
	for _, v := range d.init {
		if v.image {
			width = 900 // .p-edit_file_dialog_with_previews
		}
	}
	mo := popups.ModalOpts{
		Title:      title,
		Width:      width,
		Body:       d.body,
		Buttons:    []*popups.ModalButton{extra, next, done},
		CloseGuard: d.closeGuard,
		OnClose:    d.dropPreviews,
		OnCmdEnter: func() { // onKeyDownCapture: ⌘Enter / Ctrl+Enter goes on or submits
			if !last() {
				d.cur++
				d.a.h.Invalidate()
			} else if d.valid() && d.submit() {
				d.m.Close()
			}
		},
		FocusStops: func() []event.Tag { return []event.Tag{&d.titles[d.cur], &d.alts[d.cur]} },
	}
	if altTextTeamRequested(a.core) {
		mo.Subtitle = nsFileDetails.T("Your organization requests that you add image descriptions (alt text) to help people with visual disabilities understand what you’re sharing.")
	}
	d.m = popups.NewModal(a.h, mo)
	a.h.ShowOverlay(d.m)
	lastEditFile = d
	return d
}

// values are the form's values now.
func (d *editFileDialog) values() []editFileValues {
	out := make([]editFileValues, len(d.chips))
	for i := range d.chips {
		out[i] = editFileValues{title: d.titles[i].Text(), alt: d.alts[i].Text(), image: d.init[i].image}
	}
	return out
}

// valid is the form's validation (validateOnMount): every title
// required and at most 255 characters, every description at most 10,000.
func (d *editFileDialog) valid() bool {
	for _, v := range d.values() {
		t := strings.TrimSpace(v.title)
		if t == "" || utf8.RuneCountInString(v.title) > 255 || utf8.RuneCountInString(v.alt) > 10000 {
			return false
		}
	}
	return true
}

// dirty is Formik's dirty: a value changed.
func (d *editFileDialog) dirty() bool {
	for i, v := range d.values() {
		if v.title != d.init[i].title || v.alt != d.init[i].alt {
			return true
		}
	}
	return false
}

// closeGuard is EditFileDialogFooter's speed bump (shouldEnableSpeedbump:
// an image among the files and the form dirty).
func (d *editFileDialog) closeGuard() bool {
	hasImage := false
	for _, v := range d.init {
		hasImage = hasImage || v.image
	}
	if d.submitted || !hasImage || !d.dirty() {
		return true
	}
	keep := &popups.ModalButton{Label: nsFileDetails.T("Keep Editing"), Kind: popups.BtnPrimary}
	sb := popups.NewModal(d.a.h, popups.ModalOpts{
		Title: nsFileDetails.T("Would you like to save your work?"),
		Buttons: []*popups.ModalButton{{Label: nsFileDetails.T("Discard"), Kind: popups.BtnOutline, Do: func() bool {
			d.submitted = true // discarded: nothing more to guard
			d.m.Close()
			return true
		}}, keep},
		AutoFocus: keep,
	})
	d.a.h.ShowOverlay(sb)
	return false
}

func (d *editFileDialog) dropPreviews() {
	for i, u := range d.previews {
		if u != "" {
			localimg.Drop(u)
			d.previews[i] = ""
		}
	}
}

// submit is EditFileDialog's onSubmit over useFilesToEdit: each file's
// values saved (files.edit when uploaded, the pending file otherwise),
// then the caller's onSubmit and the reminders modal.
func (d *editFileDialog) submit() bool {
	if d.submitted || !d.valid() {
		return false
	}
	d.submitted = true
	vals := d.values()
	dirty := d.dirty()
	newAlt := false
	for i, c := range d.chips {
		v := vals[i]
		if v.alt != "" && d.init[i].alt == "" {
			newAlt = true
		}
		d.a.setDetails(c, strings.TrimSpace(v.title), v.alt, v.image)
	}
	if d.opts.onSubmit != nil {
		d.opts.onSubmit()
	}
	if dirty && newAlt {
		maybeAltTextRemindersModal(d.a.h)
	}
	return true
}

// setDetails is editFileApi for an uploaded file (files.edit, the old
// values back if it fails) or updatePendingFile for one still uploading
// (prepare applies them once it lands).
func (a *attachments) setDetails(c *attChip, title, alt string, image bool) {
	a.mu.Lock()
	oldTitle, oldAlt := c.title, c.alt
	if title != "" {
		c.title = title
	}
	if image {
		c.alt = alt
	}
	var id string
	if c.prepared != nil {
		id = c.prepared.ID
		c.prepared.Title = c.title
	} else {
		c.detailsPending = true
	}
	if c.pf != nil {
		c.pf.Title = c.title
	}
	app := a.core
	a.mu.Unlock()
	a.h.Invalidate()
	pendingChanged()
	if id == "" || app == nil {
		return
	}
	go func() {
		if err := app.EditFile(context.Background(), id, title, alt); err != nil {
			a.mu.Lock() // editFileApi's catch: the file as it was
			c.title, c.alt = oldTitle, oldAlt
			if c.prepared != nil {
				c.prepared.Title = oldTitle
			}
			a.mu.Unlock()
			a.h.Invalidate()
		}
	}()
}

// maybeAltTextRemindersModal is maybeOpenAltTextRemindersModal: once
// (seen_a11y_alt_text_reminders_modal), while reminders are off, "Would
// you like reminders to add alt text?"; Yes turns
// a11y_show_alt_text_reminders on.
func maybeAltTextRemindersModal(h Host) {
	app := h.Core()
	if app == nil || app.PrefBool("seen_a11y_alt_text_reminders_modal", false) || altTextRemindersOn(app) {
		return
	}
	go func() { _ = app.SetPref(context.Background(), "seen_a11y_alt_text_reminders_modal", true) }()
	th := h.Theme()
	yes := &popups.ModalButton{Label: nsPrefs.T("Yes"), Kind: popups.BtnPrimary, Do: func() bool {
		go func() { _ = app.SetPref(context.Background(), "a11y_show_alt_text_reminders", true) }()
		return true
	}}
	h.ShowOverlay(popups.NewModal(h, popups.ModalOpts{
		Title: nsPrefs.T("Would you like reminders to add alt text?"),
		Body: func(gtx layout.Context) layout.Dimensions {
			return label(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22, Color: th.C.Fg},
				nsPrefs.T("We can automatically prompt you to add alt text to your images before sending messages. You can always turn these reminders on or off in the Accessibility section of Preferences."))
		},
		Buttons:   []*popups.ModalButton{{Label: nsPrefs.T("No"), Kind: popups.BtnOutline}, yes},
		AutoFocus: yes,
	}))
}

// body is FileFieldset for the current page.
func (d *editFileDialog) body(gtx layout.Context) layout.Dimensions {
	th := d.a.h.Theme()
	i := d.cur
	w := gtx.Constraints.Max.X
	if d.focused != i { // focusOnMount: the name, or the description in the alt text variant
		d.focused = i
		tag := &d.titles[i]
		if d.opts.altOnly && d.init[i].image {
			tag = &d.alts[i]
		}
		gtx.Execute(key.FocusCmd{Tag: tag})
	}
	for {
		e, ok := d.titles[i].Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok && d.cur == len(d.chips)-1 && d.valid() && d.submit() {
			d.m.Close()
		}
	}
	fx, fw := 0, w
	h := 0
	if d.init[i].image {
		// grid-template-columns: 2fr 1fr, column-gap 16px
		gap := gtx.Dp(16)
		pw := (w - gap) * 2 / 3
		fx, fw = pw+gap, w-pw-gap
		ph := gtx.Dp(382) // the file viewer's image area
		d.preview(gtx, th, i, image.Rect(0, 0, pw, ph))
		h = ph
	}
	off := op.Offset(image.Pt(fx, 0)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(fw, gtx.Constraints.Max.Y)}
	fh := d.fields(g, th, i)
	off.Pop()
	return layout.Dimensions{Size: image.Pt(w, max(h, fh))}
}

// fields are EditFileDialogTitle and, for an image,
// EditFileDialogImageAltText.
func (d *editFileDialog) fields(gtx layout.Context, th *theme.Theme, i int) int {
	w := gtx.Constraints.Max.X
	y := 0
	lab := func(s string, size, lh float32, wt font.Weight, col color.NRGBA) {
		off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		dd := label(g, th.Shaper, textStyle{Size: unit.Sp(size), LineHeight: unit.Sp(lh), Weight: wt, Color: col}, s)
		off.Pop()
		y += dd.Size.Y
	}
	lab(nsFileDetails.T("File name"), 15, 22, font.Bold, th.C.Fg)
	y += gtx.Dp(8)
	ph := nsFileDetails.T("big-sur-trip-aerial.png")
	if d.titles[i].Len() > 0 {
		ph = ""
	}
	off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(40)))
	popups.Field(g, th, &d.titles[i], ph, "", 40, 15)
	off.Pop()
	y += gtx.Dp(40)
	if !d.init[i].image {
		return y
	}
	y += gtx.Dp(20)
	lab(nsFileDetails.T("Description"), 15, 22, font.Bold, th.C.Fg)
	sub := nsFileDetails.T("A description (or alt-text) helps people with visual disabilities understand what you are sharing.")
	if altTextTeamRequested(d.a.core) {
		sub = nsFileDetails.T("A description of the image you are sharing")
	}
	lab(sub, 13, 18, font.Normal, th.C.FgMuted)
	y += gtx.Dp(8)
	aph := nsFileDetails.T("Aerial view of the Bixby Bridge and coastline of the Big Sur area in California.")
	if d.alts[i].Len() > 0 {
		aph = ""
	}
	off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	textArea(gtx, th, &d.alts[i], aph, image.Pt(w, gtx.Dp(104)))
	off.Pop()
	return y + gtx.Dp(104)
}

// preview is EditFileDialogImagePreview: the image fitted into r, never
// upscaled, on the surface tint while it loads.
func (d *editFileDialog) preview(gtx layout.Context, th *theme.Theme, i int, r image.Rectangle) {
	rad := gtx.Dp(12)
	u := d.previews[i]
	if u == "" {
		fillRRect(gtx, r, rad, withAlpha(th.C.Fg, 0x0f))
		return
	}
	img, ok := d.a.h.Images().Get(u, 0)
	if !ok {
		fillRRect(gtx, r, rad, withAlpha(th.C.Fg, 0x0f))
		return
	}
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	sc := min(float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y), 1)
	dw, dh := float32(sz.X)*sc, float32(sz.Y)*sc
	o := f32.Pt(float32(r.Min.X)+(float32(r.Dx())-dw)/2, float32(r.Min.Y)+(float32(r.Dy())-dh)/2)
	st := clip.Rect(r).Push(gtx.Ops)
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sc, sc)).Offset(o)).Push(gtx.Ops)
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	st.Pop()
}

// textArea is Slack Kit's Textarea (c-input_textarea: radius 8, the input
// border, 8px 12px padding, the 5px focus ring) around ed at size.
func textArea(gtx layout.Context, th *theme.Theme, ed *widget.Editor, placeholder string, size image.Point) {
	cl := popups.ColorsFor(th)
	r := image.Rectangle{Max: size}
	rad := gtx.Dp(8)
	if gtx.Focused(ed) {
		ring, one := gtx.Dp(5), max(1, gtx.Dp(1))
		fillRRect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		fillRRect(gtx, r.Inset(-one), rad+one, cl.Focus)
		fillRRect(gtx, r, rad, th.C.Bg)
	} else {
		fillRRect(gtx, r, rad, cl.InputBorder)
		fillRRect(gtx, r.Inset(1), rad-1, th.C.Bg)
	}
	px, py := gtx.Dp(12), gtx.Dp(8)
	inner := image.Pt(max(1, size.X-2*px), max(1, size.Y-2*py))
	off := op.Offset(image.Pt(px, py)).Push(gtx.Ops)
	defer off.Pop()
	g := gtx
	g.Constraints = layout.Exact(inner)
	if ed.Len() == 0 && placeholder != "" {
		label(g, th.Shaper, textStyle{Size: 15, LineHeight: 22, Color: cl.Placeholder}, placeholder)
	}
	tm := op.Record(gtx.Ops)
	paint.ColorOp{Color: th.C.Fg}.Add(gtx.Ops)
	txt := tm.Stop()
	sm := op.Record(gtx.Ops)
	paint.ColorOp{Color: th.C.Selection}.Add(gtx.Ops)
	sel := sm.Stop()
	ed.LineHeight, ed.LineHeightScale = unit.Sp(22), 1
	ed.Layout(g, th.Shaper, font.Font{}, 15, txt, sel)
}

// imageChipsWithoutAlt are the attached images with no alt text (the
// speed bump's files).
func (a *attachments) imageChipsWithoutAlt() []*attChip {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*attChip
	for _, c := range a.chips {
		if c.kind.Image && !c.thumbBad && c.alt == "" {
			out = append(out, c)
		}
	}
	return out
}

// imageChips are the attached images.
func (a *attachments) imageChips() []*attChip {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*attChip
	for _, c := range a.chips {
		if c.kind.Image && !c.thumbBad {
			out = append(out, c)
		}
	}
	return out
}

// altTextSpeedBump is maybeOpenEditFileDialogForImages at send: with
// reminders on and images lacking alt text, the dialog opens with
// submitLabel ("Send" or "Schedule message") and the send goes on once
// it's submitted. It reports whether it opened.
func (c *Composer) altTextSpeedBump(scheduled bool, resume func()) bool {
	if c.edit != nil || !altTextRemindersOn(c.api.a) {
		return false
	}
	if len(c.att.imageChipsWithoutAlt()) == 0 {
		return false
	}
	imgs := c.att.imageChips()
	label := nsClient.T("Send")
	if scheduled {
		label = nsClient.T("Schedule message")
	}
	c.att.openEditFile(imgs, 0, editFileOpts{altOnly: true, submitLabel: label, onSubmit: resume, entry: "alt_text_speedbump"})
	return true
}

// imageOf decodes data's size (the viewer's image dimensions).
func imageOf(data []byte) (image.Point, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Point{}, false
	}
	return image.Pt(cfg.Width, cfg.Height), true
}

// pendingViewerImage is the attached image as the pending viewer shows it.
func pendingViewerImage(u string, data []byte, title string) richtext.Image {
	img := richtext.Image{URL: u, Title: title, Alt: title}
	if sz, ok := imageOf(data); ok {
		img.W, img.H = sz.X, sz.Y
	}
	return img
}
