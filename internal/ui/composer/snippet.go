package composer

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's snippet dialog (gantry-v2-async-snippet-dialog wc+S
// SnippetDialog over the snippet editor, codemirror-snippet-editor lYxy
// CodeMirrorSnippetEditor; CSS p-snippet_dialog, p-snippet_editor). Three
// ways open it (openSnippetDialog, vWtd):
//
//   - Ctrl+Shift+Enter (⌘⇧Enter) in the composer, MessageInput.onEnter's
//     openSnippetDialog(): the message's text becomes the snippet and the
//     composer empties;
//   - the paste-limit dialog's "Create a Snippet" (renderPasteLimitDialog):
//     the refused paste becomes the snippet; the composer keeps its text
//     and gets it back on cancel (initialContents);
//   - the + menu's Text snippet (createSnippetShortcut): the composer's
//     text, the composer untouched.
//
// openSnippetDialog straightens smart quotes in the text. The dialog is
// "Create snippet", 44rem wide: "Title (optional)" (placeholder
// snippet.txt, at most 255 characters; Enter creates) and "Type" (Auto
// Detect Type, Plain Text, then every type by label; an externally shared
// channel doesn't offer ASP.NET, JavaScript, JSP, VB and VBScript) on one
// row (2:1, 8px apart), "Content" (CodeMirror, 12rem, line numbers; ⌘Enter
// / Ctrl+Enter creates), "Wrap" (the snippet_editor_wrap_long_lines pref),
// and while sharing "Add a message, if you’d like." with, in a thread
// where the viewer may post, the "Also send to" checkbox. The footer has
// "Share this file" and the conversation select (in a thread "Sharing
// snippet in a thread" and the thread's conversation) and "Create
// Snippet", off while sharing somewhere the viewer can't post, without
// content, over the message limit or with a title over 255 characters; no
// Cancel. Closing it with changed content asks "Are you sure you want to
// discard this snippet permanently?" (Cancel / Delete Snippet).
//
// Create Snippet is uploadSnippet: the file is the title (else
// "Untitled") holding the content, typed with its snippet_type; shared it
// goes to the conversation (in the thread, with the message and the
// broadcast), else it is only uploaded. A failed upload opens the dialog
// again with what was typed (handleSubmitError).

// snippetDisallowedExt are wc+S's k: types an externally shared channel
// isn't offered.
var snippetDisallowedExt = map[string]bool{"aspx": true, "javascript": true, "jsp": true, "vb": true, "vbscript": true}

type snippetOption struct{ key, label string }

// snippetTypeOptions is getFileTypeOptions: Auto Detect Type, Plain Text,
// then the types sorted by their lower-cased label (lodash sortBy, stable).
func snippetTypeOptions(extShared bool) []snippetOption {
	out := []snippetOption{
		{"auto", i18n.T("snippet-dialog", "Auto Detect Type")},
		{"text", i18n.T("snippet-dialog", "Plain Text")},
	}
	rest := make([]snippetOption, 0, len(snippetTypes))
	for _, t := range snippetTypes {
		if extShared && snippetDisallowedExt[t.Key] {
			continue
		}
		rest = append(rest, snippetOption{t.Key, i18n.T("files_types", t.Label)})
	}
	sort.SliceStable(rest, func(i, j int) bool { return strings.ToLower(rest[i].label) < strings.ToLower(rest[j].label) })
	return append(out, rest...)
}

// snippetTypeLabel is getSingleFileTypeOption's label.
func snippetTypeLabel(key string) string {
	switch key {
	case "", "auto":
		return i18n.T("snippet-dialog", "Auto Detect Type")
	case "text":
		return i18n.T("snippet-dialog", "Plain Text")
	}
	for _, t := range snippetTypes {
		if t.Key == key {
			return i18n.T("files_types", t.Label)
		}
	}
	return key
}

// straightQuotes is openSnippetDialog's /‘|’/ → ' and /“|”/ → ".
func straightQuotes(s string) string {
	return strings.NewReplacer("‘", "'", "’", "'", "“", `"`, "”", `"`).Replace(s)
}

// snippetSpec is openSnippetDialog's arguments (and handleSubmitError's
// reopening).
type snippetSpec struct {
	text      string // initialSnippetText
	name, typ string // initialName, initialType
	share     *bool  // initialShareWith (nil: share when there is a conversation)
	ch, ts    string // initialChannelId, threadTs
	msg       string // the message (reopening only)
	restore   *Doc   // initialContents: back into the composer on cancel
}

type snippetDialog struct {
	c    *Composer
	h    Host
	m    *popups.Modal
	spec snippetSpec

	title, msg widget.Editor
	code       snippetCode
	typ        string
	typeBtn    widget.Clickable
	typeOv     popups.Overlay
	wrapBtn    widget.Clickable
	wrapOn     *bool // the pref's value until users.prefs.set lands
	shareBtn   widget.Clickable
	shareOn    bool
	ch         string
	chBtn      widget.Clickable
	chOv       popups.Overlay
	bcBtn      widget.Clickable
	broadcast  bool
	create     *popups.ModalButton
	focused    bool
	submitted  bool
}

// openSnippet opens the snippet dialog from the composer: fromKeys is
// Ctrl+Shift+Enter (the message moves into the snippet), else the + menu
// (the message stays).
func (c *Composer) openSnippet(fromKeys bool) {
	if c.edit != nil {
		return
	}
	spec := snippetSpec{text: c.ed.Doc().Text(), ch: c.ch, ts: c.ts}
	if fromKeys {
		c.clearAfterSend() // s.value(""): the message is the snippet now
	}
	openSnippetDialog(c, spec)
}

// openSnippetFromPaste is the paste-limit dialog's "Create a Snippet":
// the refused paste, with the composer's contents to put back on cancel.
func (c *Composer) openSnippetFromPaste(pasted string) {
	if c.edit != nil {
		return
	}
	openSnippetDialog(c, snippetSpec{text: pasted, ch: c.ch, ts: c.ts, restore: c.ed.Doc().Clone()})
}

// lastSnippet is the dialog openSnippetDialog opened last (tests).
var lastSnippet *snippetDialog

func openSnippetDialog(c *Composer, spec snippetSpec) *snippetDialog {
	spec.text = straightQuotes(spec.text)
	d := &snippetDialog{c: c, h: c.h, spec: spec, typ: spec.typ, ch: spec.ch}
	if d.typ == "" {
		d.typ = "auto"
	}
	d.title.SingleLine, d.title.Submit = true, true
	d.title.SetText(spec.name)
	d.msg.SingleLine, d.msg.Submit = true, true
	d.msg.SetText(spec.msg)
	d.code.ed.SetText(spec.text)
	d.code.list.Axis, d.code.hlist.Axis = layout.Vertical, layout.Horizontal
	d.shareOn = spec.ch != ""
	if spec.share != nil {
		d.shareOn = *spec.share
	}
	d.create = &popups.ModalButton{Label: i18n.T("snippet-dialog", "Create Snippet"), Kind: popups.BtnPrimary,
		Enabled: func() bool { return !d.goDisabled() }, Do: func() bool { return d.submit() }}
	d.m = popups.NewModal(c.h, popups.ModalOpts{
		Title:      i18n.T("snippet-dialog", "Create snippet"),
		Width:      704, // .p-snippet_dialog 44rem
		Body:       d.body,
		Buttons:    []*popups.ModalButton{d.create},
		FooterLeft: d.footer,
		CloseGuard: d.closeGuard,
	}).OldDialog()
	c.h.ShowOverlay(d.m)
	lastSnippet = d
	return d
}

// wrap is the snippet_editor_wrap_long_lines pref.
func (d *snippetDialog) wrap() bool {
	if d.wrapOn != nil {
		return *d.wrapOn
	}
	return d.c.api.a != nil && d.c.api.a.PrefBool("snippet_editor_wrap_long_lines", false)
}

// conv is the selected conversation (ok false: none or unknown).
func (d *snippetDialog) conv() (core.Conv, bool) {
	if d.ch == "" || d.c.api.a == nil {
		return core.Conv{}, false
	}
	return d.c.api.conv(d.ch)
}

// canPost is canPostInSelectedChannel as far as Clack knows it: a known
// conversation that isn't archived.
func (d *snippetDialog) canPost() bool {
	cv, ok := d.conv()
	return ok && !cv.IsArchived
}

// goDisabled is isGoButtonDisabled.
func (d *snippetDialog) goDisabled() bool {
	content := d.code.ed.Text()
	over := d.shareOn && jsLen(d.msg.Text()) > composerMaxLength
	return d.shareOn && !d.canPost() && d.spec.ts == "" || content == "" || over || utf8.RuneCountInString(d.title.Text()) > 255
}

// closeGuard is shouldShowSpeedBump on cancel or close: changed content
// asks first; otherwise handleCancel puts the composer's text back.
func (d *snippetDialog) closeGuard() bool {
	if d.submitted || d.code.ed.Text() == d.spec.text {
		d.cancel()
		return true
	}
	popups.Confirm(d.h, i18n.T("snippet-dialog", "Are you sure you want to discard this snippet permanently?"), "",
		i18n.T("snippet-dialog", "Delete Snippet"), true, nil, func() {
			d.cancel()
			d.m.Close()
		}) // Confirm shows itself
	return false
}

// cancel is handleCancel: initialContents back into the composer.
func (d *snippetDialog) cancel() {
	if d.submitted || d.spec.restore == nil {
		return
	}
	d.c.ed.SetDoc(d.spec.restore)
	d.c.lastVer = d.c.ed.Version()
	d.c.Focus()
}

// snippetUpload is uploadSnippet's file.
func snippetUpload(name, typ, content string) core.UploadFile {
	if name == "" {
		name = i18n.T("snippet-dialog", "Untitled")
	}
	return core.UploadFile{Name: name, Data: []byte(content), SnippetType: typ}
}

// submit is handleSubmit: upload (and share) off the UI goroutine, close.
func (d *snippetDialog) submit() bool {
	if d.goDisabled() || d.submitted {
		return false
	}
	d.submitted = true
	f := snippetUpload(d.title.Text(), d.typ, d.code.ed.Text())
	share, ch, ts := d.shareOn, d.ch, d.spec.ts
	msg := strings.TrimSpace(d.msg.Text())
	broadcast := share && ts != "" && d.broadcast && d.canPost()
	api, c := d.c.api, d.c
	again := d.spec
	again.name, again.typ, again.text, again.msg, again.share, again.ch = d.title.Text(), d.typ, d.code.ed.Text(), d.msg.Text(), &share, ch
	go func() {
		ctx := context.Background()
		var err error
		if share && ch != "" {
			_, err = api.upload(ctx, core.UploadOpts{Channel: ch, ThreadTS: ts, Comment: msg, Broadcast: broadcast, Files: []core.UploadFile{f}})
		} else {
			_, err = api.prepareUpload(ctx, f, nil)
		}
		if err != nil {
			c.later(func() { openSnippetDialog(c, again) }) // handleSubmitError
		}
	}()
	return true
}

func (d *snippetDialog) handle(gtx layout.Context) {
	for {
		e, ok := d.title.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok && !d.goDisabled() { // handleFileNameKeydown
			if d.submit() {
				d.m.Close()
			}
		}
	}
	for {
		e, ok := d.msg.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok && !d.goDisabled() { // the message input's onEnter
			if d.submit() {
				d.m.Close()
			}
		}
	}
	// handleCodeMirrorKeyDown: ⌘Enter / Ctrl+Enter creates; Escape stops
	// editing (the editor lets go of the keyboard) instead of closing
	for {
		e, ok := gtx.Event(key.Filter{Focus: &d.code.ed, Name: key.NameReturn, Required: key.ModShortcut},
			key.Filter{Focus: &d.code.ed, Name: key.NameEnter, Required: key.ModShortcut},
			key.Filter{Focus: &d.code.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		if ke.Name == key.NameEscape {
			gtx.Execute(key.FocusCmd{})
			continue
		}
		if !d.goDisabled() && d.submit() {
			d.m.Close()
		}
	}
	if d.wrapBtn.Clicked(gtx) {
		on := !d.wrap()
		d.wrapOn = &on
		if a := d.c.api.a; a != nil { // handleWrapChange: setUserPrefByApi
			go func() { _ = a.SetPref(context.Background(), "snippet_editor_wrap_long_lines", on) }()
		}
	}
	if d.shareBtn.Clicked(gtx) {
		d.shareOn = !d.shareOn
	}
	if d.bcBtn.Clicked(gtx) {
		d.broadcast = !d.broadcast
	}
}

// field rectangles in window coordinates for the selects' lists
func (d *snippetDialog) body(gtx layout.Context) layout.Dimensions {
	d.handle(gtx)
	th := d.h.Theme()
	cl := popups.ColorsFor(th)
	w := gtx.Constraints.Max.X
	org := popups.ModalBodyOrigin(gtx, d.m)
	gap := gtx.Dp(8)
	lab := func(y int, s string, x int) int {
		off := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		dd := label(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22, Weight: font.Bold, Color: th.C.Fg}, s)
		off.Pop()
		return dd.Size.Y + gtx.Dp(4)
	}
	// .p-snipper_editor__header_fields: the title flex 2 1em, the type
	// flex 1em with 8px before it
	em := gtx.Dp(15)
	free := w - 2*em - gap
	nameW := em + free*2/3
	typeX := nameW + gap
	typeW := w - typeX
	y := 0
	lh := lab(y, i18n.T("snippet-dialog", "Title (optional)"), 0)
	lab(y, i18n.T("snippet-dialog", "Type"), typeX)
	y += lh
	fh := popups.SelectFieldHeight(gtx)
	off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(nameW, fh))
	popups.Field(g, th, &d.title, i18n.T("snippet-dialog", "snippet.txt"), "", 36, 15)
	off.Pop()
	if d.typeBtn.Clicked(gtx) && !overlayOpen(d.typeOv) {
		ext := false
		if cv, ok := d.conv(); ok {
			ext = cv.IsShared // is_ext_shared
		}
		opts := snippetTypeOptions(ext && d.shareOn)
		labels := make([]string, len(opts))
		sel := 0
		for i, o := range opts {
			labels[i] = o.label
			if o.key == d.typ {
				sel = i
			}
		}
		r := image.Rectangle{Min: org.Add(image.Pt(typeX, y)), Max: org.Add(image.Pt(typeX+typeW, y+fh))}
		d.typeOv = popups.NewOptionList(d.h, r, labels, sel, func(i int) {
			d.typ = opts[i].key
			d.h.Invalidate()
		})
		d.h.ShowOverlay(d.typeOv)
	}
	off = op.Offset(image.Pt(typeX, y)).Push(gtx.Ops)
	popups.SelectField(gtx, th, &d.typeBtn, typeW, "", snippetTypeLabel(d.typ), overlayOpen(d.typeOv), false)
	off.Pop()
	y += fh + gtx.Dp(16)

	// renderFileContents: "Content" and CodeMirror (12rem)
	y += lab(y, i18n.T("snippet-dialog", "Content"), 0)
	off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	g = gtx
	g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(192)))
	d.focused = d.code.layout(g, th, cl, d.wrap())
	off.Pop()
	y += gtx.Dp(192) + gap

	// renderSubContent: Wrap, and the a11y label while the editor has focus
	off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	cd := snippetCheck(gtx, th, &d.wrapBtn, d.wrap(), false, i18n.T("snippet-dialog", "Wrap"), 13, 18)
	if d.focused {
		a11y := i18n.T("snippet-dialog", "Press {key} to stop editing.", "key", "Esc")
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(w-cd.Size.X-gtx.Dp(8), 0), Max: image.Pt(w-cd.Size.X-gtx.Dp(8), gtx.Dp(18))}
		ao := op.Offset(image.Pt(cd.Size.X, 0)).Push(gtx.Ops)
		label(g, th.Shaper, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, Align: text.End}, a11y)
		ao.Pop()
	}
	off.Pop()
	y += cd.Size.Y

	// renderMessageInput: hidden while not sharing
	if d.shareOn {
		y += gtx.Dp(16) // .p-share_dialog__section
		off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		g = gtx
		g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(40)))
		popups.Field(g, th, &d.msg, i18n.T("snippet-dialog", "Add a message, if you’d like."), "", 40, 15)
		off.Pop()
		y += gtx.Dp(40)
		if d.spec.ts != "" && d.canPost() { // maybeRenderBroadcast
			y += gap
			off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			bd := snippetCheck(gtx, th, &d.bcBtn, d.broadcast, false, d.broadcastLabel(), 15, 22)
			off.Pop()
			y += bd.Size.Y
		}
		y += gtx.Dp(8) // .p-snippet_dialog .c-dialog__body padding-bottom
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// broadcastLabel is FileUploadBroadcastCheckbox's text.
func (d *snippetDialog) broadcastLabel() string {
	cv, _ := d.conv()
	switch cv.Kind {
	case "mpim":
		return i18n.T("file-upload", "Also send to the group")
	case "im":
		return i18n.T("file-upload", "Also send as direct message")
	}
	return i18n.T("file-upload", "Also send to ") + "#" + cv.Name
}

// convLabel names a conversation in the select ("#general", a DM's name).
func convLabel(cv core.Conv) string {
	if cv.Kind == "im" || cv.Kind == "mpim" {
		return cv.Name
	}
	return "#" + cv.Name
}

// footer is renderShareWithPicker: in a thread "Sharing snippet in a
// thread" over the conversation; else "Share this file" and the
// conversation select (disabled while not sharing).
func (d *snippetDialog) footer(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	room := gtx.Constraints.Max.X
	if d.spec.ts != "" {
		cv, _ := d.conv()
		a := label(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, i18n.T("snippet-dialog", "Sharing snippet in a thread"))
		off := op.Offset(image.Pt(0, a.Size.Y)).Push(gtx.Ops)
		b := label(gtx, th.Shaper, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, MaxLines: 1}, convLabel(cv))
		off.Pop()
		return layout.Dimensions{Size: image.Pt(max(a.Size.X, b.Size.X), a.Size.Y+b.Size.Y)}
	}
	fh := popups.SelectFieldHeight(gtx)
	cd := snippetCheck(gtx, th, &d.shareBtn, d.shareOn, false, i18n.T("snippet-dialog", "Share this file"), 15, 22)
	x := cd.Size.X + gtx.Dp(12)
	selW := min(gtx.Dp(260), room-x)
	if selW < gtx.Dp(80) {
		return layout.Dimensions{Size: image.Pt(cd.Size.X, max(cd.Size.Y, fh))}
	}
	if d.chBtn.Clicked(gtx) && d.shareOn && !overlayOpen(d.chOv) {
		d.openConvList(gtx, x, selW)
	}
	cv, _ := d.conv()
	sel := convLabel(cv)
	if d.ch == "" {
		sel = i18n.T("snippet-dialog", "Share with:")
	}
	cy := (fh - cd.Size.Y) / 2
	if cy < 0 {
		cy = 0
	}
	off := op.Offset(image.Pt(x, -cy)).Push(gtx.Ops)
	popups.SelectField(gtx, th, &d.chBtn, selW, "", sel, overlayOpen(d.chOv), !d.shareOn)
	off.Pop()
	return layout.Dimensions{Size: image.Pt(x+selW, cd.Size.Y)}
}

// openConvList lists the conversations the select offers (the sidebar's,
// channels first), the selected one highlighted, under the select (x
// into the footer, w wide).
func (d *snippetDialog) openConvList(gtx layout.Context, x, w int) {
	if d.c.api.a == nil {
		return
	}
	var convs []core.Conv
	for _, cv := range d.c.api.snapshot().Convs {
		if cv.IsArchived || cv.Name == "" {
			continue
		}
		convs = append(convs, cv)
	}
	sort.SliceStable(convs, func(i, j int) bool {
		ci, cj := convs[i].Kind == "im" || convs[i].Kind == "mpim", convs[j].Kind == "im" || convs[j].Kind == "mpim"
		if ci != cj {
			return !ci
		}
		return strings.ToLower(convs[i].Name) < strings.ToLower(convs[j].Name)
	})
	labels := make([]string, len(convs))
	sel := -1
	for i, cv := range convs {
		labels[i] = convLabel(cv)
		if cv.ID == d.ch {
			sel = i
		}
	}
	// the footer's select: its row is the card's bottom less the footer's
	// 20px padding, from 28px in
	r := d.m.Rect()
	left := r.Min.X + gtx.Dp(28) + x
	anchor := image.Rect(left, r.Max.Y-gtx.Dp(20)-popups.SelectFieldHeight(gtx), left+w, r.Max.Y-gtx.Dp(20))
	d.chOv = popups.NewOptionList(d.h, anchor, labels, sel, func(i int) {
		d.ch = convs[i].ID
		d.h.Invalidate()
	})
	d.h.ShowOverlay(d.chOv)
}

// snippetCheck draws a checkbox with its label after it (c-label--inline,
// the box 8px before the text), clicks on btn.
func snippetCheck(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, on, disabled bool, s string, size, lh float32) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Sp(unit.Sp(lh))
		box := gtx.Dp(16)
		off := op.Offset(image.Pt(0, (h-box)/2)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		popups.Checkbox(g, th, on, false, disabled)
		off.Pop()
		x := box + gtx.Dp(8)
		off = op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		g.Constraints.Max.X = max(1, gtx.Constraints.Max.X-x)
		dd := label(g, th.Shaper, textStyle{Size: unit.Sp(size), LineHeight: unit.Sp(lh), Color: th.C.Fg, MaxLines: 1}, s)
		off.Pop()
		return layout.Dimensions{Size: image.Pt(x+dd.Size.X, max(h, dd.Size.Y))}
	})
}

// ---- the content editor (CodeMirror) ----

// snippetCode is the snippet editor's CodeMirror: line numbers in a
// gutter (.CodeMirror-gutters #f7f7f7 with a 1px #ddd edge; numbers #999,
// right-aligned, padding 0 3px 0 5px, at least 20px), monospace text at
// 13px on 1.5 lines (.CodeMirror-sizer) with 4px around the lines
// (.CodeMirror-lines) and 4px beside them, wrapped only with Wrap on; it
// scrolls inside its 12rem. A tab draws as Gio's missing glyph (its
// shaper has no tab stops); the content keeps it.
type snippetCode struct {
	ed    widget.Editor
	list  layout.List
	hlist layout.List
	click widget.Clickable
}

const snippetCodeSize, snippetCodeLine = 13, 19.5

func (s *snippetCode) layout(gtx layout.Context, th *theme.Theme, cl *popups.Colors, wrap bool) bool {
	sz := gtx.Constraints.Max
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(8)
	focused := gtx.Focused(&s.ed)
	bg := th.C.Bg
	if focused { // p-snippet_editor__codemirror--focus-within
		ring, one := gtx.Dp(5), max(1, gtx.Dp(1))
		fillRRect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		fillRRect(gtx, r.Inset(-one), rad+one, cl.Focus)
		fillRRect(gtx, r, rad, bg)
	} else {
		fillRRect(gtx, r, rad, cl.InputBorder)
		fillRRect(gtx, r.Inset(1), rad-1, bg)
	}
	defer clip.UniformRRect(r.Inset(1), rad-1).Push(gtx.Ops).Pop()

	lines := strings.Count(s.ed.Text(), "\n") + 1
	digits := len(itoa(lines))
	lineH := gtx.Sp(snippetCodeLine)
	numW := max(gtx.Dp(20), digits*gtx.Sp(7)+gtx.Dp(1))
	gutW := gtx.Dp(5) + numW + gtx.Dp(3) + 1
	gutBg, gutEdge, numCol := rgbHex(0xf7f7f7), rgbHex(0xdddddd), rgbHex(0x999999)
	if th.Dark {
		gutBg, gutEdge, numCol = withAlpha(th.C.Fg, 0x0d), withAlpha(th.C.Fg, 0x26), th.C.FgMuted
	}
	paint.FillShape(gtx.Ops, gutBg, clip.Rect{Max: image.Pt(gutW-1, sz.Y)}.Op())
	paint.FillShape(gtx.Ops, gutEdge, clip.Rect{Min: image.Pt(gutW-1, 0), Max: image.Pt(gutW, sz.Y)}.Op())

	mono := font.Font{Typeface: font.Typeface(Mono)}
	textW := sz.X - gutW - 2*gtx.Dp(4)
	edW := textW
	if !wrap {
		edW = gtx.Dp(4096) // lines don't wrap; the row scrolls sideways
	}
	ed := func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(textW, 0), Max: image.Pt(edW, 1<<20)}
		tm := op.Record(gtx.Ops)
		paint.ColorOp{Color: th.C.Fg}.Add(gtx.Ops)
		txt := tm.Stop()
		sm := op.Record(gtx.Ops)
		paint.ColorOp{Color: th.C.Selection}.Add(gtx.Ops)
		sel := sm.Stop()
		s.ed.LineHeight, s.ed.LineHeightScale = unit.Sp(snippetCodeLine), 1
		return s.ed.Layout(gtx, th.Shaper, mono, snippetCodeSize, txt, sel)
	}
	numbers := func(gtx layout.Context, n int, heights []int) {
		y := 0
		for i := 0; i < n; i++ {
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(numW, 0), Max: image.Pt(numW, lineH)}
			off := op.Offset(image.Pt(gtx.Dp(5), y)).Push(gtx.Ops)
			label(g, th.Shaper, textStyle{Size: snippetCodeSize, LineHeight: snippetCodeLine, Typeface: font.Typeface(Mono), Color: numCol, Align: text.End}, itoa(i+1))
			off.Pop()
			h := lineH
			if i < len(heights) {
				h = heights[i]
			}
			y += h
		}
	}
	// each line's height: one line each unwrapped, measured when wrapped
	heights := make([]int, lines)
	for i := range heights {
		heights[i] = lineH
	}
	if wrap {
		for i, ln := range strings.Split(s.ed.Text(), "\n") {
			if ln == "" {
				continue
			}
			m := op.Record(gtx.Ops)
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(textW, 1<<20)}
			dd := label(g, th.Shaper, textStyle{Size: snippetCodeSize, LineHeight: snippetCodeLine, Typeface: font.Typeface(Mono), Color: th.C.Fg}, ln)
			m.Stop()
			heights[i] = max(lineH, dd.Size.Y)
		}
	}
	gtx.Constraints = layout.Exact(sz)
	s.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		pad := gtx.Dp(4) // .CodeMirror-lines padding 4px 0
		total := 2 * pad
		for _, h := range heights {
			total += h
		}
		no := op.Offset(image.Pt(0, pad)).Push(gtx.Ops)
		numbers(gtx, lines, heights)
		no.Pop()
		off := op.Offset(image.Pt(gutW+gtx.Dp(4), pad)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(sz.X-gutW-gtx.Dp(4), 1<<20)}
		var dd layout.Dimensions
		if wrap {
			dd = ed(g)
		} else {
			dd = s.hlist.Layout(g, 1, func(gtx layout.Context, _ int) layout.Dimensions { return ed(gtx) })
		}
		off.Pop()
		return layout.Dimensions{Size: image.Pt(sz.X, max(total, dd.Size.Y+2*pad, sz.Y))}
	})
	return focused
}

func rgbHex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
