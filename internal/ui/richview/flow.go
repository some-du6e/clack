package richview

import (
	"image"
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
	"github.com/some-du6e/clack/internal/workobject"
)

// ---------- display list ----------

type fragKind uint8

const (
	fText fragKind = iota
	fImage
)

// frag is one positioned inline thing: a shaped word or an inline image.
type frag struct {
	kind   fragKind
	x      float32 // text: pen x; image: left
	y      int     // text: baseline; image: top
	w      float32
	size   int // image size px / text px size (for decorations)
	top    int // line box top/bottom, for hit tests and selection
	bot    int
	line   int32
	sh     *shaped
	col    color.NRGBA
	style  richtext.Style
	target int16
	url    string // image
	p0, p1 int32  // plain text byte range
	alt    bool   // image frag has text fallback in plain (":name:")
	comp   int16  // component tag (index+1 into display.comps), 0 = static
	hov    int16  // hover layer (index+1 into display.hovers), 0 = none
	part   uint8  // CSS part (css.go), 0 = none
	boxed  bool   // its part's background is a deco box (code, pill)
	// ell is a clamp's "…" (ellipsize): drawn over the components too, as
	// Chromium paints it after every child of its line
	ell    bool
	weight font.Weight
	tip    int16 // emoji: index+1 into display.tips (its :name: tooltip); also the "(edited)" word's
	// ulT and ulU are how far the frag's decoration runs on past it, over
	// the spaces after it on its line: those of its own target (a link's
	// underline) and those with its underline/strike style. CSS decorates
	// an element's spaces too, so a decorated run of words is one line,
	// not a line per word.
	ulT, ulU float32
}

// box.cut sides
const (
	cutStart uint8 = 1 << iota
	cutEnd
)

type boxKind uint8

const (
	bFill   boxKind = iota // filled (rounded) rect
	bBorder                // fill + 1px border
	bCircle
	bRing   // circle outline
	bSquare // small filled square
	bPlay   // play triangle
	bChevron
	bCaret      // small filled down-pointing triangle
	bCaretRight // small filled right-pointing triangle
	bFade       // a vertical gradient: transparent at the top, fill at the bottom (corners)
)

type box struct {
	kind   boxKind
	r      image.Rectangle
	radius int
	fill   color.NRGBA
	stroke color.NRGBA
	bw     int
	layer  uint8 // 0 = under text, 1 = above images and text (overlays), 2 = above images, under text (scrims, pills)
	target int16 // hover-highlight target (-1 none)
	hover  color.NRGBA
	// perCorner uses corners (NW NE SE SW) instead of radius.
	perCorner bool
	corners   [4]int
	// cut: the inline box this is a line's piece of goes on before it
	// (cutStart) or after it (cutEnd), where its border is open (slice)
	cut uint8
	// hoverOver: the hover fill takes the fill's place (a pill's :hover
	// background-color), drawn over it rather than under
	hoverOver bool
	// inline: one line's piece of an inline element's box (a pill's, a
	// code chip's: a deco's)
	inline  bool
	comp    int16 // component tag, 0 = static
	hov     int16 // hover layer (index+1 into display.hovers), 0 = none
	tgTo    int16 // > target: targets target..tgTo all hover the box (a card's links)
	part    uint8 // CSS part (css.go): its background (or color, colFill) replaces fill
	colFill bool  // fill is the part's text color (list markers)
}

type imgPrim struct {
	r      image.Rectangle
	url    string
	radii  [4]int // NW NE SE SW
	target int16
	px     int         // requested decode size
	fit    bool        // contain (letterbox) instead of cover
	comp   int16       // component tag, 0 = static
	hov    int16       // hover layer (index+1 into display.hovers), 0 = none
	tint   color.NRGBA // an iconURL image: the icon's color
	alpha  float32     // < 1: drawn at this opacity (a pending upload, .5)
	play   bool        // a still the viewer played: drawn playing (animated.go)
}

// removeBtn is an unfurl's remove ("x") button (unfurl.go): drawn while
// its card (or a target inside it) or the button is hovered.
type removeBtn struct {
	r         image.Rectangle
	card, del int16
	cardTo    int16 // last target made inside the card
	// rail: the v1/v2 style left of the card (unfurl_hover.go); col is
	// its 20px column, area the box whose hover shows it (empty: only
	// the button's own hover, v2).
	rail      bool
	col, area image.Rectangle
}

// overlayIcon is a vector icon painted over images (video play buttons).
type overlayIcon struct {
	r    image.Rectangle
	name string
	col  color.NRGBA
}

type targetKind uint8

const (
	tLink targetKind = iota
	tUser
	tChannel
	tUsergroup
	tImage
	tFile
	tButton
	tCard       // whole-card click (opens URL)
	tExpand     // attachment "Show more" / "Show less" (id: attachment number)
	tCollapse   // image block's hide / show toggle (img: the image)
	tRemove     // an unfurl's remove button (id: the attachment id)
	tBot        // "Added by <app>" under Block Kit blocks (id: the bot id): its profile card
	tHuddleJoin // a huddle message's Join Huddle (id: the channel; huddle.go)
	tWorkObject // a work object card or one of its buttons (id: the action; workobject.go)
	tSnippet    // a snippet's Show more / Show less (file, act; snippet.go)
)

// cardOnly reports whether target t is a hover card trigger only (no
// link: target.cardOnly).
func (d *display) cardOnly(t int16) bool {
	return t >= 0 && int(t) < len(d.targets) && d.targets[t].cardOnly
}

type target struct {
	kind targetKind
	url  string
	id   string
	img  *richtext.Image
	file *richtext.File
	btn  *richtext.Button
	// underline on hover (links)
	underline bool
	// self: a mention of the viewer or of a group they're in (--mention)
	self bool
	// slug: a message_mention pill (.p-rich_text_slug), never underlined,
	// not even by the "underline links" preference
	slug bool
	// website: a link the "underline links" preference underlines, which
	// in Slack are only those marked data-stringify-link (links in message
	// text, mrkdwn and rich text: boot-shared kQRT renderDefaultLink) and
	// an attachment's title link (HbXx AttachmentTitle): jRB6 sets
	// underlineStyle "underline" for those alone
	website bool
	// inset: the hover decoration is box-shadow: inset 0 -1px, a line on
	// the bottom edge of the link's inline box under its text and icons
	// (the message card's context line, .context__pPPwj a:hover)
	inset bool
	att   *richtext.Attachment // tExpand: the attachment to expand
	// cardOnly: a channel the viewer can't see (BaseMissingChannel's
	// chip): not a link, but BaseMrkdwnChannel still wraps it in the
	// channel hover card's trigger
	cardOnly bool
	// cardless: a channel target that opens no hover card (a work
	// object's channel field)
	cardless bool
	// urlCard: a link in message text (rich text's Link, mrkdwn's
	// renderLink), BaseMrkdwnLink's renderDefaultLink: its hover is
	// UrlHoverCard under truncated_url_hover_card (View.isURLCard)
	urlCard bool
	// tab: a channel-tab slug's tab id (a tLink whose id is the channel):
	// the slug opens the channel hover card when TabSlugCard says Slack's
	// would
	tab string
	// inCard: a tImage inside a v3 link card (unfurl.go). A click opens
	// the image viewer; a right-click is the card's, as in Slack, where
	// the image has no context menu of its own.
	inCard bool
	// wob is a tWorkObject footer button's primary action.
	wob *workobject.Button
	// menuOnly: a file card that only has the file's context menu (a
	// snippet's pillow: PillowFile with no onClick for an expandable
	// file): a click does nothing and the pointer is no hand.
	menuOnly bool
	// act is a tSnippet's action: FileShowMore or FileShowLess.
	act FileAction
}

type hit struct {
	r image.Rectangle
	t int16
}

type lineInfo struct {
	top, bot int
	f0, f1   int // frag range
}

// display is the laid-out result of a Doc at one width.
type display struct {
	frags   []frag
	boxes   []box
	imgs    []imgPrim
	targets []target
	hits    []hit
	lines   []lineInfo
	plain   []byte
	size    image.Point
	// overhang is how far paint reaches left of the box: an ordered list's
	// outside ::marker wider than the li's 20px margin ("10. ") overflows
	// into the message gutter, which Slack doesn't clip.
	overhang int
	comps    []compBox // Slack components (see comps.go)
	pillN    []int16   // pillRects' count of each pill's line boxes
	slots    []slot    // interactive element runs drawn by Options.Elements
	tips     []string  // emoji tooltips (":name:"), frag.tip-1
	codes    []codeRun // inline code runs and code blocks (codecopy.go)
	fileSecs []fileSec // file cards with a section button (filesection.go)
	removes  []removeBtn
	cards    []v3Card // v3 cards: context menu, hover card (unfurl_hover.go)
	// overlayIcons paint over images (unfurl.go's video buttons)
	overlayIcons []overlayIcon
	// spinners are InfiniteSpinners drawn every frame (pending uploads)
	spinners []spinPrim
	// names are the user mentions laid out and the plugin labels they
	// used (members.go)
	names []nameUse
	// hovers are the primitives drawn only while a card is hovered (a
	// work object card's hovered subtitle, actions bar and Refresh):
	// those with hov == index+1
	hovers []hoverLayer
	// with animations off (animated.go): the stills that play when
	// clicked (PlayPauseAnimations) and attachments' Expand buttons
	plays   []playCtl
	expands []expandBtn
	// bars are the files' FileActions (file_actions.go), pills the
	// snippets' Show more / Show less (snippet.go)
	bars  []fileBar
	pills []snipPill
	// ells counts the frags that are a clamp's "…" (frag.ell)
	ells int
}

// hoverLayer is drawn while the View's hovered target is in card..cardTo
// and outside skip..skipTo (the actions bar's own buttons, which hide
// the hovered subtitle, as Slack's isActionsBarHovered does).
type hoverLayer struct {
	card, cardTo int16
	skip, skipTo int16
	bar          bool // the actions bar itself: shown over its own buttons too
}

// shown reports whether the layer draws with target t hovered.
func (h *hoverLayer) shown(t int16) bool {
	if t < 0 || h.card < 0 || t < h.card || t > h.cardTo {
		return false
	}
	return h.bar || t < h.skip || t > h.skipTo
}

// slot is a run of interactive elements the owner draws (Options.Elements).
type slot struct {
	r   image.Rectangle
	bs  []richtext.Button
	acc bool
}

func (d *display) reset() {
	d.frags = d.frags[:0]
	d.boxes = d.boxes[:0]
	d.imgs = d.imgs[:0]
	d.targets = d.targets[:0]
	d.hits = d.hits[:0]
	d.lines = d.lines[:0]
	d.plain = d.plain[:0]
	d.size = image.Point{}
	d.overhang = 0
	d.comps = d.comps[:0]
	d.slots = d.slots[:0]
	d.tips = d.tips[:0]
	d.codes = d.codes[:0]
	d.fileSecs = d.fileSecs[:0]
	d.removes = d.removes[:0]
	d.cards = d.cards[:0]
	d.overlayIcons = d.overlayIcons[:0]
	d.spinners = d.spinners[:0]
	d.names = d.names[:0]
	d.hovers = d.hovers[:0]
	d.plays = d.plays[:0]
	d.expands = d.expands[:0]
	d.bars = d.bars[:0]
	d.pills = d.pills[:0]
	d.ells = 0
}

func (d *display) addTarget(t target) int16 {
	d.targets = append(d.targets, t)
	return int16(len(d.targets) - 1)
}

// ---------- inline flow ----------

type itemKind uint8

const (
	itWord itemKind = iota
	itSpace
	itImage
	itBreak
	itPad
	// itSlot is the place of a pill's ::before or ::after, which its
	// component draws (addPseudoSlot): laid out like a pad, its position
	// noted
	itSlot
)

// decoMissing and decoMissingGone are MissingText's bar for a user
// mention whose member the store can't name (msgcard.go missingMember):
// the unknown stub's filled one and a non-existent member's outlined one.
const (
	decoMissing     uint8 = 5
	decoMissingGone uint8 = 6
)

type deco struct {
	kind   uint8 // 1 pill, 2 code, 3 highlight, 4 colour swatch, decoMissing, decoMissingGone
	bg     color.NRGBA
	border color.NRGBA
	padL   float32
	padR   float32
	padT   float32
	padB   float32
	radius int
	target int16
	face   faceKey
	self   bool
	comp   int16
	part   uint8
}

type item struct {
	kind   itemKind
	sh     *shaped
	face   faceKey
	w      float32
	col    color.NRGBA
	style  richtext.Style
	deco   int16
	target int16
	url    string
	size   int
	p0, p1 int32
	brk    bool // break opportunity after this item (CJK, images)
	// open marks a pad that opens an element (a pill's start padding): it
	// goes to the next line with what follows it, as an inline box's
	// start edge does. close marks one that closes an element (its end
	// padding, a rule's border and margin after it): it stays on the line
	// of what it closes
	open, close bool
	// atom is an atomic inline box that isn't an image (a pill's box
	// ::before or ::after, the colour swatch): a line may break before it
	// and after it whatever the characters around it, as Chromium's
	// LineBreaker breaks around every atomic inline (CanBreakAfter,
	// CanBreakAfterAtomicInline; the sticky-images quirk is quirks mode's)
	atom bool
	alt  bool
	// own is an atomic inline's own box, which the flow draws in its
	// place: its deco's index+1 (the colour swatch, MissingText's bar), 0
	// none. deco is then the element it is in (inline code's chip), -1 none
	own int16
	// itSlot: which pseudo-element's place it is (0 ::before, 1 ::after)
	slot uint8
	comp int16
	part uint8
	tip  int16
	br   richtext.BrKind // itBreak: how it draws (richtext.BrKind)
}

// flowStyle configures one inline flow.
type flowStyle struct {
	face     font.Font
	size     int // px
	lineH    int // px
	col      color.NRGBA
	emoji    int  // emoji px
	pre      bool // preserve spaces and newlines, mono
	maxLines int
	ellipsis bool
	noSelect bool
	// flatEmoji keeps emoji from making their line taller (Slack's
	// c-emoji__medium in 13px attachment text: a 22px image with -11px
	// margins inside the line box).
	flatEmoji bool
	// paraH is a c-mrkdwn__br's height (richtext.BrPara), 0 for its 8px:
	// 22px straight inside a bordered quote.
	paraH int
}

// lay holds per-layout state.
type lay struct {
	st    *Style
	d     *display
	scale float32 // px per dp
	spx   float32 // px per sp
	jumbo bool
	// lead is where the first line of the doc's first paragraph starts
	// (px): Compact's sender sits inline before it (View.SetLead). The
	// first flow consumes it.
	lead  float32
	items []item
	decos []deco
	// decoLines is each deco's lines in a flow (its fragments' ends,
	// box-decoration-break: slice)
	decoLines []decoSpan
	opts      Options
	// noPlain is set while laying out non-selectable labels.
	noPlain bool
	// mode says which Slack components lay out as such (comps.go); cs is
	// their state from the View, inAtt is set inside an attachment card.
	mode      uint8
	cs        *[]compState
	inAtt     bool
	authorImg int // inside a card: its author icon, imgs index+1
	// expanded holds the attachments whose text was expanded (View
	// state); expandAt is the one being laid out (truncate.go).
	expanded map[*richtext.Attachment]bool
	expandAt *richtext.Attachment
	// expandKey, when set, is the section whose "Show more" is being laid
	// out (its key in kitExpanded).
	expandKey string
	// collapsed holds the image blocks hidden with their toggle (View state).
	collapsed map[*richtext.Image]bool
	// playing holds the stills the viewer played (View state, animated.go);
	// noAnim: Accessibility > "Allow animations" is off
	playing map[string]bool
	noAnim  bool
	// titleFrags is a YouTube card's title frags [from, to) (compBlock).
	titleFrags [2]int
	// media is the card's AttachmentMedia part (fixedMedia), for
	// compBlock to make it its own component
	media mediaNote
	// CSS parts (css.go), nil while no CSS is installed: ctr is the
	// current container's element path, curPart the part new items get.
	css     *cssParts
	ctr     partPath
	curPart uint8
	inBlock bool // inside a .p-rich_text_block
}

func (l *lay) dp(v float32) int      { return int(v*l.scale + 0.5) }
func (l *lay) sp(v float32) int      { return int(v*l.spx + 0.5) }
func (l *lay) dpf(v float32) float32 { return v * l.scale }

func (l *lay) faceFor(base font.Font, sizePx int, s richtext.Style) faceKey {
	f := base
	if s&richtext.Bold != 0 {
		bw := font.Bold
		if l.st.BoldWeight != 0 {
			bw = l.st.BoldWeight
		}
		if f.Weight < bw {
			f.Weight = bw
		}
	}
	if s&richtext.Italic != 0 {
		f.Style = font.Italic
	}
	return faceKey{f: f, size: fixed.I(sizePx)}
}

func (l *lay) plainAdd(s string) (int32, int32) {
	if l.noPlain {
		n := int32(len(l.d.plain))
		return n, n
	}
	p0 := int32(len(l.d.plain))
	l.d.plain = append(l.d.plain, s...)
	return p0, int32(len(l.d.plain))
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// addText splits s into word/space/break items.
func (l *lay) addText(s string, fs *flowStyle, k faceKey, col color.NRGBA, st richtext.Style, dc, tg int16) {
	segs := splitEmoji(s)
	if segs == nil {
		l.addTextRun(s, fs, k, col, st, dc, tg)
		return
	}
	for _, sg := range segs {
		if sg.stem == "" {
			l.addTextRun(sg.text, fs, k, col, st, dc, tg)
			continue
		}
		p0, p1 := l.plainAdd(sg.text)
		ep := fs.emoji
		l.items = append(l.items, item{kind: itImage, face: k, w: float32(ep + 2*l.dp(1)), url: l.st.emojiURL(sg.stem), size: ep,
			deco: dc, target: tg, p0: p0, p1: p1, brk: true, alt: true, part: l.curPart})
	}
}

// addTextRun splits emoji-free text into word/space/break items.
func (l *lay) addTextRun(s string, fs *flowStyle, k faceKey, col color.NRGBA, st richtext.Style, dc, tg int16) {
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		w := s[start:end]
		sh := l.st.word(k, w)
		p0, p1 := l.plainAdd(w)
		l.items = append(l.items, item{kind: itWord, sh: sh, face: k, w: sh.adv, col: col, style: st, deco: dc, target: tg, p0: p0, p1: p1, part: l.curPart})
		start = -1
	}
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\n':
			flush(i)
			p0, p1 := l.plainAdd("\n")
			l.items = append(l.items, item{kind: itBreak, face: k, deco: dc, p0: p0, p1: p1})
		case r == ' ' || r == '\t' || r == '\r' || (r != ' ' && unicode.IsSpace(r)):
			flush(i)
			n := 1
			if r == '\t' {
				n = 4
				if !fs.pre {
					n = 1
				}
			}
			if r == '\r' {
				break
			}
			sp := l.st.spaceOf(k)
			// Collapse runs of spaces outside pre/rich text? Slack uses
			// pre-wrap everywhere, so spaces are preserved.
			p0, p1 := l.plainAdd(s[i : i+sz])
			l.items = append(l.items, item{kind: itSpace, face: k, w: sp.adv * float32(n), col: col, style: st, deco: dc, target: tg, p0: p0, p1: p1, part: l.curPart})
		case isCJK(r):
			flush(i)
			start = i
			flush(i + sz)
			l.items[len(l.items)-1].brk = true
		case r == '-' && start >= 0 && i+1 < len(s) && s[i+1] != '-' && s[i+1] != ' ':
			// Browsers may break after a hyphen inside a word.
			flush(i + sz)
			l.items[len(l.items)-1].brk = true
		default:
			if start < 0 {
				start = i
			}
		}
		i += sz
	}
	flush(len(s))
}

func (l *lay) addPad(w float32, dc int16, k faceKey) {
	l.items = append(l.items, item{kind: itPad, w: w, deco: dc, face: k, target: -1, part: l.curPart})
}

// addOpen adds a pad that opens an element (a pill's start padding, a
// rule's margin and border before it): it stays with what follows, so a
// line never ends with an element's start (Chromium's LineBreaker gives an
// open tag no break opportunity after it: HandleOpenTag).
func (l *lay) addOpen(w float32, dc int16, k faceKey) {
	l.items = append(l.items, item{kind: itPad, w: w, deco: dc, face: k, target: -1, part: l.curPart, open: true})
}

// addClose adds a pad that closes an element (its end padding, a rule's
// border and margin after it): a break after the element's last content
// comes after it ("a break opportunity before a close tag always
// propagates to after the close tag", LineBreaker::HandleCloseTag).
func (l *lay) addClose(w float32, dc int16, k faceKey) {
	l.items = append(l.items, item{kind: itPad, w: w, deco: dc, face: k, target: -1, part: l.curPart, close: true})
}

// addPseudoSlot keeps the place, w px wide inside deco dc, of a pill's
// ::before (which 0) or ::after (1), which its component draws there
// (comps.go): the flow lays it out like a pad and notes where it went.
// box is a box pseudo-element (an inline-block: MentionProfiles'
// picture), an atomic inline the line may break before and after; text
// content flows with the text around it.
func (l *lay) addPseudoSlot(which uint8, w float32, box bool, dc int16, k faceKey, col color.NRGBA) {
	l.items = append(l.items, item{kind: itSlot, w: w, deco: dc, face: k, col: col, target: -1, part: l.curPart, slot: which, atom: box})
}

// addSpans converts spans into items.
func (l *lay) addSpans(spans []richtext.Span, fs *flowStyle) {
	ctrPart := l.curPart
	if l.css != nil {
		defer func() { l.curPart = ctrPart }()
	}
	p := &l.st.Palette
	for i := range spans {
		sp := &spans[i]
		st := sp.Style
		var pt uint8
		if l.css != nil {
			pt = l.spanPart(sp)
			l.curPart = pt
		}
		col := fs.col
		size := fs.size
		base := fs.face
		var dc int16 = -1
		var tg int16 = -1
		if st&richtext.Code != 0 && !fs.pre {
			base = font.Font{Typeface: l.st.Mono}
			size = l.sp(12)
			col = p.CodeText
		}
		k := l.faceFor(base, size, st)
		if pt != 0 {
			k = l.partFace(k, pt)
		}
		noAt := false
		var swatch color.NRGBA // a colour element's swatch, after its value
		// a pill's component state (compState: its ::before and ::after,
		// the margin and border a rule gives its ends); pill: it has one
		var ps compState
		pill := false
		n0 := len(l.items)
		var cmp int16 // a mention pill laid out as a Slack component
		txt := sp.Text
		switch sp.Kind {
		case richtext.Break:
			p0, p1 := l.plainAdd("\n")
			l.items = append(l.items, item{kind: itBreak, face: k, deco: -1, target: -1, p0: p0, p1: p1, br: sp.Br})
			continue
		case richtext.Emoji:
			if sp.URL == "" {
				txt := sp.Text
				if txt == "" {
					txt = ":" + sp.ID + ":"
				}
				l.addText(txt, fs, k, col, st, -1, -1)
				continue
			}
			// the copy text is the image's data-stringify-emoji, the
			// Emoji's text prop: ":name:" (with ::skin-tone-N), not the
			// unicode character
			alt := sp.Text
			if sp.ID != "" {
				alt = ":" + sp.ID + ":"
			}
			p0, p1 := l.plainAdd(alt)
			ep := fs.emoji
			var tip int16
			if sp.ID != "" && !l.opts.NoEmojiTips { // Slack's emoji tooltip: the :name: (with ::skin-tone-N)
				l.d.tips = append(l.d.tips, ":"+sp.ID+":")
				tip = int16(len(l.d.tips))
			}
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ep + 2*l.dp(1)), url: sp.URL, size: ep, deco: -1, target: -1, p0: p0, p1: p1, brk: true, alt: true, tip: tip, part: pt})
			continue
		case richtext.InlineImage:
			if sp.URL == "" {
				break
			}
			p0, p1 := l.plainAdd(sp.Text)
			ip := max(fs.emoji, l.dp(16))
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ip + 2*l.dp(1)), url: sp.URL, size: ip, deco: -1, target: -1, p0: p0, p1: p1, brk: true, part: pt})
			continue
		case richtext.ColorSwatch:
			if c := parseColor(sp.Text, color.NRGBA{}); c.A > 0 && !uipref.Get().HideHexSwatch {
				swatch = c // after the value
			}
		case richtext.Link:
			if sp.Truncated && sp.URL != expandURL {
				l.truncatedLink(sp, fs, k, pt) // msgcard.go
				continue
			}
			if sp.URL == expandURL && l.expandKey != "" { // section Show more / Show less (truncate.go)
				tg = l.d.addTarget(target{kind: tExpand, id: l.expandKey, underline: true})
			} else if sp.URL == expandURL { // attachment Show more / Show less (truncate.go)
				tg = l.d.addTarget(target{kind: tExpand, att: l.expandAt, underline: true})
			} else {
				tg = l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true, website: true, urlCard: true})
			}
			if st&richtext.Code == 0 {
				col = p.Link
			}
		case richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast:
			if sp.Kind == richtext.User && sp.Hidden {
				l.hiddenMember(sp, fs, k, pt) // msgcard.go
				continue
			}
			kind := tUser
			switch sp.Kind {
			case richtext.Channel:
				kind = tChannel
			case richtext.Usergroup:
				kind = tUsergroup
			case richtext.User:
				// the member as plugins present it (Nicknames); one the
				// store can't name is MemberSlug's BaseMissingMember, and
				// RollupName's "" in a system message (Muted)
				var miss MemberMiss
				if txt, miss = l.mentionText(sp.ID, txt); miss != MemberLoaded && !l.opts.Muted {
					l.missingMember(miss, k, col) // msgcard.go
					continue
				}
			}
			if l.opts.Muted && sp.Kind == richtext.User {
				// System messages show member names as plain text that
				// underlines on hover (Slack's rollup_member).
				tg = l.d.addTarget(target{kind: kind, id: sp.ID, underline: true})
				noAt = true
				break
			}
			if sp.Kind == richtext.Channel && sp.Off && sp.Lock {
				// BaseMissingChannel's Nonexistent chip (richtext channel.go)
				l.privateChannel(sp, fs, k, pt)
				continue
			}
			if sp.Kind == richtext.Channel && sp.URL != "" && !sp.Off {
				// a channel-tab slug (tab_id): the link opens the tab
				tg = l.d.addTarget(target{kind: tLink, url: sp.URL, id: sp.ID, tab: tabOfSlug(sp.URL)})
			} else if sp.Kind != richtext.Broadcast && !sp.Off { // Off: a channel the viewer can't see, not a link
				tg = l.d.addTarget(target{kind: kind, id: sp.ID, self: sp.Self})
			} // a hidden channel with a fallback name: plain text, no card (BaseMrkdwnChannel returns the span before the trigger)
			bg, fg := p.MentionBg, p.MentionText
			if sp.Self {
				// c-member_slug--mention / c-mrkdwn__user_group--mention
				// over the --link look: surf-hgl-3, the link color (content-
				// hgl-3 in dark)
				bg, fg = p.SelfMentionBg, p.SelfLinkText
			}
			if sp.Kind == richtext.Broadcast {
				bg, fg = p.SelfMentionBg, p.SelfMentionText
				k = l.faceFor(base, size, st|richtext.Bold)
				if pt != 0 {
					k = l.partFace(k, pt)
				}
			}
			col = fg
			pad, padV := l.dpf(2), l.dpf(1)
			if fs.pre || st&richtext.Code != 0 {
				pad, padV = 0, 0
			}
			l.decos = append(l.decos, deco{kind: 1, bg: bg, padL: pad, padR: pad, padT: padV, padB: padV, radius: l.dp(3), target: tg, face: k, self: sp.Self || sp.Kind == richtext.Broadcast, part: pt})
			dc = int16(len(l.decos) - 1)
			if l.mode&modePills != 0 && !l.inAtt && (sp.Kind == richtext.User || sp.Kind == richtext.Channel) {
				kind := compUser
				if sp.Kind == richtext.Channel {
					kind = compChannel
				}
				cmp = l.addComp(compBox{kind: kind, span: *sp, em: float32(k.size) / 64})
				l.decos[dc].comp = cmp
				if n := l.compState(cmp).name; n != "" && kind == compChannel {
					txt = n
					if !sp.Lock {
						txt = "#" + n
					}
				}
				if kind == compUser && l.compState(cmp).noAt {
					noAt = true // MemberSlug supressMentionAtSymbol
				}
				ps, pill = *l.compState(cmp), true
			}
		case richtext.MessageMention:
			l.messageMention(sp, fs, k, pt)
			continue
		case richtext.Canvas:
			l.canvasSlug(sp, fs, k, pt)
			continue
		case richtext.ChannelTab:
			l.channelTabSlug(sp, fs, k, pt) // canvasslug.go
			continue
		case richtext.Date:
			if sp.URL != "" {
				tg = l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true})
				col = p.Link
			}
		}
		if st&richtext.Code != 0 && !fs.pre && dc < 0 {
			l.decos = append(l.decos, deco{kind: 2, bg: p.CodeBg, border: p.CodeBorder, padL: l.dpf(3), padR: l.dpf(3), padT: l.dpf(2), padB: l.dpf(1), radius: l.dp(3), target: tg, face: k, part: pt})
			dc = int16(len(l.decos) - 1)
		} else if st&richtext.FindHighlight != 0 && dc < 0 {
			// over a keyword highlight: only the find span draws
			bg := p.FindHighlight
			if l.opts.FindFocused {
				bg = p.FindFocused
			}
			l.decos = append(l.decos, deco{kind: 3, bg: bg, face: k, target: -1, part: pt})
			dc = int16(len(l.decos) - 1)
		} else if st&richtext.Highlight != 0 && dc < 0 {
			l.decos = append(l.decos, deco{kind: 3, bg: p.Highlight, face: k, target: -1, part: pt})
			dc = int16(len(l.decos) - 1)
		}
		if pill && ps.edge[0] != 0 {
			l.addOpen(ps.edge[0], -1, k) // a rule's margin and border before the pill's box
		}
		if dc >= 0 && l.decos[dc].padL > 0 {
			l.addOpen(l.decos[dc].padL, dc, k)
		}
		if pill && ps.pseudo[0] > 0 && dc >= 0 {
			// the slug's ::before (MentionProfiles' picture) inside the pill,
			// first in it: its place goes on the pill's first line, where
			// the component draws it (comps.go)
			l.addPseudoSlot(0, ps.pseudo[0], ps.pbox[0], dc, k, col)
		}
		if sp.Kind == richtext.Channel && sp.Lock {
			// a private channel the viewer is in: lock-filled, then its
			// name without "#"
			l.addIcon("lock-filled", fs.size, k, col, dc, tg)
		}
		if noAt {
			txt = strings.TrimPrefix(txt, "@")
		}
		if !fs.pre && st&richtext.Code != 0 {
			// Inline code wraps normally.
			txt = strings.ReplaceAll(txt, "\n", " ")
		}
		c0 := int32(len(l.d.plain))
		l.addText(txt, fs, k, col, st, dc, tg)
		if !fs.pre && st&richtext.Code != 0 {
			l.addCodeRun(c0, int32(len(l.d.plain)))
		}
		if pill && ps.pseudo[1] > 0 && dc >= 0 {
			l.addPseudoSlot(1, ps.pseudo[1], ps.pbox[1], dc, k, col) // its ::after, last in it
		}
		if swatch.A > 0 && dc >= 0 && l.decos[dc].kind == 2 {
			// a code-styled colour: WithFormattingStyles (UqHX) wraps
			// Color's children, the value and its swatch, in the code
			// element, so the swatch sits inside the chip, before its end
			// padding
			l.addSwatch(swatch, dc, col, k)
			swatch = color.NRGBA{}
		}
		if dc >= 0 && l.decos[dc].padR > 0 {
			l.addClose(l.decos[dc].padR, dc, k)
		}
		if pill && ps.edge[1] != 0 {
			l.addClose(ps.edge[1], -1, k) // a rule's border and margin after it
		}
		if cmp != 0 {
			for i := n0; i < len(l.items); i++ {
				l.items[i].comp = cmp
			}
			if ex := l.compState(cmp).extra; ex != 0 {
				l.addClose(float32(ex), -1, k) // the plugin's pill is wider/narrower
			}
		}
		if swatch.A > 0 {
			l.addSwatch(swatch, -1, col, k)
		}
	}
}

// addSwatch adds rich text's Color swatch (boot-shared d7OQ) after its
// value, inside deco in (inline code's chip; -1 none): div.c-mrkdwn__hex,
// an inline-block 13px square (content-box) in a 1px
// --sk_foreground_max_solid border, radius 3, margin 0 0 -1px 2px, its
// bottom 1px under the baseline. An atomic inline: a line may break
// before and after it, and holding only it a line is a full line box
// (the strut's). col is the color it inherits (a clamp's "…" after it
// takes it).
func (l *lay) addSwatch(c color.NRGBA, in int16, col color.NRGBA, k faceKey) {
	l.decos = append(l.decos, deco{kind: 4, bg: c, border: l.st.Palette.HexBorder, face: k, target: -1, radius: l.dp(3)})
	l.items = append(l.items, item{kind: itPad, w: l.dpf(2) + float32(l.dp(13)+2*l.dp(1)), deco: in, own: int16(len(l.decos)), face: k,
		col: col, target: -1, part: l.curPart, atom: true})
}

// flow lays out l.items at (x0, y0) within maxW px. Returns the height and
// the widest line width.
func (l *lay) flow(x0, y0, maxW int, fs *flowStyle) (h, wmax int) {
	items := l.items
	// Drop one trailing hard break (pre-wrap does not render it). A
	// rich_text <br> (BrLine) stays: it ends its line like any other, and
	// the empty line after it is never finished, so "a<br>" is one line
	// and "a<br><br>" two, as CSS draws them.
	if n := len(items); n > 0 && items[n-1].kind == itBreak && items[n-1].br == richtext.BrPlain {
		items = items[:n-1]
	}
	if len(items) == 0 {
		return 0, 0
	}
	base := l.faceFor(fs.face, fs.size, 0)
	bm := l.st.spaceOf(base)
	lineH := fs.lineH
	// Baseline offset in a CSS line box: half-leading + ascent.
	baseOff := (float32(lineH)-(bm.asc+bm.desc))/2 + bm.asc

	d := l.d
	y := y0
	lines := 0
	x := float32(0)
	lineStart := len(d.frags)
	lineH0 := lineH
	maxWf := float32(maxW)
	truncated := false
	// Compact's inline sender holds the first line's start: that line is
	// a full line even when its first word wraps to the next.
	leadLine := l.lead > 0
	x, l.lead = l.lead, 0

	// Decorations over spaces (frag.ulT, ulU): last is this line's last
	// frag, endT/endU where the spaces after it that share its target /
	// its underline-strike style reach (broken by a pad or another space).
	last := -1
	var endT, endU float32
	brokeT, brokeU := false, false
	gapReset := func() { endT, endU, brokeT, brokeU = 0, 0, false, false }
	const lineDeco = richtext.Underline | richtext.Strike

	// brH is the height of an empty line the next finishLine ends at a
	// rich_text break: a <br>'s full line box, a c-mrkdwn__br's block.
	// 0 is the 8px empty line between two plain breaks.
	brH := 0
	paraH := fs.paraH
	if paraH == 0 {
		paraH = l.dp(8) // .c-mrkdwn__br{height:8px;display:block}
	}

	// Pass: chunk items into unbreakable runs separated by spaces/breaks.
	var decoRuns []placedRun // only for items with deco >= 0
	var slots []placedRun    // pseudo-element places (itSlot)
	var atoms []placedRun    // atomic inlines drawn in their place (item.own)
	// kids is what a flow that may end in "…" placed, in order (words,
	// spaces, pads, places, images), for ellipsize to find the last line's
	// children
	var kids []placedRun
	clamp := fs.ellipsis && fs.maxLines > 0
	note := func(q, fi int, x, w float32) {
		if clamp {
			kids = append(kids, placedRun{item: q, frag: fi, x: x, w: w, line: len(d.lines)})
		}
	}
	// cutAt is the first item a truncated flow didn't lay out (whole): a
	// deco with items from there on goes on past the truncation
	cutAt := len(items)
	// trail is where this line's last run of decorated spaces starts in
	// decoRuns (-1: none): spaces that end a line collapse (white-space:
	// normal, as Slack's message text is), so a pill or a highlight that
	// wraps after one ends at its last word, flush, as Chromium paints it
	trail := -1
	// atomLine: a pseudo-element's place or an atomic inline is on this
	// line, which isn't empty without a frag (a pill's picture before
	// "@name" that went on, a colour's swatch that wrapped after its
	// value): a line box with an inline-block in it has the strut's height
	atomLine := false
	finishLine := func() {
		last = -1 // spaces at the end of a line are never decorated
		gapReset()
		if trail >= 0 {
			decoRuns = decoRuns[:trail]
			trail = -1
		}
		// Compute this line's height: emoji may be taller.
		lh := lineH0
		if lineStart == len(d.frags) && !fs.pre && !leadLine && !atomLine {
			lh = l.dp(8) // an empty line between paragraphs
			if brH != 0 {
				lh = brH
			}
		}
		brH = 0
		leadLine, atomLine = false, false
		for i := lineStart; i < len(d.frags); i++ {
			if f := &d.frags[i]; f.kind == fImage {
				if h := f.size + l.dp(4); f.size > lineH0 && h > lh && !fs.flatEmoji {
					lh = h
				}
			}
		}
		off := baseOff + float32(lh-lineH0)/2
		for i := lineStart; i < len(d.frags); i++ {
			f := &d.frags[i]
			f.top, f.bot = y, y+lh
			f.line = int32(len(d.lines))
			if f.kind == fText {
				f.y = y + int(off+0.5)
			} else {
				f.y = y + (lh-f.size)/2
			}
		}
		d.lines = append(d.lines, lineInfo{top: y, bot: y + lh, f0: lineStart, f1: len(d.frags)})
		if w := int(x + 0.999); w > wmax {
			wmax = w
		}
		y += lh
		lines++
		x = 0
		lineStart = len(d.frags)
	}

	emit := func(it *item, sh *shaped, w float32, p0, p1 int32) {
		if last >= 0 { // the spaces before this frag: decorated by the previous one
			f := &d.frags[last]
			if end := f.x + f.w; endT > end {
				f.ulT = endT - end
			}
			if end := f.x + f.w; endU > end {
				f.ulU = endU - end
			}
		}
		gapReset()
		trail = -1
		last = len(d.frags)
		if it.kind == itImage {
			d.frags = append(d.frags, frag{kind: fImage, x: float32(x0) + x + l.dpf(1), w: float32(it.size), size: it.size, url: it.url, target: it.target, p0: p0, p1: p1, alt: it.alt, comp: it.comp, tip: it.tip, part: it.part, col: it.col})
		} else if it.kind == itWord {
			d.frags = append(d.frags, frag{kind: fText, x: float32(x0) + x, w: w, size: int(it.face.size >> 6), sh: sh, col: it.col, style: it.style, target: it.target, p0: p0, p1: p1, comp: it.comp,
				part: it.part, boxed: it.deco >= 0, weight: it.face.f.Weight, tip: it.tip})
		}
		x += w
	}

	i := 0
	for i < len(items) && !truncated {
		it := &items[i]
		switch it.kind {
		case itBreak:
			switch it.br {
			case richtext.BrNone: // swallowed by Slack's placeholders
				i++
				continue
			case richtext.BrLine:
				brH = lineH0
			case richtext.BrPara:
				// the block ends the line it follows (a <br> before
				// it already did), then takes its own height
				if x > 0 || lineStart < len(d.frags) {
					brH = lineH0
					finishLine()
					if fs.maxLines > 0 && lines >= fs.maxLines {
						truncated, cutAt = i < len(items)-1, i+1
						i++
						continue
					}
				}
				brH = paraH
			}
			finishLine()
			if fs.maxLines > 0 && lines >= fs.maxLines {
				truncated, cutAt = i < len(items)-1, i+1
			}
			i++
			continue
		case itSpace:
			if x > 0 || fs.pre {
				if x+it.w <= maxWf || fs.pre {
					if it.deco >= 0 {
						if trail < 0 && !fs.pre {
							trail = len(decoRuns)
						}
						decoRuns = append(decoRuns, placedRun{item: i, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
					}
					if last >= 0 {
						f := &d.frags[last]
						end := float32(x0) + x + it.w
						if !brokeT && it.target >= 0 && it.target == f.target {
							endT = end
						} else {
							brokeT = true
						}
						if !brokeU && it.style&lineDeco != 0 && it.style&lineDeco == f.style&lineDeco {
							endU = end
						} else {
							brokeU = true
						}
					}
					note(i, -1, float32(x0)+x, it.w)
					x += it.w
				} // else: a space that does not fit is swallowed by the wrap

			}
			i++
			continue
		}
		// Gather a chunk: consecutive words/pads/images until a break opportunity.
		j := i
		cw := float32(0)
		for j < len(items) {
			it := &items[j]
			if it.kind == itSpace || it.kind == itBreak {
				break
			}
			if j > i && (it.kind == itImage || it.atom) {
				// a break opportunity before an image or another atomic
				// inline (a pill's picture), whatever precedes it: Chromium's
				// LineBreaker::CanBreakAfter breaks a text item before any
				// atomic inline it finds after the element's zero-length open
				// tags, "even when the adjacent character is U+00A0". The pads
				// opening the element it starts (a pill's padding) go to the
				// next line with it, as the open tag does.
				q := j
				for q > i && items[q-1].kind == itPad && items[q-1].open {
					q--
				}
				if q > i {
					for p := q; p < j; p++ {
						cw -= items[p].w
					}
					j = q
					break
				}
			}
			cw += it.w
			j++
			if it.brk || it.atom {
				// a break opportunity after it (an atomic inline always has
				// one: CanBreakAfterAtomicInline), after the pads that close
				// its element (a pill's end padding), which stay on its line
				for j < len(items) && items[j].kind == itPad && items[j].close {
					cw += items[j].w
					j++
				}
				break
			}
		}
		if x > 0 && x+cw > maxWf {
			finishLine()
			if fs.maxLines > 0 && lines >= fs.maxLines {
				truncated, cutAt = true, i
				break
			}
		}
		for q := i; q < j && !truncated; q++ {
			it := &items[q]
			if it.kind == itPad || it.kind == itSlot {
				if it.deco >= 0 {
					decoRuns = append(decoRuns, placedRun{item: q, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
				}
				if it.kind == itSlot {
					slots = append(slots, placedRun{item: q, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines)})
				}
				if it.own > 0 {
					atoms = append(atoms, placedRun{item: q, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.own - 1})
				}
				if it.kind == itSlot || it.atom {
					atomLine = true
				}
				note(q, -1, float32(x0)+x, it.w)
				trail = -1
				brokeT, brokeU = true, true // a pill's padding, an icon's margin
				x += it.w
				continue
			}
			if it.kind == itWord && x+it.w > maxWf && it.w > 0 {
				// Forced break inside a word that cannot fit.
				sh := it.sh
				p0 := it.p0
				for sh != nil {
					room := maxWf - x
					if sh.adv <= room {
						if it.deco >= 0 {
							decoRuns = append(decoRuns, placedRun{item: q, frag: len(d.frags), x: float32(x0) + x, w: sh.adv, line: len(d.lines), decoI: it.deco})
						}
						note(q, len(d.frags), float32(x0)+x, sh.adv)
						emit(it, sh, sh.adv, p0, it.p1)
						break
					}
					if x > 0 {
						finishLine()
						if fs.maxLines > 0 && lines >= fs.maxLines {
							truncated, cutAt = true, q
							break
						}
						continue
					}
					head, tail, nb := l.st.split(it.face, sh, room)
					if it.deco >= 0 {
						decoRuns = append(decoRuns, placedRun{item: q, frag: len(d.frags), x: float32(x0) + x, w: head.adv, line: len(d.lines), decoI: it.deco})
					}
					p1 := min(p0+int32(nb), it.p1) // labels without plain text keep empty ranges
					note(q, len(d.frags), float32(x0)+x, head.adv)
					emit(it, head, head.adv, p0, p1)
					p0 = p1
					sh = tail
					if sh != nil {
						finishLine()
						if fs.maxLines > 0 && lines >= fs.maxLines {
							truncated, cutAt = true, q
							break
						}
					}
				}
				continue
			}
			if it.deco >= 0 {
				decoRuns = append(decoRuns, placedRun{item: q, frag: len(d.frags), x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
			}
			note(q, len(d.frags), float32(x0)+x, it.w)
			emit(it, it.sh, it.w, it.p0, it.p1)
		}
		i = j
	}
	if !truncated && (x > 0 || lineStart < len(d.frags)) {
		finishLine()
	} else if truncated && lineStart < len(d.frags) {
		finishLine()
	}
	if truncated && fs.ellipsis && len(d.lines) > 0 {
		// "…" hides the children after it (their frags, places and atomic
		// inlines go); the inline boxes keep the extent they were laid out
		// with, under it, as Chromium's TruncateLine leaves the boxes'
		// rects (ComputeInlinePositions ran before it)
		last := len(d.lines) - 1
		l.ellipsize(&d.lines[last], last, x0, maxWf, fs, items, kids, &slots, &atoms)
	}

	// Each deco's first and last line, and whether it goes on past a
	// truncation: a pill that wraps is a box per line, the start edge's
	// rounded corners on its first, the end edge's on its last, square
	// where it broke, as Chromium paints an inline box over lines
	// (box-decoration-break: slice; Slack sets no clone). Inline code
	// slices the same way, its border open where it broke.
	dl := l.decoLines[:0]
	for len(dl) < len(l.decos) {
		dl = append(dl, decoSpan{first: -1, last: -1})
	}
	for _, r := range decoRuns {
		e := &dl[r.decoI]
		if e.first < 0 || r.line < e.first {
			e.first = r.line
		}
		e.last = max(e.last, r.line)
	}
	if truncated {
		for q := cutAt; q < len(items); q++ {
			if dc := items[q].deco; dc >= 0 && int(dc) < len(dl) {
				dl[dc].goesOn = true
			}
		}
	}
	l.decoLines = dl
	b0 := len(d.boxes)

	// Decoration rects: merge consecutive runs of the same deco on a line.
	for a := 0; a < len(decoRuns); {
		r := decoRuns[a]
		b := a + 1
		x1 := r.x + r.w
		for b < len(decoRuns) && decoRuns[b].decoI == r.decoI && decoRuns[b].line == r.line {
			x1 = decoRuns[b].x + decoRuns[b].w
			b++
		}
		if r.line < len(d.lines) {
			dc := &l.decos[r.decoI]
			ln := d.lines[r.line]
			fm := l.st.spaceOf(dc.face)
			// content area of the decoration's font, centered on the baseline
			bl := float32(ln.top) + baseOff + float32((ln.bot-ln.top)-lineH0)/2
			top := bl - fm.asc - dc.padT
			bot := bl + fm.desc + dc.padB
			rr := image.Rect(int(r.x+0.5), int(top+0.5), int(x1+0.5), int(bot+0.5))
			switch dc.kind {
			case 2:
				bx := box{kind: bBorder, r: rr, radius: dc.radius, fill: dc.bg, stroke: dc.border, bw: l.dp(1), target: -1, part: dc.part, inline: true}
				dl[r.decoI].slice(&bx, r.line, dc.radius)
				d.boxes = append(d.boxes, bx)
			default:
				bx := box{kind: bFill, r: rr, radius: dc.radius, fill: dc.bg, target: dc.target, comp: dc.comp, part: dc.part, inline: true}
				if dc.kind == 1 && !dc.self && !l.d.cardOnly(dc.target) {
					bx.hover, bx.hoverOver = l.st.Palette.MentionHoverBg, true
				}
				if dc.kind == 1 {
					dl[r.decoI].slice(&bx, r.line, dc.radius)
				}
				d.boxes = append(d.boxes, bx)
			}
			if dc.target >= 0 && !d.cardOnly(dc.target) {
				d.hits = append(d.hits, hit{r: rr, t: dc.target})
			}
		}
		a = b
	}
	// The atomic inlines the flow draws in their places, over the box of
	// the element they are in (a code chip's)
	for _, r := range atoms {
		if r.line >= len(d.lines) {
			continue
		}
		dc := &l.decos[r.decoI]
		ln := d.lines[r.line]
		bl := float32(ln.top) + baseOff + float32((ln.bot-ln.top)-lineH0)/2
		switch dc.kind {
		case 4:
			// the swatch: 2px in from its margin, 13px plus its border,
			// its bottom 1px under the baseline
			sw := l.dp(13) + 2*l.dp(1)
			x := int(r.x+0.5) + l.dp(2)
			bottom := int(bl+0.5) + l.dp(1)
			d.boxes = append(d.boxes, box{kind: bBorder, r: image.Rect(x, bottom-sw, x+sw, bottom), radius: dc.radius, fill: dc.bg, stroke: dc.border, bw: l.dp(1), target: -1})
		case decoMissing, decoMissingGone:
			// MissingText's bar (missingMember): 8px tall, its bottom on
			// the baseline, the non-existent one outlined
			base := int(bl + 0.5)
			bx := box{kind: bFill, r: image.Rect(int(r.x+0.5), base-l.dp(8), int(r.x+r.w+0.5), base), radius: dc.radius, fill: dc.bg, target: -1}
			if dc.kind == decoMissingGone {
				bx.kind, bx.stroke, bx.bw = bBorder, dc.border, max(1, l.dp(1))
			}
			d.boxes = append(d.boxes, bx)
		}
	}

	// Where the pseudo-elements' places went (a pill's ::before on its
	// first line, its ::after on its last), from the pill's box on that
	// line: the component draws them there (comps.go), on the line's
	// baseline, wherever later steps move the box (a table cell's
	// alignment, a card's centring: pillRects resolves them).
	for _, sl := range slots {
		it := &items[sl.item]
		if it.comp <= 0 || int(it.comp) > len(d.comps) || sl.line >= len(d.lines) {
			continue
		}
		nth, bi := int16(0), -1
		for i := b0; i < len(d.boxes); i++ {
			bx := &d.boxes[i]
			if bx.comp != it.comp || bx.kind != bFill {
				continue
			}
			if cy := (bx.r.Min.Y + bx.r.Max.Y) / 2; cy >= d.lines[sl.line].top && cy < d.lines[sl.line].bot {
				bi = i
				break
			}
			nth++
		}
		if bi < 0 {
			continue
		}
		ln := d.lines[sl.line]
		fm := l.st.spaceOf(it.face)
		base := ln.top + int(baseOff+float32((ln.bot-ln.top)-lineH0)/2+0.5)
		o := d.boxes[bi].r.Min
		d.comps[it.comp-1].slots[it.slot] = pseudoSlot{ok: true, nth: nth, dx: sl.x - float32(o.X), dBase: float32(base - o.Y),
			dTop: float32(ln.top - o.Y), dBot: float32(ln.bot - o.Y), asc: fm.asc, desc: fm.desc, size: float32(it.face.size) / 64}
	}
	l.items = l.items[:0]
	l.decos = l.decos[:0]
	return y - y0, wmax
}

// placedRun is an item the flow placed: its x and width on its line (and
// its frag, -1 for pads and spaces), for the deco boxes and places.
type placedRun struct {
	item  int
	frag  int // frag index or -1 for pads/spaces
	x     float32
	w     float32
	line  int
	decoI int16
}

// decoSpan is the lines a deco runs over in a flow, and whether it goes
// on past a truncation (its box on the flow's last line isn't its end).
type decoSpan struct {
	first, last int
	goesOn      bool
}

// slice gives bx, the deco's box on line, Chromium's corners and sides for
// an inline box over lines: its start edge (NW SW, a left border) only on
// its first line, its end edge (NE SE, a right border) only on its last
// and not when it goes on past a truncation.
func (e decoSpan) slice(bx *box, line, radius int) {
	start, end := line == e.first, line == e.last && !e.goesOn
	if start && end {
		return
	}
	bx.perCorner = true
	if start {
		bx.corners[0], bx.corners[3] = radius, radius // NW, SW
	} else {
		bx.cut |= cutStart
	}
	if end {
		bx.corners[1], bx.corners[2] = radius, radius // NE, SE
	} else {
		bx.cut |= cutEnd
	}
}

// ellipsize ends the last line (index line) with "…" as Chromium's
// LineTruncator::TruncateLine does for a line clamp (line_truncator.cc).
// From the line's end, each child (a run of text, an image, an atomic
// inline: a pill's picture in its place, a colour's swatch) is kept with
// "…" right after it when both fit (EllipsizeChild). A text run that
// doesn't is cut to the characters that fit, mid-word and spaces counted
// (TruncateChild's OffsetToFit); an atomic child is never cut. A child
// with nothing kept is hidden, and so is everything after the one "…"
// follows. The line's first child keeps at least its first character, or
// whole if atomic, with "…" after it even when they overflow. After an
// atomic child "…" goes its left margin past the child's margin box
// (PlaceEllipsisNextTo: its border box's offset plus its margin box's
// width) in its color when it is a block flow (an inline-block); after
// text it is in the line's style. The inline boxes keep their extent
// (TruncateLine changes no box's rect), the hidden frags, places (slots)
// and atomic inlines (atoms) go. kids is what the flow placed.
func (l *lay) ellipsize(ln *lineInfo, line, x0 int, maxW float32, fs *flowStyle, items []item, kids []placedRun, slots, atoms *[]placedRun) {
	d := l.d
	k := l.faceFor(fs.face, fs.size, 0)
	ell := l.st.word(k, "…")
	end := float32(x0) + maxW - ell.adv // where a child must end for "…" to fit after it
	for len(kids) > 0 && kids[0].line < line {
		kids = kids[1:]
	}
	// the spaces that end the line collapse (white-space: normal)
	for len(kids) > 0 && items[kids[len(kids)-1].item].kind == itSpace && !fs.pre {
		kids = kids[:len(kids)-1]
	}
	child := func(r *placedRun) bool {
		it := &items[r.item]
		return it.kind != itPad || it.own > 0 // an inline box's padding or edge isn't one
	}
	first := -1
	for i := range kids {
		if child(&kids[i]) {
			first = i
			break
		}
	}
	ex, col := float32(x0), fs.col
	keep := -1     // kids[:keep+1] stay; -1: none
	var cutF *frag // a word cut to fit, its head and the head's bytes
	var cutSh *shaped
	var cutNb int
	for i := len(kids) - 1; i >= 0 && keep < 0; i-- {
		r := &kids[i]
		if !child(r) {
			continue
		}
		it := &items[r.item]
		if it.kind == itWord || it.kind == itSpace {
			// a run of text: its words and the spaces between them, of
			// one element and style
			j := i
			for j > 0 && sameRun(&items[kids[j-1].item], it) {
				j--
			}
			for q := i; q >= j && keep < 0; q-- {
				s := &kids[q]
				if s.x+s.w <= end {
					keep, ex = q, s.x+s.w
					break
				}
				if items[s.item].kind != itWord || s.frag < 0 {
					continue // a space: one character, there or not
				}
				f := &d.frags[s.frag]
				if f.kind != fText || f.sh == nil {
					continue
				}
				// measured in the word's own shape, as TruncateText keeps a
				// view of the child's shape result (the head shaped alone can
				// be a fraction wider)
				if head, _, nb := l.st.split(items[s.item].face, f.sh, end-f.x); head != f.sh {
					if hw := f.sh.caretX(utf8.RuneCountInString(head.str)); hw <= end-f.x {
						keep, ex, cutF, cutSh, cutNb = q, f.x+hw, f, head, nb
					}
				}
			}
			if keep < 0 && j == first {
				// nothing of the line's first child fits: its first
				// character stays, "…" after it
				if s := &kids[j]; s.frag >= 0 && d.frags[s.frag].kind == fText && d.frags[s.frag].sh != nil {
					f := &d.frags[s.frag]
					head, _, nb := l.st.split(items[s.item].face, f.sh, 0)
					keep, ex = j, f.x+f.w
					if head != f.sh {
						keep, ex, cutF, cutSh, cutNb = j, f.x+f.sh.caretX(utf8.RuneCountInString(head.str)), f, head, nb
					}
				} else {
					keep, ex = j, kids[j].x+kids[j].w
				}
			}
			i = j
			continue
		}
		// an atomic child: an image, a pseudo-element's place, a swatch,
		// MissingText's bar
		ml := float32(0)
		block := it.kind == itSlot && it.atom || it.own > 0
		switch {
		case it.kind == itSlot && it.comp > 0 && l.cs != nil && int(it.comp) <= len(*l.cs):
			ml = l.compState(it.comp).pml[it.slot]
		case it.own > 0 && l.decos[it.own-1].kind == 4:
			ml = l.dpf(2) // the swatch's margin-left
		}
		xe := r.x + ml + r.w
		if it.kind == itImage && r.frag >= 0 {
			xe = d.frags[r.frag].x + d.frags[r.frag].w
		}
		if xe <= end || i == first {
			keep, ex = i, xe
			if block {
				col = it.col
			}
		}
	}
	// what "…" hides: the frags, places and atomic inlines after it
	f1 := ln.f0
	cutItem := -1
	if keep >= 0 {
		cutItem = kids[keep].item
		for q := keep; q >= 0; q-- {
			if kids[q].frag >= 0 {
				f1 = kids[q].frag + 1
				break
			}
		}
	}
	if cutF != nil {
		cutF.sh, cutF.w, cutF.p1 = cutSh, cutSh.adv, min(cutF.p0+int32(cutNb), cutF.p1)
	}
	if f1 < ln.f1 {
		d.frags = d.frags[:f1]
		ln.f1 = f1
	}
	if f1 > ln.f0 {
		// the spaces after the last word kept aren't drawn
		if f := &d.frags[f1-1]; f.kind == fText && ex <= f.x+f.w+0.01 {
			f.ulT, f.ulU = 0, 0
		}
	}
	hide := func(rs []placedRun) []placedRun {
		out := rs[:0]
		for _, r := range rs {
			if r.line != line || r.item <= cutItem {
				out = append(out, r)
			}
		}
		return out
	}
	*slots = hide(*slots)
	*atoms = hide(*atoms)
	bm := l.st.spaceOf(k)
	ty := ln.top + int((float32(fs.lineH)-(bm.asc+bm.desc))/2+bm.asc+float32(ln.bot-ln.top-fs.lineH)/2+0.5)
	for i := ln.f0; i < ln.f1; i++ {
		if f := &d.frags[i]; f.kind == fText {
			ty = f.y // the line's baseline
			break
		}
	}
	d.frags = append(d.frags, frag{kind: fText, x: ex, w: ell.adv, size: fs.size, sh: ell, col: col, target: -1, top: ln.top, bot: ln.bot, line: int32(line), y: ty,
		p0: int32(len(d.plain)), p1: int32(len(d.plain)), part: l.curPart, ell: true})
	ln.f1 = len(d.frags)
	d.ells++
}

// sameRun reports that items a and b (words and spaces) are one text run
// for the clamp (one Chromium text item): of one element, style and font.
func sameRun(a, b *item) bool {
	return (a.kind == itWord || a.kind == itSpace) && a.style == b.style && a.deco == b.deco && a.target == b.target &&
		a.comp == b.comp && a.part == b.part && a.face == b.face && a.col == b.col
}

// textFlow is the common entry: spans laid out at (x, y) with width w.
func (l *lay) textFlow(spans []richtext.Span, x, y, w int, fs *flowStyle) (int, int) {
	l.addSpans(spans, fs)
	return l.flow(x, y, w, fs)
}

// plainFlow lays out a plain string in one style.
func (l *lay) plainFlow(s string, x, y, w int, fs *flowStyle, tg int16) (int, int) {
	k := l.faceFor(fs.face, fs.size, 0)
	if l.curPart != 0 {
		k = l.partFace(k, l.curPart)
	}
	l.noPlain = fs.noSelect
	l.addText(s, fs, k, fs.col, 0, -1, tg)
	l.noPlain = false
	return l.flow(x, y, w, fs)
}

// measure returns the width of s in face k (single line).
func (l *lay) measure(s string, k faceKey) float32 {
	if segs := splitEmoji(s); segs != nil {
		var w float32
		for _, sg := range segs {
			if sg.stem != "" {
				w += float32(k.size>>6)*1.2 + 2*l.dpf(1) // emoji ≈ 1.2em, like the flows
			} else {
				w += l.measure(sg.text, k)
			}
		}
		return w
	}
	var w float32
	for _, part := range strings.SplitAfter(s, " ") {
		trim := strings.TrimSuffix(part, " ")
		if trim != "" {
			w += l.st.word(k, trim).adv
		}
		if len(trim) != len(part) {
			w += l.st.spaceOf(k).adv
		}
	}
	return w
}
