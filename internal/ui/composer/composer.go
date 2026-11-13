package composer

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Composer is the message box under a channel or thread, or the inline
// editor of an existing message (NewEdit).
type Composer struct {
	h      Host
	api    coreAPI
	gtip   groupTip // the @group slug tooltip (group_tip.go)
	ttip   textyTip // an emoji's title tooltip (texty_tip.go)
	ch, ts string
	edit   *core.Message // edit mode: the message being edited
	// unsendBy is when the message just sent stops being unsendable
	// (Slack's messageEligibleForUnsending, expiryTs = send + 15 s).
	unsendBy time.Time
	onDone   func()
	done     bool
	ed       *Editor
	ac       *Autocomplete
	ui       chrome
	att      *attachments
	st       EditorStyle
	focusReq bool
	// cmdK is the Mod+K coachmark while it shows (cmdk_coachmark.go);
	// drawnAt is the frame the composer was last laid out in (the
	// coachmark goes when its composer isn't drawn).
	cmdK    *cmdKCoachmark
	drawnAt time.Time
	stGen   uint64 // th.Gen st's colours were taken at
	// focus dropped by Gio without a blur (restoreDroppedFocus)
	focusPress    uint64 // pressCount when last seen focused
	focusRestored bool   // asked for it back once since
	focused       bool

	// altAsked is includeMissingImageAttachmentsAltTextSpeedBump off: the
	// send the alt text speed bump resumes.
	altAsked bool

	// Origin is the composer's top-left corner in window coordinates. The
	// parent sets it each frame (Gio has no global transform query); menus
	// and the emoji picker are anchored with it.
	Origin image.Point

	// OnEditLast is called when Up is pressed in an empty composer (Slack
	// edits your last message). The ui root wires it to the message list.
	OnEditLast func()
	// OnFocusList moves keyboard focus to the message list with its newest
	// message selected (Accessibility > "Press ↑ in the empty message
	// field to: Move focus to the message list").
	OnFocusList func()
	// OnDownWhenEmpty is the view's onDownArrowWhenEmpty: Down in the
	// empty box. The Threads view focuses the item below the reply box
	// (client-threads-view onDownArrowInEmptyInput, focusItemBelowInput);
	// ThreadFooter's default and the channel's do nothing.
	OnDownWhenEmpty func()

	// InThreadsView marks the Threads view's inline reply composer:
	// .p-threads_view .p-threads_footer__input_container has margin 0 20px,
	// so none of the flexpane's 12px over the box.
	InThreadsView bool

	// SaveEdit, when set, saves an inline edit instead of core.Edit
	// (ui/messages routes it through taut.Dispatch("commitMessageEdit")).
	// The blocks are the composer's own; plugin send transforms are the
	// saver's job (commitMessageEdit's native applies them). fileIDs is
	// nil for an edit whose files didn't change (saveMessageEdit), else
	// every file the message keeps, in order (saveMessageEditWithFiles:
	// chat.update's file_ids).
	SaveEdit func(ctx context.Context, ch, ts, text string, blocks json.RawMessage, fileIDs []string) error

	// editFiles0 is an edit's initialFileIds (the message's files)
	editFiles0 []string

	broadcast  bool
	lastTyping time.Time
	typingOn   bool // typing reported since the composer was last empty
	lastVer    uint64
	emojiVer   uint64 // custom emoji set version the doc was last scanned at
	// charsOverLimit at limitVer (overLimit: counted once per change)
	limitVer  uint64
	limitOver int
	limitOK   bool

	// Server draft sync (syncDraftCreate / syncDraftUpdates /
	// deleteDraftApi): see drafts_sync.go.
	dsync draftSync
	// live is the local draft plugins read as you type (taut_drafts.go).
	live liveDraft
	// the files in the draft (draft_files.go): attVer counts changes to
	// them; touched is set once the user (or a restore) filled the
	// composer, after which server drafts no longer restore into it;
	// srvGen is the core.DraftsGen last looked at.
	attVer  uint64
	touched bool
	srvGen  uint64

	setBroadcastFn func(bool) // bound once: the plugin buttons' broadcast.set

	// Slack boundaries (slack.go): this frame's chrome state, the props
	// patched boundaries handed back, and widgets bound once so the
	// unpatched path allocates nothing.
	cs            chromeState
	paneP, inputP slackc.Props
	sendJS        slackc.JSFunc // a patched prepareAndSendMessage (send checks)
	pending       *outgoing     // the message sendJS is deciding on
	top           int           // context bar height above the box
	ctxShowing    bool          // onIsContextBarShowing
	sbar          scheduledBar  // the scheduled-send context bar (schedule_bar.go)
	cmdPick       core.Command  // the command last picked from the / list
	fbar          floatBar      // the floating formatting bar (floatbar.go)
	drewInput     bool          // the editor was laid out (no roadblock)
	roadblock     string        // SetRoadblock: Slack's notice instead of the editor
	rbLink, rbURL string        // SetRoadblockLink: a link after it ("Learn more")
	rbClick       widget.Clickable
	wPane, wInput layout.Widget
	wEditor, wAtt layout.Widget
	keyS, draftS  string
	// attachGen is the key's swap count when this composer took its slot
	// (stash_draft.go): behind, its saves belong to a stashed draft
	attachGen uint64
}

// plainTextLocal is the "Format messages with markup" choice made in
// Clack's Preferences; until then Slack's plain_text_mode pref decides.
var plainTextLocal *bool

// SetPlainTextMode sets Preferences > Advanced > "Format messages with
// markup" (Slack's plain_text_mode) for every composer.
func SetPlainTextMode(on bool) { plainTextLocal = &on }

// ResetPlainTextMode forgets the local choice (the account pref applies).
func ResetPlainTextMode() { plainTextLocal = nil }

// PlainTextMode reports the effective setting: the local choice, else the
// pref Slack sent (off by default: live formatting, like Slack).
func PlainTextMode(a *core.App) bool {
	if plainTextLocal != nil {
		return *plainTextLocal
	}
	return a != nil && coreAPI{a}.plainTextMode()
}

// showFormatting is Slack's "Show formatting" (Aa) toggle; one setting for
// every composer, like Slack's pref: the account pref
// msg_input_sticky_composer (boot-shared Ex/u kx reads it; ComposerButton's
// click writes it with setUserPrefByApi, WysiwygContainer). Without a
// signed-in core (tests) it is this package value alone.
var showFormatting = true

// formattingWrite is the Aa value on its way to users.prefs.set: until
// the write lands (or fails) it stands in for the pref.
var formattingWrite struct {
	sync.Mutex
	pending int
	on      bool
}

// syncShowFormatting reads msg_input_sticky_composer (shown when Slack
// sends no value: the toolbar is on by default).
func syncShowFormatting(api coreAPI) {
	if api.a == nil {
		return
	}
	formattingWrite.Lock()
	pending, on := formattingWrite.pending, formattingWrite.on
	formattingWrite.Unlock()
	if pending > 0 {
		showFormatting = on
		return
	}
	showFormatting = api.a.PrefBool("msg_input_sticky_composer", true)
}

// toggleShowFormatting is ComposerButton's click: flip the toolbar and
// store the choice in msg_input_sticky_composer, off the UI goroutine.
func (c *Composer) toggleShowFormatting() {
	showFormatting = !showFormatting
	if c.api.a == nil {
		return
	}
	on, api := showFormatting, c.api
	formattingWrite.Lock()
	formattingWrite.pending++
	formattingWrite.on = on
	formattingWrite.Unlock()
	go func() {
		err := api.a.SetPref(context.Background(), "msg_input_sticky_composer", on)
		formattingWrite.Lock()
		formattingWrite.pending--
		formattingWrite.Unlock()
		if err != nil {
			c.later(func() {}) // redraw with the pref Slack kept
		}
	}()
}

// UpArrowShortcut is Accessibility > "Press ↑ in the empty message field
// to:" (Slack's client-side composerUpArrowShortcutBehavior, uMEH): 0
// unset, 1 LIST (focus the message list), 2 LIST_IF_EMPTY, 3 EDIT. The ui
// root sets it from ui.json.
var UpArrowShortcut = func() int { return 0 }

// SetShowFormatting sets the Aa toggle for every composer (the ui root can
// restore it from prefs); ShowFormatting reads it.
func SetShowFormatting(on bool) { showFormatting = on }
func ShowFormatting() bool      { return showFormatting }

// New returns the composer for a channel (threadTS "") or a thread.
func New(h Host, channelID, threadTS string) *Composer {
	c := &Composer{h: h, api: coreAPI{h.Core()}, ch: channelID, ts: threadTS}
	c.init()
	if d := loadDraft(channelID, threadTS); d != nil {
		c.ed.SetDoc(d)
	}
	c.lastVer = c.ed.Version()
	return c
}

// NewEdit returns an inline editor for m (one of the viewer's messages).
// onDone runs after Save or Cancel; the caller then drops the composer.
func NewEdit(h Host, m core.Message, onDone func()) *Composer {
	c := &Composer{h: h, api: coreAPI{h.Core()}, ch: m.Channel, ts: m.ThreadTS, onDone: onDone}
	mm := m
	c.edit = &mm
	c.init()
	var r AtomResolver
	if c.api.a != nil {
		r = c.api.resolver()
	}
	c.ed.SetDoc(docForMessage(m, r))
	c.lastVer = c.ed.Version()
	c.focusReq = true
	// editComposerFiles: the message's files (the edit's initialFileIds)
	// sit in the edit's attachments row, removable like new ones
	c.editFiles0 = editFileIDs(&mm)
	c.att.restoreFiles(editDraftFiles(&mm))
	return c
}

// editDraftFiles is the message's files as restorable chips (an id and a
// thumbnail: nothing to upload again).
func editDraftFiles(m *core.Message) []core.DraftFile {
	var out []core.DraftFile
	for _, b := range m.Doc.Blocks {
		f := b.File
		if b.Kind != richtext.FileBlock || f == nil || f.ID == "" {
			continue
		}
		df := core.DraftFile{ID: f.ID, Name: f.Name, Title: f.Title, Mimetype: f.Mime, PrettyType: f.PrettyType, Size: f.Size}
		if f.Thumb != nil && (f.IsImage || f.IsVideo) {
			df.Thumb = f.Thumb.URL
		}
		out = append(out, df)
	}
	return out
}

func (c *Composer) init() {
	c.ed = NewEditor()
	c.ed.Snippets = c.edit == nil      // MessageInput's enableSnippetCreation; not the edit composer
	c.ed.CmdKCoachmark = c.edit == nil // MessageInput's onCmdKWhenSelected; the edit composer (cjbg) has none
	c.bindSlugs()
	if c.api.a != nil {
		warmEmoji()
		c.ed.emoji = c.lookupEmoji
		c.emojiVer = c.api.emojiVersion()
	}
	c.ac = newAutocomplete(c.h, c.ch)
	bindAutocomplete(c.ed, c.ac, c.h.Invalidate)
	c.ed.Intercept = c.floatBarKey(c.ed.Intercept) // the floating bar's Escape first
	accept := c.ac.OnAccept
	c.ac.OnAccept = func(it acItem, start, end int) {
		if it.Kind == acCommand && it.Command != nil {
			c.cmdPick = *it.Command // the picked command's app: slashCommandAppId
		}
		accept(it, start, end)
	}
	c.ac.later, c.ac.thread, c.ac.threadTS = c.later, c.ts != "", c.ts
	// ThreadFooter passes onPageUp / onPageDown that return false (the
	// keys do nothing in a reply box); the channel's input and the edit
	// composer pass none
	c.ed.PageKeysHandled = c.ts != "" && c.edit == nil
	if c.edit == nil { // the conversation's files, kept across composers
		c.att = attachmentsFor(draftKey(c.ch, c.ts), c.h)
		c.att.setOnChange(c.attChanged)
		// the + menu's Workflow row (Slack has none when editing)
		c.att.insertLink = func(text, url string) { c.ed.SetLink(text, url); c.Focus() }
	} else {
		c.att = newAttachments(c.h)
	}
	typingHub(c.h.Core()).addHost(c.h) // listen for typing events
	c.initIDs()
	c.attachGen = attachGenOf(c.key())
	c.wPane, c.wInput = c.layoutPaneBody, c.layoutInputBody
	c.setBroadcastFn = c.setBroadcast
	c.wEditor, c.wAtt = c.layoutEditor, c.att.Layout
}

// Focus asks for keyboard focus on the next frame.
func (c *Composer) Focus() { c.focusReq = true; c.h.Invalidate() }

// Done reports whether an edit composer was saved or cancelled.
func (c *Composer) Done() bool { return c.done }

// Editing reports whether this is an edit composer.
func (c *Composer) Editing() bool { return c.edit != nil }

// InsertText inserts plain text at the caret.
func (c *Composer) InsertText(s string) { c.ed.InsertText(s); c.Focus() }

// InsertEmoji inserts an emoji atom at the caret.
func (c *Composer) InsertEmoji(name string) {
	lo, hi := c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	c.ed.InsertAtom(lo, hi, c.emojiAtom(name))
	c.Focus()
}

// Close stores the draft and releases background work.
func (c *Composer) Close() {
	c.saveDraft()
	c.ac.Close()
}

func (c *Composer) emojiAtom(name string) *Atom {
	name = strings.Trim(name, ":")
	url, txt, _ := c.api.emoji(name)
	return emojiAtomFrom(name, url, txt)
}

// lookupEmoji resolves a typed shortcode against the loaded set: the
// custom map (aliases followed) then the standard table, both O(1).
func (c *Composer) lookupEmoji(name string) (*Atom, bool) {
	url, txt, ok := c.api.emoji(name)
	if !ok {
		return nil, false
	}
	return emojiAtomFrom(name, url, txt), true
}

// syncEmojiSet converts shortcodes typed before the custom emoji set they
// name had loaded (or changed): once per new set version.
func (c *Composer) syncEmojiSet() {
	if c.api.a == nil {
		return
	}
	if v := c.api.emojiVersion(); v != c.emojiVer {
		c.emojiVer = v
		if c.ed.ConvertEmoji() {
			c.h.Invalidate()
		}
	}
}

// prepareSend is the document submit serializes: a copy with every
// remaining ":name:" shortcode as an emoji (Slack converts on send too)
// and, under "Format messages with markup", the typed markup parsed into
// formatting the way Slack's send does.
func (c *Composer) prepareSend(d *Doc) *Doc {
	doc := d.Clone()
	c.resolvePendingSlugs(doc)
	var look emojiLookup
	if c.api.a != nil {
		look = c.lookupEmoji
	}
	convertEmojiRange(doc, 0, doc.Len(), look)
	convertNativeEmojiRange(doc, 0, doc.Len(), look)
	if PlainTextMode(c.api.a) && c.api.a != nil {
		doc = parseMarkup(doc, c.api.resolver())
	}
	return doc
}

// ---- layout ----

// Layout draws the composer. Channel/thread composers take the full width
// they're given (margins included) and size their height to the content;
// edit composers draw just the box.
func (c *Composer) Layout(gtx layout.Context) layout.Dimensions {
	runRestores()
	c.drawnAt = gtx.Now
	c.maybeRestoreServerDraft()
	syncHooks()
	th := c.h.Theme()
	// the style's colours are the theme's: rebuild them when it changes
	// (th.Gen: a live Light/Dark switch or new stylesheet tokens; the two
	// modes may share one shaper, so the shaper alone doesn't say)
	if c.st.Shaper != th.Shaper || c.st.Shaper == nil || c.stGen != th.Gen {
		c.st = c.editorStyle()
		c.stGen = th.Gen
	}
	c.st.Images = c.emojiImage
	c.st.PlaceholderText = ""

	// Not in a measure pass (gtx.Disabled(): a patched boundary above the
	// composer, ChatTabs' ClientPrimaryView, measures it first), where a
	// FocusCmd goes nowhere and the request would be lost.
	if c.focusReq && gtx.Source.Enabled() {
		c.focusReq = false
		c.ed.Focus(gtx)
	}
	c.ed.plainText = PlainTextMode(c.api.a)
	c.ed.smart = SmartSymbols(c.api.a)
	syncShowFormatting(c.api)
	c.ed.upMode, c.ed.upBrowse = UpArrowShortcut(), c.edit == nil && c.api.a != nil && c.api.upToBrowse()
	c.syncEmojiSet()
	c.handleEditor(gtx)
	c.restoreDroppedFocus(gtx)
	c.handleActions(gtx)

	// Slack's boundaries (slack.go): MessagePaneInput / InputContainer
	// (the context bar, the roadblock, MessageInput), or MessageInput for
	// an edit. Unpatched each is a direct call.
	c.cs = c.chromeState(gtx)
	c.ui.prime(&c.cs)
	c.top, c.drewInput = 0, false
	c.paneP, c.inputP = nil, nil
	var dims layout.Dimensions
	switch {
	case c.edit != nil:
		dims = slackc.Component(gtx, "MessageInput", c, (*Composer).inputProps, (*Composer).layoutInput)
	case c.ts != "":
		dims = slackc.Component(gtx, "InputContainer", c, (*Composer).paneProps, (*Composer).layoutPane)
	default:
		dims = slackc.Component(gtx, "MessagePaneInput", c, (*Composer).paneProps, (*Composer).layoutPane)
	}

	// Autocomplete after the editor laid out, anchored at the trigger,
	// with what plugins changed on TextyAutocomplete.
	c.ac.setOverrides(c.ui.f.acP)
	c.ac.Update(c.ed.Doc(), caretOf(c.ed))
	c.acceptAutocomplete()
	if c.ac.Open() && c.drewInput {
		c.layoutPopup(gtx)
	}
	if c.drewInput { // floatbar.go: the formatting bar over a selection
		c.layoutFloatBar(gtx, c.ui.InputOffset().Add(image.Pt(0, c.top)))
	}
	c.handleActions(gtx)
	return dims
}

func caretOf(e *Editor) int { _, car := e.Selection(); return car }

// quillBlank is Quill's editor.isBlank(), which puts ql-blank on the
// editor and so shows Texty's .ql-placeholder (.ql-blank~.ql-placeholder
// display block): one empty line that is a plain block. A quote, list,
// code block or heading line is another blot, so a format toggled on in
// an empty composer hides the placeholder.
func quillBlank(d *Doc) bool {
	return d.Len() == 0 && len(d.Lines) <= 1 && (len(d.Lines) == 0 || d.Lines[0].Kind == Para)
}

func (c *Composer) editorStyle() EditorStyle {
	th := c.h.Theme()
	st := DefaultEditorStyle(th.Shaper)
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
	// the composer's quote bar (.c-texty_input_unstyled__container
	// .ql-editor blockquote:before): Slack Kit's sk_foreground_low_solid,
	// content-sec under the dark theme
	st.QuoteBar = rgb(0xdddddd)
	if th.Dark {
		st.QuoteBar = rgb(0xb9babd)
		if v, ok := css.SlackVars(true)["--dt_color-content-sec"]; ok {
			if col, ok := parseCSSColor(v); ok {
				st.QuoteBar = col
			}
		}
	}
	st.Selection = p.Selection
	st.Placeholder = p.FgSubtle
	st.MaxHeight = 460 - 16
	return st
}

func (c *Composer) emojiImage(url string, px int) (paint.ImageOp, bool) {
	if url == "" {
		return paint.ImageOp{}, false
	}
	return c.h.Images().Get(url, px)
}

func (c *Composer) layoutPopup(gtx layout.Context) {
	org := c.ui.InputOffset().Add(image.Pt(0, c.top))
	r := c.ed.PosRect(c.ac.Anchor())
	left := org.X + r.Min.X - gtx.Dp(17)
	bottom := org.Y + r.Min.Y - gtx.Dp(5)
	// The popup may extend above the composer (it floats over the
	// message list), up to the composer's top-of-window position.
	avail := gtx.Constraints.Max
	pg := gtx
	pg.Constraints = layout.Constraints{Max: image.Pt(max(avail.X-left, gtx.Dp(420)), gtx.Dp(300))}
	m := op.Record(gtx.Ops)
	d := c.ac.Layout(pg, gtx.Dp(575))
	call := m.Stop()
	if d.Size.Y == 0 {
		return
	}
	left = max(left, 0)
	if left+d.Size.X > avail.X {
		left = max(avail.X-d.Size.X, 0)
	}
	off := op.Offset(image.Pt(left, bottom-d.Size.Y)).Push(gtx.Ops)
	op.Defer(gtx.Ops, call)
	off.Pop()
}

// acceptAutocomplete applies a queued pick (only used when no OnAccept is
// bound; bindAutocomplete applies picks synchronously).
func (c *Composer) acceptAutocomplete() {
	it, s, e, ok := c.ac.Accepted()
	if !ok {
		return
	}
	applyPick(c.ed, it, s, e)
	c.h.Invalidate()
}

// bindAutocomplete connects the popup to the editor. Picks are applied
// inside the key event that made them (OnAccept) and keys re-sync against
// the live document (Live), so text typed in the same input batch as the
// Tab/Enter lands after the inserted pill instead of being reordered.
func bindAutocomplete(ed *Editor, ac *Autocomplete, invalidate func()) {
	ed.Intercept = ac.Key
	ac.Live = func() (*Doc, int) { return ed.Doc(), caretOf(ed) }
	ac.OnAccept = func(it acItem, start, end int) {
		// the pick's recordFrecencyEvent ran in Autocomplete.accept
		// (recordPick), for every list
		applyPick(ed, it, start, end)
		if invalidate != nil {
			invalidate()
		}
	}
}

// applyPick replaces the trigger token [start, end) with the pick: a pill
// (mention, channel, emoji) or the command text, caret after it.
func applyPick(ed *Editor, it acItem, start, end int) {
	if it.Kind == acCommand {
		ed.Replace(start, end, "/"+it.Name+" ")
	} else if it.Atom != nil {
		ed.InsertAtom(start, end, it.Atom)
	}
}

func (c *Composer) chromeState(gtx layout.Context) chromeState {
	st := chromeState{
		Focused:        c.ed.Focused(gtx),
		ShowFormatting: showFormatting && !c.ed.plainText, // Slack hides the toolbar under markup mode
		Active:         c.ed.ActiveStyle(),
		Block:          c.ed.ActiveBlock(),
		Link:           c.ed.ActiveLink(),
		CanSend:        !c.ed.Empty() || c.att.Len() > 0 || c.att.gifCount() > 0,
		HintInPre:      uipref.Get().EnterIsSpecial && c.ed.inCode(),
		Broadcast:      c.broadcast,
		SetBroadcast:   c.setBroadcastFn,
		Origin:         c.Origin,
		Key:            c.key(),
		Channel:        c.ch,
		ThreadTS:       c.ts,
		DraftID:        c.draftID(),
		ViewContext:    c.viewContext(),
		Count:          c.ed.Doc().Len(),
		OverLimit:      c.overLimit(),
		Pending:        c.att.Len() > 0,
	}
	if isHeading(st.Block) && c.api.a != nil {
		st.HeadingFormats = c.api.a.Experiment("rich_text_headers_and_dividers") == "on"
	}
	if d := c.ed.Doc(); d.Len() == 0 && (len(d.Lines) == 0 || d.Lines[0].Kind == Para) {
		st.InputEmpty = true // checkIfTextyIsEmptyOfTextOrFormats
	}
	if lo, hi := c.ed.Selection(); lo <= hi {
		st.SelLo, st.SelN = lo, hi-lo
	} else {
		st.SelLo, st.SelN = hi, lo-hi
	}
	st.InCode = st.Block == CodeBlock
	st.ShowPlaceholder = quillBlank(c.ed.Doc())
	switch {
	case c.edit != nil:
		st.Mode = modeEdit
		st.ShowPlaceholder = false
		st.NoEditFiles = fileAttachAfterSendDisabled(c.api.a)
	case c.ts != "":
		st.Mode = modeThread
		// Slack's MessageInput: "Reply to thread…" under the
		// nested_secondary_view experiment (isNestedSecondaryViewEnabled,
		// on for the account: docs/THREADS-AUDIT.md), else "Reply…".
		st.Placeholder = i18n.T("threads", "Reply…")
		if a := c.api.a; a != nil && a.Experiment("nested_secondary_view") == "on" {
			st.Placeholder = i18n.T("threads", "Reply to thread…")
		}
	default:
		st.Mode = modeChannel
	}
	if cv, ok := c.api.conv(c.ch); ok {
		st.ChannelName = cv.Name
		switch cv.Kind {
		case "private":
			st.Private = true
		case "im", "mpim":
			st.IsDM, st.Group = true, cv.Kind == "mpim"
		}
		if st.Mode == modeChannel {
			if cv.Kind == "im" && cv.User != "" && cv.User == c.api.self().ID {
				st.Placeholder = msgInputNS.T("Jot something down") // isSelfDm
			} else {
				// the chrome draws a channel's #/lock icon before the name
				st.Placeholder, st.PlaceholderPre, st.PlaceholderPost = placeholderText(cv.Name)
				st.PlaceholderMsg = !st.IsDM
			}
		}
	} else if st.Mode == modeChannel {
		st.Placeholder = "Message"
	}
	// suppressBotTyping: a thread whose app status the footer's nugget shows
	// (app_thread_nugget_fe) leaves the app out of the typing line
	var quietBot string
	if ca := c.h.Core(); ca != nil && c.ts != "" {
		quietBot, _ = ca.SuppressBotTyping(c.ch, c.ts)
	}
	if uipref.Get().ShowTyping { // Messages & media > "Display information about who is currently typing"
		st.TypingIDs, st.TypingNames = typingHub(c.h.Core()).typers(c.ch, c.ts, c.api.self().ID)
		if quietBot != "" {
			st.TypingIDs, st.TypingNames = dropTyper(st.TypingIDs, st.TypingNames, quietBot)
		}
	}
	st.Typing = typingLine(st.TypingNames)
	if uipref.Get().ShowTyping && quietBot == "" {
		if name, status := typingHub(c.h.Core()).assistantLine(c.ch, c.ts, c.api.self().ID); status != "" {
			st.Typing, st.TypingStatus, st.TypingAgent = name+" "+status, status, name // AiAppStatusIndicator
		}
	}
	st.ScrolledTop = c.ed.scrollY > 0
	st.ScrolledBottom = c.ed.scrollY+c.ed.viewH < c.ed.contentH
	if st.ThreadTS != "" && slackc.Patched(tautButtons) {
		st.ChannelType = c.channelType() // only plugin buttons read it
	}
	return st
}

// ---- editor events ----

func (c *Composer) handleEditor(gtx layout.Context) {
	for {
		ev, ok := c.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case EvChange:
			c.changed()
		case EvSubmit:
			c.submit()
		case EvEditLast:
			if c.edit == nil && c.OnEditLast != nil {
				c.OnEditLast()
			}
		case EvFocusList:
			if c.edit == nil && c.OnFocusList != nil {
				c.OnFocusList()
			}
		case EvDownEmpty:
			if c.edit == nil && c.OnDownWhenEmpty != nil {
				c.OnDownWhenEmpty()
			}
		case EvUndoEmpty:
			if !c.tryUnsend() {
				c.ed.Undo()
			}
		case EvCancel:
			if c.edit != nil {
				c.finishEdit()
			}
		case EvLinkDialog:
			c.openLink()
		case EvPasted:
			c.att.PasteClipboardImage()
		case EvPasteLimit:
			c.pasteLimitDialog(c.ed.RefusedPaste())
		case EvSnippet:
			c.openSnippet(true)
		case EvCmdKSelected:
			c.cmdKSelected()
		case EvPlusMenu:
			// onPlusMenuShortcut: forcePlusMenuOpen, the + menu at its
			// button (the edit composer's too, unless
			// fileAttachAfterSendDisabled)
			if (c.edit == nil || !c.cs.NoEditFiles) && c.ui.rect[crAttach] != (image.Rectangle{}) && !c.ui.hidden(crAttach) {
				c.att.Menu(c.ui.rect[crAttach].Add(c.Origin).Add(image.Pt(0, c.top)), c.plusMenu())
			}
		case EvNavBack, EvNavForward:
			if n, ok := c.h.(historyNavigator); ok && c.edit == nil {
				if ev.Kind == EvNavBack {
					n.Back()
				} else {
					n.Forward()
				}
			}
		case EvFocus:
			c.focused = true
		case EvBlur:
			c.focused = false
		}
	}
}

func (c *Composer) changed() {
	v := c.ed.Version()
	if v == c.lastVer {
		return
	}
	c.lastVer = v
	if c.edit != nil {
		return
	}
	c.touched = true
	c.saveDraft()
	c.syncDraftSoon()
	c.liveDraftSoon()
	if c.ed.Empty() {
		c.typingStopped()
	} else if time.Since(c.lastTyping) > 3*time.Second {
		c.lastTyping = time.Now()
		c.typingOn = true
		api, ch, ts := c.api, c.ch, c.ts
		go sendTyping(api, ch, ts)
	}
}

func (c *Composer) handleActions(gtx layout.Context) {
	acts := c.ui.Actions()
	if len(c.fbar.acts) > 0 { // a copy: Actions hands back the chrome's own buffer
		acts = append(append([]chromeAction(nil), acts...), c.fbar.acts...)
		c.fbar.acts = c.fbar.acts[:0]
	}
	for _, a := range acts {
		a.Anchor = a.Anchor.Add(c.Origin).Add(image.Pt(0, c.top))
		switch a.Kind {
		case caFocus:
			c.ed.Focus(gtx)
		case caStyle:
			if !(c.ed.ActiveBlock() == CodeBlock) {
				c.ed.ToggleStyle(a.Style)
			}
			c.ed.Focus(gtx)
		case caBlock:
			c.ed.ToggleBlock(a.Block)
			c.ed.Focus(gtx)
		case caLink:
			c.openLink()
		case caFormatting:
			c.toggleShowFormatting()
			c.ed.Focus(gtx)
		case caAttach:
			c.att.Menu(a.Anchor, c.plusMenu())
		case caEmoji:
			c.openEmojiPicker(a.Anchor) // gifs.go: picked GIFs go into the draft
		case caMention:
			c.mentionTrigger()
			c.ed.Focus(gtx)
		case caShortcut:
			// maybeTriggerSlashCommandsAutocomplete: only into an empty
			// input without a block format (the button is disabled
			// otherwise)
			if d := c.ed.Doc(); d.Len() == 0 && d.Lines[0].Kind == Para {
				c.ed.InsertText("/")
			}
			c.ed.Focus(gtx)
		case caSend:
			c.submit()
		case caSchedule:
			c.scheduleMenu(a.Anchor)
		case caBroadcast:
			c.broadcast = !c.broadcast
		case caSave:
			c.submit()
		case caCancel:
			c.finishEdit()
		case caParagraph:
			c.paragraphMenu(a.Anchor)
		case caDivider:
			c.ed.InsertDivider()
			c.ed.Focus(gtx)
		}
		c.h.Invalidate()
	}
}

// plusMenu is the + menu's composer side: the conversation and the
// Text snippet row's snippet dialog (create-text-snippet, the same dialog
// Ctrl+Shift+Enter opens with the message).
func (c *Composer) plusMenu() plusMenu {
	pm := plusMenu{ch: c.ch}
	if c.edit == nil {
		// useComposerIAFormatter: no Text snippet (nor Workflow) when
		// isMessageEdited && isEditMessageShortcutsButton
		pm.snippet = func() { c.openSnippet(false) }
	}
	if _, ok := c.h.(gifHost); ok && gifPickerOn(c.api.a) && c.edit == nil {
		pm.gif = c.openGifPicker
	}
	return pm
}

// mentionTrigger is the @ button's maybeTriggerMentionAutocomplete: it
// replaces the selection with "@" (an "@" right before the caret is
// replaced, not doubled) with a space first when the character before
// the caret is text other than a space (getText(index-1, 1): a newline
// counts as text, an emoji or mention pill has none), and leaves the
// caret after it so the member autocomplete opens. Nothing happens with
// the caret between two characters of one mention.
func (c *Composer) mentionTrigger() {
	lo, hi := c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	d := c.ed.Doc()
	start, s := lo, "@"
	if lo > 0 && d.Attrs[lo-1].Atom == nil {
		switch d.Runes[lo-1] {
		case '@':
			start = lo - 1
		case ' ':
		default:
			s = " @"
		}
	}
	c.ed.Replace(start, hi, s)
}

// openLink is openTextyLinkFormBasedOnSelection (boot-shared NTqE): the
// dialog's Text is editable unless the range holds an embed (emoji,
// mention: "\uE000" in getContentsForRange's text) or, for a new link,
// a line break; then ComposerLinkModal draws the range's rich text and
// Save links it in place (saveLink's retain branch: didTextChange false).
func (c *Composer) openLink() {
	sl, sh := c.ed.Selection()
	lo, hi, url := c.ed.LinkAt()
	d := c.ed.Doc()
	raw := d.String(lo, hi)
	txt := raw
	editing := sl == sh && url != "" // the caret in a link: the edit path
	editable := !strings.ContainsRune(raw, ObjRune) && (editing || !strings.ContainsRune(raw, '\n'))
	if strings.ContainsRune(txt, ObjRune) {
		txt = d.Slice(lo, hi).Text()
	}
	if lo != hi {
		c.ed.SetSelection(lo, hi)
	}
	save := func(text, url string) {
		c.ed.SetLink(text, url)
		c.Focus()
	}
	if editable || lo == hi {
		openLinkDialog(c.h, txt, url, save, c.Focus)
		return
	}
	var r richtext.Resolver
	if c.api.a != nil && c.api.a.S != nil {
		r = c.api.resolver()
	}
	// the text handed back is the range's own runes, so SetLink links
	// them where they are
	openLinkDialogRich(c.h, raw, url, linkPreview(r, d.Slice(lo, hi)), save, c.Focus)
}

func (c *Composer) paragraphMenu(anchor image.Rectangle) {
	// Slack's ¶ menu: Paragraph and Big/Medium/Small heading, the caret's
	// current kind checked. Picking one sets the selected lines to it.
	// (ComposerMenuTrigger: radio items with IconForFormat, the caret's
	// kind checked, paragraph for anything that isn't a heading)
	cur := c.ed.ActiveBlock()
	if !isHeading(cur) {
		cur = Para
	}
	item := func(label, icon string, k BlockKind) popups.MenuItem {
		return popups.MenuItem{Label: label, Icon: icon, Radio: true, Checked: cur == k, Do: func() {
			c.ed.SetBlock(k)
			c.Focus()
		}}
	}
	items := []popups.MenuItem{
		item("Paragraph", "paragraph", Para),
		item("Big heading", "heading-1", Header1),
		item("Medium heading", "heading-2", Header2),
		item("Small heading", "heading-3", Header3),
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 248, Anchor: anchor, Side: popups.Above}))
}

func (c *Composer) scheduleMenu(anchor image.Rectangle) {
	if c.overLimit() > 0 || c.ed.Empty() && c.att.Len() == 0 && c.att.gifCount() == 0 {
		return
	}
	// ScheduleSendMenu (schedule_menu.go): the rows for now, the Custom
	// time dialog last
	now, m := time.Now(), c.scheduleMember()
	h24 := uipref.Get().Time24
	exp := c.api.a != nil && c.api.a.Experiment("updated_scheduled_sends_desktop") == "on"
	items := []popups.MenuItem{{Key: popups.LabelKey("Schedule message"), Header: draftsNS.T("Schedule message")}}
	for _, r := range scheduleRows(now, h24, exp, m) {
		r := r
		if r.opt == schedCustom {
			items = append(items, popups.Sep(), popups.MenuItem{Key: r.key, Label: r.label, Do: func() {
				// openScheduleMessageDialog
				m := c.scheduleMember()
				openSchedulePicker(c.h, schedCustomDefault(time.Now(), m), m, func(t time.Time) {
					lastCustomSchedule = t
					c.schedule(t)
				})
			}})
			continue
		}
		items = append(items, popups.MenuItem{Key: r.key, Label: r.label, Subtitle: r.desc, Do: func() {
			at := r.at
			if r.opt < schedMember { // the time for the option when it is picked
				at = schedStandardTime(r.opt, time.Now())
			}
			c.schedule(at)
		}})
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 260, Anchor: anchor, Side: popups.Above, AlignRight: true}))
}

// scheduleMember is the DM's other member for the schedule menu's time
// zone rows (getMemberFromDraftForTimeZoneDetails: exactly one member).
func (c *Composer) scheduleMember() *schedMemberInfo {
	if c.api.a == nil {
		return nil
	}
	cv, ok := c.api.conv(c.ch)
	if !ok || cv.Kind != "im" || cv.User == "" {
		return nil
	}
	u, ok := c.api.user(cv.User)
	if !ok || u.TZ == "" && u.TZOffset == 0 {
		return nil
	}
	me := c.api.self()
	return &schedMemberInfo{name: u.Name, loc: schedMemberLoc(u), sameTZ: u.TZ == "" || u.TZ == me.TZ, offset: u.TZOffset != 0}
}

// scheduleFailedText is scheduleSendMessage's ephemeral when the
// schedule fails.
const scheduleFailedText = "We’re not sure what happened, but your message couldn’t be scheduled. It’s been saved in your Drafts so you can try again."

// schedule posts the current message at time t via core (chat scheduling
// goes through drafts in core). Files aren't scheduled (Slack schedules
// text/blocks only); a scheduled message clears the composer like a send.
func (c *Composer) schedule(t time.Time) {
	if c.edit != nil || c.ed.Empty() || !t.After(time.Now()) {
		return // the menu's times are ahead; the dialog only takes 5 min to 120 days out
	}
	doc := c.prepareSend(c.ed.Doc())
	text, blocks := doc.Mrkdwn(), doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts
	broadcast := c.broadcast && ts != ""
	c.clearAfterSend()
	go func() {
		ctx := context.Background()
		// Slack's scheduleMessage goes through prepareAndSendMessage with
		// includeBroadcastKeywordWarning and dateScheduled: the warning
		// (its scheduled wording, "Schedule Message"), then the keyword
		// check (broadcast.go)
		if used := deltaKeywords(doc.Delta()); len(used) > 0 {
			j := &sendJob{api: api, ch: ch, ts: ts, confirm: c.confirmBroadcast}
			enabled := api.broadcastEnabled(ctx, ch)
			if !j.confirmed(ctx, broadcastAsk{Channel: ch, Used: used, Scheduled: true}, enabled) {
				c.restore(doc)
				return
			}
			if msg, no := j.prohibited(ctx, ch, ts, used, enabled); no {
				api.localEphemeral(ch, ts, msg)
				c.restore(doc)
				return
			}
		}
		text, blocks := transformBlocks(ctx, text, blocks)
		_, err := api.scheduleMessage(ctx, ch, ts, text, blocks, broadcast, t.Unix())
		if err != nil {
			// scheduleSendMessage's failure: a local ephemeral
			// (scheduleSendError); the message stays a draft
			api.localEphemeral(ch, ts, scheduleFailedText)
			c.restore(doc)
		}
		// success says nothing: Slack's context bar shows the schedule
	}()
}

// ---- sending ----

// pasteLimitDialog is renderPasteLimitDialog: the title, the limit's
// explanation, "Create a Snippet" (the snippet dialog with the refused
// paste) and OK.
func (c *Composer) pasteLimitDialog(pasted string) {
	th := c.h.Theme()
	m := popups.NewModal(c.h, popups.ModalOpts{
		Title:        i18n.T("msg_input", pasteLimitTitle),
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			st := textStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}
			b := st
			b.Weight = font.Bold
			return richLabel(gtx, th.Shaper, []richSpan{{pasteLimitText, st}, {pasteLimitBold, b}, {pasteLimitAfter, st}})
		},
		Buttons: []*popups.ModalButton{
			{Label: i18n.T("msg_input", "Create a Snippet"), Kind: popups.BtnOutline, Do: func() bool {
				c.openSnippetFromPaste(pasted)
				return true
			}},
			{Label: i18n.T("msg_input", "OK"), Kind: popups.BtnPrimary, Do: func() bool { return true }},
		},
	}).OldDialog()
	c.h.ShowOverlay(m)
}

// historyNavigator is the ui root's history (triggerNavigateBack /
// triggerNavigateForward): Mod+Left / Mod+Right in an empty composer.
type historyNavigator interface {
	Back()
	Forward()
}

// slashHuddler is the ui root's /huddle: start or join the huddle in a
// conversation (internal/ui/huddleui through ui/huddle_wire.go).
type slashHuddler interface{ SlashHuddle(channel string) }

// deleteEmptiedEdit is the edit composer's startDelete (boot-shared cjbg
// maybeSave): saving an edit with nothing left in it (whitespace aside,
// no files) opens Slack's delete-message dialog for the message instead
// of saving; the editor stays open behind it.
func (c *Composer) deleteEmptiedEdit() {
	if c.edit == nil {
		return
	}
	popups.ConfirmDelete(c.h, *c.edit)
}

// overLimit is how far the message is past the 12,000-character limit
// (charlimit.go), counted once per document change.
func (c *Composer) overLimit() int {
	if v := c.ed.Version(); !c.limitOK || v != c.limitVer {
		c.limitVer, c.limitOver, c.limitOK = v, charsOverLimit(c.ed.Doc()), true
	}
	return c.limitOver
}

func (c *Composer) submit() {
	d := c.ed.Doc()
	if c.overLimit() > 0 {
		// isSendingDisabled (isOverMaxLength): Enter does nothing and the
		// send button is disabled; an edit can't be saved (cjbg maybeSave
		// returns while textyExceedsMaxLength)
		return
	}
	if d.Empty() && c.att.Len() == 0 && (c.edit != nil || c.att.gifCount() == 0) {
		if c.edit == nil {
			return
		}
		// cjbg maybeSave: an emptied message with no files left in the
		// edit (fileIdsToSave; the message's own files are chips here
		// too, hidden but kept under fileAttachAfterSendDisabled, where
		// a message with sent files saves the emptied comment) offers to
		// delete
		c.deleteEmptiedEdit()
		return
	}
	if c.edit != nil && c.editFilesChanged() {
		c.saveEditWithFiles(c.prepareSend(d))
		return
	}
	doc := c.prepareSend(d)
	text := doc.Mrkdwn()
	blocks := doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts

	if c.edit != nil {
		m := c.edit
		save := c.SaveEdit
		go func() {
			ctx := context.Background()
			var err error
			if save != nil {
				err = save(ctx, m.Channel, m.TS, text, blocks, nil)
			} else {
				text, blocks := transformBlocks(ctx, text, blocks)
				err = api.edit(ctx, m.Channel, m.TS, text, blocks)
			}
			if err != nil {
				c.later(func() { c.h.Toast("Couldn't save your edit: " + err.Error()) })
			}
		}()
		c.finishEdit()
		return
	}

	// Slash commands (only when the message starts with a known command
	// or any /word; Slack sends unknown /words as commands too).
	if c.att.Len() == 0 {
		plain := strings.TrimSpace(d.Text()) // as typed: markup mode must not eat a command's * or _
		if strings.HasPrefix(plain, "/") && !strings.HasPrefix(plain, "//") {
			cmd, rest, _ := strings.Cut(plain, " ")
			if hs, ok := c.h.(slashHuddler); ok && strings.EqualFold(cmd, "/huddle") && ts == "" {
				// /huddle starts or joins the huddle here (Slack's
				// startHuddle), like the header button
				c.clearAfterSend()
				hs.SlashHuddle(ch)
				return
			}
			// slashCommandAppId: the app of the command picked from the
			// list, while the message still runs it
			app := ""
			if p := c.cmdPick; p.AppID != "" && strings.EqualFold(strings.TrimPrefix(p.Name, "/"), strings.TrimPrefix(cmd, "/")) {
				app = p.AppID
			}
			c.cmdPick = core.Command{}
			c.clearAfterSend()
			go func() {
				// handleSlashCommand: the reply or the failure is
				// Slackbot's ephemeral; the app's modal arrives as
				// view_opened for the command's client_token
				res, err := runCommand(context.Background(), api, core.CommandOpts{Channel: ch, Command: cmd, Text: strings.TrimSpace(rest), ThreadTS: ts, AppID: app})
				if err == nil && res.KeepInput {
					c.restore(doc)
				}
			}()
			return
		}
	}

	// prepareAndSendMessage's alt text speed bump
	// (maybeOpenEditFileDialogForImages, editfile.go): images without alt
	// text, with reminders on, go through Edit alt text first; its Send
	// sends without asking again
	if c.att.Len() > 0 && !c.altAsked && c.altTextSpeedBump(false, func() {
		c.altAsked = true
		c.submit()
		c.altAsked = false
	}) {
		return
	}

	o := &outgoing{doc: doc, text: text, blocks: blocks, broadcast: c.broadcast && ts != ""}
	if c.att.Len() == 0 {
		// prepareAndSendMessage: the draft's GIFs as EwsX attachments.
		// With files they wait in the draft: Clack shares files with
		// files.share, which takes no attachments.
		o.gifs = c.att.takeGifs()
		o.attachments = gifAttachments(o.gifs, gifPickerOn(c.api.a))
	}
	c.clearAfterSend()
	switch {
	case c.sendJS.ID != 0:
		c.sendViaJS(o)
	case SendChecks != nil:
		c.checkThenSend(o)
	default:
		c.send(o)
	}
}

// outgoing is a message on its way out of the composer.
type outgoing struct {
	doc       *Doc
	text      string
	blocks    json.RawMessage
	broadcast bool
	// gifs are the draft's GIFs going with it (gifs.go) and attachments
	// their chat.postMessage attachments
	gifs        []*draftGif
	attachments json.RawMessage
	// delta is the delta a plugin handed back to the composer's
	// prepareAndSendMessage (bChannel's "/bchannel …"); nil: doc's
	delta map[string]any
}

// send posts the message (with any attached files) through Slack's
// prepareAndSendMessage → sendMessage thunks.
//
// Attached files leave the composer at once, as in Slack (the draft is
// gone and the placeholder message shows them while their uploads
// finish); a failed send puts them back.
func (c *Composer) send(o *outgoing) {
	batch := c.att.takeForSend()
	hasFiles := !batch.empty()
	job := &sendJob{api: c.api, ch: c.ch, ts: c.ts, o: o, prepared: batch.prepared, pending: batch.pending, raw: batch.raw,
		files: hasFiles, draftID: c.draftID(), view: c.viewContext(), warn: c.ts == "", confirm: c.confirmBroadcast}
	go func() {
		err := job.run(context.Background())
		if hasFiles && err != nil {
			c.later(func() {
				// the placeholder is gone and nothing was posted: the
				// message (restore, below) and its files go back into the
				// draft, and Slack's FileUploadErrorDialog says so (a
				// dismissed warning or Slackbot's answer needs no dialog)
				c.att.putBack(batch.chips)
				if !isQuietSendStop(err) {
					popups.FileUploadError(c.h, func() { c.h.Navigate("drafts") })
				}
			})
		} else if err != nil && !isQuietSendStop(err) { // Slackbot already said why, or the warning was dismissed
			c.later(func() { c.h.Toast("Message not sent: " + err.Error()) })
		}
		if err != nil {
			c.restore(o.doc)
			c.later(func() { c.att.putBackGifs(o.gifs) })
			return
		}
		c.later(func() {
			c.unsendBy = time.Now().Add(unsendWindow)
			c.att.dropPreviews(batch.chips...) // sent: no viewer shows them any more
		})
	}()
}

// unsendWindow is how long Mod+Z can unsend a message (15e3 ms).
const unsendWindow = 15 * time.Second

// unsender is the host's undoSendLastMessageInChannel: it unsends the
// viewer's last message in the conversation (asking first when
// should_show_unsend_message_confirmation is on) and hands its contents
// back for the composer.
type unsender interface {
	UnsendLast(channel, threadTS string, put func(*Doc))
}

// tryUnsend is onCmdZWhenEmpty: within the window after a send from this
// composer, Mod+Z in the empty composer unsends it. false: plain undo.
func (c *Composer) tryUnsend() bool {
	u, ok := c.h.(unsender)
	if !ok || c.edit != nil || c.unsendBy.IsZero() || time.Now().After(c.unsendBy) {
		return false
	}
	c.unsendBy = time.Time{}
	u.UnsendLast(c.ch, c.ts, c.restore)
	return true
}

// DocForMessage is a message's contents as composer text (Slack's
// getDeltaForMessageObject): its rich text, else its text. r names the
// atoms (nil: their ids).
func DocForMessage(m core.Message, r AtomResolver) *Doc { return docForMessage(m, r) }

// docForMessage is getDeltaForMessageObject (boot-shared +LRL, build
// fa938a086569) without plain-text, markdown or opaque-block mode: the
// message's own rich_text blocks through convertBlocksToDelta (V/2c),
// else its text. Every element keeps what it is: a message_mention is the
// message slug again (formatMessageSlugAttributesForDelta: its url,
// author, channel and thread), a link its text with the url (the url as
// the text when it has none), a mention, emoji or broadcast its atom, a
// date its fallback and a color its value. The rendered document is only
// a fallback for a message without blocks: its spans no longer carry what
// a slug points at (Karim's edit, 2026-11-12, showed a message_mention as
// the plain label "clod in #clack" and saved it without the link).
func docForMessage(m core.Message, r AtomResolver) *Doc {
	if raw := richTextOnly(m.Blocks); raw != nil {
		if d, err := DocFromBlocksJSON(raw, r); err == nil && d != nil && d.Len() > 0 {
			return d
		}
	}
	d := DocFromRichtext(m.Doc.Blocks)
	if d == nil || d.Len() == 0 {
		d = NewDoc()
		d.Insert(0, m.Text, Attr{})
	}
	return d
}

// richTextOnly keeps a message's rich_text blocks (getDeltaForMessageObject's
// blocks.filter(type === rich_text)); nil when it has none.
func richTextOnly(blocks json.RawMessage) json.RawMessage {
	var bs []json.RawMessage
	if len(blocks) == 0 || json.Unmarshal(blocks, &bs) != nil {
		return nil
	}
	var keep []json.RawMessage
	for _, b := range bs {
		var t struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &t) == nil && t.Type == "rich_text" {
			keep = append(keep, b)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	out, err := json.Marshal(keep)
	if err != nil {
		return nil
	}
	return out
}

// sendJob is one outgoing message; run happens off the UI goroutine.
type sendJob struct {
	api           coreAPI
	ch, ts        string
	o             *outgoing
	prepared      []core.PreparedUpload
	pending       []*core.PendingFile // eager uploads still running: the share waits for them
	raw           []core.UploadFile
	files         bool
	draftID, view string
	// warn is includeBroadcastKeywordWarning (the channel composer, not
	// threads); confirm asks the user through the warning dialog and
	// waits (nil: no dialog, so a due warning stops the send).
	warn    bool
	confirm func(ctx context.Context, ask broadcastAsk) bool
}

func (j *sendJob) run(ctx context.Context) error {
	delta := j.o.delta
	if delta == nil {
		delta = j.o.doc.Delta()
	}
	args := prepareSendArgs{ChannelID: j.ch, ReplyToTs: j.ts, Delta: delta, Text: j.o.text, Blocks: j.o.blocks,
		ShouldBroadcast: j.o.broadcast, Broadcast: j.o.broadcast, DraftID: j.draftID, ViewContext: j.view,
		IncludeBroadcastKeywordWarning: j.warn}
	if t := currentTaut(); t.ThunkPatched(thPrepareAndSend) {
		for _, p := range j.prepared {
			args.FileIDs = append(args.FileIDs, p.ID)
		}
		_, err := taut.Dispatch(ctx, t, thPrepareAndSend, args, func(ctx context.Context, a prepareSendArgs) (any, error) {
			return nil, j.prepareAndSend(ctx, a) // broadcast.go
		})
		return err
	}
	return j.prepareAndSend(ctx, args)
}

// deliver is prepareAndSendMessage's native: share the files with the
// message as their comment, or post the message.
func (j *sendJob) deliver(ctx context.Context, ch, ts, text string, blocks json.RawMessage, broadcast bool) error {
	if j.files {
		comment := text
		if j.o.doc.Empty() {
			comment = ""
		}
		return sendFiles(ctx, j.api, ch, ts, comment, blocks, broadcast, j.prepared, j.pending, j.raw)
	}
	return postMessage(ctx, j.api, ch, ts, text, blocks, broadcast, j.o.attachments)
}

// checkThenSend runs the SendChecks hook (composer.addSendCheck) and
// sends, or puts the message back (files stay attached).
func (c *Composer) checkThenSend(o *outgoing) {
	check := SendChecks
	if check == nil {
		c.send(o)
		return
	}
	check(SendCheck{ChannelID: c.ch, ThreadTS: c.ts, Blocks: o.blocks, Text: o.text}, func(ok bool) {
		c.later(func() {
			if ok {
				c.send(o)
			} else {
				c.restore(o.doc)
			}
		})
	})
}

// sendViaJS sends through a plugin's prepareAndSendMessage (taut's send
// checks wrap it on MessagePaneInput / InputContainer). It calls back
// into prepareAndSend to really send; a thrown error or rejected promise
// puts the message back.
func (c *Composer) sendViaJS(o *outgoing) {
	c.pending = o
	opts := slackc.Props{
		"delta":           o.doc.Delta(),
		"channelId":       c.ch,
		"shouldBroadcast": o.broadcast,
		"blocks":          o.blocks,
		"text":            o.text,
	}
	if c.ts != "" {
		opts["replyToTs"] = c.ts
	}
	c.sendJS.CallResult(func(_ any, err error) {
		if c.pending == o {
			c.pending = nil // a plugin sent it itself, or held it back
		}
		if err != nil {
			c.restore(o.doc)
		}
	}, opts)
}

// prepareAndSend is the prepareAndSendMessage({delta, channelId,
// replyToTs, shouldBroadcast}) MessagePaneInput and InputContainer pass
// to JS. It sends the message the composer handed to sendViaJS, with a
// plugin's shouldBroadcast override (SlimMessageBox) and delta (bChannel
// turns the message into its /bchannel command; sendJob runs it as one).
func (c *Composer) prepareAndSend(opts slackc.Props) {
	o := c.pending
	c.pending = nil
	if o == nil {
		return
	}
	if opts.Has("shouldBroadcast") {
		o.broadcast = opts.Bool("shouldBroadcast") && c.ts != ""
	}
	if d := opts.Map("delta"); d != nil {
		o.delta = map[string]any(d)
	}
	c.checkThenSend(o)
}

func (c *Composer) clearAfterSend() {
	c.ed.SetDoc(NewDoc())
	c.lastVer = c.ed.Version()
	c.deleteServerDraft("message_sent")
	c.clearLiveDraft()
	c.broadcast = false
	c.lastTyping = time.Time{}
	c.typingStopped()
	clearDraft(c.ch, c.ts)
	c.h.Invalidate()
}

// restore puts an unsent message back if the user hasn't typed since.
func (c *Composer) restore(d *Doc) {
	c.later(func() {
		if c.ed.Empty() {
			c.ed.SetDoc(d)
			c.lastVer = c.ed.Version()
			c.saveDraft()
		}
	})
}

// later runs f on the UI goroutine at the next composer Layout.
func (c *Composer) later(f func()) {
	restoreQ.Lock()
	restoreQ.fns = append(restoreQ.fns, f)
	restoreQ.Unlock()
	c.h.Invalidate()
}

// editFilesChanged is prepareAndSaveEditMessage's C: the edit's files
// differ from the message's (one removed, reordered or added, or an
// upload pending), so Save goes through saveMessageEditWithFiles.
func (c *Composer) editFilesChanged() bool {
	a := c.att
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.chips) != len(c.editFiles0) {
		return true
	}
	for i, ch := range a.chips {
		if ch.data != nil || ch.prepared == nil || ch.prepared.ID != c.editFiles0[i] {
			return true
		}
	}
	return false
}

// saveEditWithFiles is cjbg's maybeSave for an edit whose files changed
// (prepareAndSaveEditMessage → saveMessageEditWithFiles): the edit
// composer closes at once, the eager uploads (attachUploadToMessage's
// uploadPromise) finish, then chat.update goes out with file_ids, the
// files left in the edit in their order: the message's own that weren't
// removed, then the new ones. A failed upload or save toasts and leaves
// the message as it was (Slack restores the edit).
func (c *Composer) saveEditWithFiles(doc *Doc) {
	m := c.edit
	text, blocks := doc.Mrkdwn(), doc.RichTextBlocks()
	files := c.att.takeForEdit()
	save, api := c.SaveEdit, c.api
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		ids, err := editUploads(ctx, api, files)
		if err == nil {
			if save != nil {
				err = save(ctx, m.Channel, m.TS, text, blocks, ids)
			} else {
				text, blocks := transformBlocks(ctx, text, blocks)
				err = api.editWithFiles(ctx, m.Channel, m.TS, text, blocks, ids)
			}
		}
		if err != nil {
			c.later(func() { c.h.Toast("Couldn't save your edit: " + err.Error()) })
		}
	}()
	c.finishEdit()
}

// editFileIDs is the edit's initialFileIds: the files the message has,
// in its order.
func editFileIDs(m *core.Message) []string {
	var ids []string
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.FileBlock && b.File != nil && b.File.ID != "" {
			ids = append(ids, b.File.ID)
		}
	}
	return ids
}

// editFile is one file an edit saves: an id already known, an eager
// upload to wait for, or bytes to upload (the eager upload failed).
type editFile struct {
	id  string
	pf  *core.PendingFile
	raw *core.UploadFile
}

// takeForEdit takes every chip, in order, for an edit's save.
func (a *attachments) takeForEdit() []editFile {
	a.mu.Lock()
	var out []editFile
	for _, c := range a.chips {
		switch {
		case c.prepared != nil:
			out = append(out, editFile{id: c.prepared.ID})
		case c.pf != nil && c.prepErr == nil && a.h.Core() != nil:
			c.pf.Title = c.title
			out = append(out, editFile{pf: c.pf})
		default:
			out = append(out, editFile{raw: &core.UploadFile{Name: c.name, Title: c.title, Data: c.data}})
		}
	}
	a.chips, a.total = nil, 0
	a.mu.Unlock()
	a.h.Invalidate()
	pendingChanged()
	return out
}

// editUploads resolves an edit's files to their ids, in order: known ids
// at once, eager uploads when they finish, and files whose eager upload
// failed uploaded again (saveMessageEditWithFiles awaits the uploads'
// promises).
func editUploads(ctx context.Context, api coreAPI, files []editFile) ([]string, error) {
	ids := []string{}
	for _, f := range files {
		switch {
		case f.id != "":
			ids = append(ids, f.id)
		case f.pf != nil:
			r, err := f.pf.Wait(ctx)
			if err != nil {
				return nil, err
			}
			ids = append(ids, r.ID)
		default:
			r, err := api.prepareUpload(ctx, *f.raw, nil)
			if err != nil {
				return nil, err
			}
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

// TakesEditFiles reports whether files dropped on this edit composer's
// pane go to the edit (FileDragDrop: messageEditUploadNewFiles while a
// message is being edited, unless fileAttachAfterSendDisabled).
func (c *Composer) TakesEditFiles() bool {
	return c.edit != nil && !c.done && !fileAttachAfterSendDisabled(c.api.a)
}

// fileAttachAfterSendDisabled is Slack's +sRp: the team pref
// disable_file_attach_after_send is true (files can't be added to a sent
// message).
func fileAttachAfterSendDisabled(a *core.App) bool {
	if a == nil {
		return false
	}
	v, ok := a.TeamPref("disable_file_attach_after_send")
	return ok && string(v) == "true"
}

// editFileIDs is the edit's initialFileIds: the files the message has,
// in its order.
func editFileIDs(m *core.Message) []string {
	var ids []string
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.FileBlock && b.File != nil && b.File.ID != "" {
			ids = append(ids, b.File.ID)
		}
	}
	return ids
}

// editUploads resolves the files attached while editing to their ids, in
// the chips' order: prepared uploads at once, eager uploads when they
// finish, and files whose eager upload failed uploaded again (Slack's
// saveMessageEditWithFiles awaits the uploads' promises).
func editUploads(ctx context.Context, api coreAPI, b sendBatch) ([]string, error) {
	var ids []string
	for _, p := range b.prepared {
		ids = append(ids, p.ID)
	}
	for _, pf := range b.pending {
		r, err := pf.Wait(ctx)
		if err != nil {
			return nil, err
		}
		ids = append(ids, r.ID)
	}
	for _, f := range b.raw {
		r, err := api.prepareUpload(ctx, f, nil)
		if err != nil {
			return nil, err
		}
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func (c *Composer) finishEdit() {
	if c.done {
		return
	}
	c.done = true
	if c.onDone != nil {
		c.onDone()
	}
	c.h.Invalidate()
}

// restoreQ runs editor mutations from background goroutines on the UI
// goroutine (the next Layout of any composer).
var restoreQ struct {
	sync.Mutex
	fns []func()
}

func runRestores() {
	restoreQ.Lock()
	fns := restoreQ.fns
	restoreQ.fns = nil
	restoreQ.Unlock()
	for _, f := range fns {
		f()
	}
}

// ---- drafts (memory only: message contents never touch disk) ----

var drafts struct {
	sync.Mutex
	m map[string]*Doc
}

func draftKey(ch, ts string) string { return ch + "/" + ts }

func loadDraft(ch, ts string) *Doc {
	drafts.Lock()
	defer drafts.Unlock()
	if d := drafts.m[draftKey(ch, ts)]; d != nil {
		return d.Clone()
	}
	return nil
}

func (c *Composer) saveDraft() {
	if c.edit != nil {
		return
	}
	drafts.Lock()
	defer drafts.Unlock()
	if drafts.m == nil {
		drafts.m = map[string]*Doc{}
	}
	k := draftKey(c.ch, c.ts)
	if c.ed.Empty() {
		delete(drafts.m, k)
		return
	}
	drafts.m[k] = c.ed.Doc().Clone()
}

func clearDraft(ch, ts string) {
	drafts.Lock()
	delete(drafts.m, draftKey(ch, ts))
	drafts.Unlock()
}

// HasDraft reports whether a channel or thread has an unsent draft, text
// or files (for the sidebar's draft pencil).
func HasDraft(ch, ts string) bool {
	drafts.Lock()
	_, ok := drafts.m[draftKey(ch, ts)]
	drafts.Unlock()
	return ok || storedFiles(draftKey(ch, ts))
}

// setBroadcast sets the thread composer's "Also send to" checkbox (a
// plugin button's broadcast.set). Runs on the UI goroutine (slackc calls
// Go funcs from its pump).
func (c *Composer) setBroadcast(on bool) {
	if c.ts == "" || c.broadcast == on {
		return
	}
	c.broadcast = on
	c.h.Invalidate()
}

// restoreDroppedFocus is the fallback for a focus Gio dropped anyway
// (editor_focus.go): the editor got no blur (c.focused, from its
// FocusEvents) but isn't focused, so its handler vanished for a frame
// rather than anything taking the focus. It asks for the focus back once,
// unless the pointer was pressed since it was last focused (a click may
// have been meant for something else).
func (c *Composer) restoreDroppedFocus(gtx layout.Context) {
	if !gtx.Source.Enabled() {
		return
	}
	if c.ed.Focused(gtx) {
		c.focusPress, c.focusRestored = pressCount(), false
		return
	}
	if !c.focused || c.focusRestored {
		return
	}
	c.focusRestored = true
	focusStats.dropped.Add(1)
	if pressCount() != c.focusPress {
		c.focused = false
		return
	}
	focusStats.restored.Add(1)
	c.ed.Focus(gtx)
}

// FocusTag is the key-focus tag of the composer's editor (tests check
// that nothing takes focus from it).
func (c *Composer) FocusTag() any { return c.ed }

// PopupOpen reports whether the composer's own popup (autocomplete) is
// up: Escape is the popup's then, not the thread pane's.
func (c *Composer) PopupOpen() bool { return c.ac != nil && c.ac.Open() }

// TypingLine is the typing indicator's text under the composer as last
// laid out ("" none; tests).
func (c *Composer) TypingLine() string { return c.ui.cur.Typing }

// DocText is the composer's text as plain characters (tests).
func (c *Composer) DocText() string { return c.ed.Doc().Text() }

// SendRect and ScheduleRect are the send split button's two halves in
// window coordinates as of the last frame, empty when not drawn (tests
// click them like a user).
func (c *Composer) SendRect() image.Rectangle     { return c.winRect(crSend) }
func (c *Composer) ScheduleRect() image.Rectangle { return c.winRect(crSchedule) }

// BoxRect is the input box (its border included) in window coordinates
// as of the last frame, empty before it was drawn (tests measure the space
// above it).
func (c *Composer) BoxRect() image.Rectangle {
	if !c.ui.f.boxOK {
		return image.Rectangle{}
	}
	return c.ui.f.box.Add(c.Origin).Add(image.Pt(0, c.top))
}

func (c *Composer) winRect(id crBtn) image.Rectangle {
	if c.ui.shown&(1<<id) == 0 {
		return image.Rectangle{}
	}
	return c.ui.rect[id].Add(c.Origin).Add(image.Pt(0, c.top))
}

// AttachFile attaches a file held in memory, like a paste or the + menu's
// picker (its eager upload starts at once). Safe from any goroutine.
func (c *Composer) AttachFile(name string, data []byte) { c.att.Add(name, data) }

// Attached is the number of files attached now.
func (c *Composer) Attached() int { return c.att.Len() }
