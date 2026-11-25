package fakeslack

// Quip's side of each fixture canvas (docs/CANVAS-EDIT-PLAN.md
// "fakeslack"): the thread, document and sections Slack's canvas editor
// loads and saves through /canvas/-/load-data, fetch-latest and
// edit-document (quip_endpoints.go). The sections are made from the
// canvas's blocks (canvas.Doc, which the HTML export is written from) and
// follow every change to them: a block another path changed gets a new
// sequence, a new block a new section, a gone one a deleted section. A
// save through edit-document changes the sections and writes them back
// into the blocks, so the export shows it.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
)

// quipModel is one canvas's Quip document.
type quipModel struct {
	thread, doc, shard string
	threadSeq, docSeq  int64
	maxSeq             int64 // max_section_sequence: the last sequence given
	secs               map[string]*quip.Section
	// sig is each section's source as it was last synced (its block's
	// JSON, the title): a section is made again only when its source
	// changed, so a save written back into the blocks reads back as is.
	sig map[string]string
	// opaque are the blocks Clack doesn't edit (tables, images, files,
	// embeds, columns, callouts), kept whole for the way back.
	opaque  map[string]canvas.Block
	titleID string
	// titleRTML is the title section's RTML as TitleSpans made it
	titleRTML string
	listOf    map[string]string // list item -> its LIST section
	// sessions are the edit sessions' last sequences (edit-document's
	// session and sequence fields).
	sessions map[string]int64
}

// quipThreadID is the canvas's quip_thread_id (files.info).
func quipThreadID(c *FakeCanvas) string { return "FAKE" + c.ID[len(c.ID)-4:] }

// quipDocID is its document's id; the first three characters are the
// shard chars, the ones the fake's section ids carry.
func quipDocID(c *FakeCanvas) string { return "FAKdoc" + c.ID[len(c.ID)-4:] }

func (m *quipModel) nextSeq() int64 {
	m.maxSeq = (m.maxSeq/1000 + 1) * 1000
	return m.maxSeq
}

// quip returns c's model, synced with its blocks (cs.mu held).
func (cs *canvasStore) quip(s *Server, c *FakeCanvas) *quipModel {
	if cs.quips == nil {
		cs.quips = map[string]*quipModel{}
	}
	m := cs.quips[c.ID]
	if m == nil {
		m = &quipModel{thread: quipThreadID(c), doc: quipDocID(c), shard: "FAK", threadSeq: 1000, docSeq: 1000,
			secs: map[string]*quip.Section{}, sig: map[string]string{}, opaque: map[string]canvas.Block{},
			listOf: map[string]string{}, sessions: map[string]int64{}}
		m.titleID = cs.secID()
		cs.quips[c.ID] = m
	}
	cs.quipSync(s, c, m)
	return m
}

// derived is one section as the blocks say it should be.
type derived struct {
	sec quip.Section
	sig string
}

// quipSync makes the model follow c's blocks.
func (cs *canvasStore) quipSync(s *Server, c *FakeCanvas, m *quipModel) {
	var want []derived
	add := func(sec quip.Section, sig string) { want = append(want, derived{sec, sig}) }
	titleRTML, titleSig := quip.EscapeText(c.Title, false), "title:"+c.Title
	var titleCtls []derived
	if c.TitleSpans != nil {
		n := 0
		titleRTML = spansRTML(c.TitleSpans, func(x richtext.Span) string {
			id := fmt.Sprintf("%s-c%d", m.titleID, n)
			n++
			sec, ok := controlSection(id, x)
			if !ok {
				return ""
			}
			// an emoji is stored nameless, its id only, as Slack's rich-text
			// import (30B3) and its :name: controlifier write it; the name
			// is the emoji store's display name once its model loads
			b, _ := json.Marshal(x)
			titleCtls = append(titleCtls, derived{sec, "control:" + string(b)})
			return id
		})
		b, _ := json.Marshal(c.TitleSpans)
		titleSig = "title:" + string(b)
	}
	m.titleRTML = titleRTML
	add(quip.Section{ID: quip.Ptr(m.titleID), Type: quip.Ptr(quip.TypeTitle), Style: quip.Ptr(quip.StyleTitle),
		Content: &quip.Content{Title: &quip.ContentTitle{Text: quip.Ptr(titleRTML), EmojiInfo: c.TitleEmojiInfo}}}, titleSig)
	want = append(want, titleCtls...)
	// keep holds an unchanged block's section as it is, with the controls
	// its RTML names (made by a save, they keep the client's ids)
	keep := func(id, sig string) bool {
		old := m.secs[id]
		if old == nil || old.Deleted != nil && *old.Deleted || m.sig[id] != sig {
			return false
		}
		for _, cid := range quip.ControlIDs(quip.SectionRTML(old)) {
			if c := m.secs[cid]; c != nil {
				add(*c, m.sig[cid])
			}
		}
		add(*old, sig)
		return true
	}
	controls := func(owner string, sp []richtext.Span) string {
		n := 0
		return spansRTML(sp, func(x richtext.Span) string {
			id := fmt.Sprintf("%s-c%d", owner, n)
			n++
			if sec, ok := controlSection(id, x); ok {
				b, _ := json.Marshal(x)
				add(sec, "control:"+string(b))
				return id
			}
			return ""
		})
	}
	text := func(b *canvas.Block, style int32) {
		sig := blockSig(b)
		if keep(b.ID, sig) {
			return
		}
		rt := ""
		if b.Kind == canvas.Code {
			rt = codeRTML(b.Code)
		} else {
			rt = controls(b.ID, b.Spans)
		}
		add(quip.Section{ID: quip.Ptr(b.ID), Type: quip.Ptr(quip.TypeText), Style: quip.Ptr(style),
			Content: &quip.Content{Text: &quip.ContentText{Text: quip.Ptr(rt)}}}, sig)
	}
	bs := c.Doc.Blocks
	for i := range bs {
		assignIDs(&bs[i], cs.secID)
	}
	for i := 0; i < len(bs); i++ {
		b := &bs[i]
		switch {
		case b.Kind.IsList():
			j := i + 1
			for j < len(bs) && bs[j].Kind == b.Kind {
				j++
			}
			list := ""
			for k := i; k < j && list == ""; k++ {
				list = m.listOf[bs[k].ID]
			}
			if list == "" {
				list = cs.secID()
			}
			style := map[canvas.Kind]int32{canvas.Bullet: quip.StyleBullet, canvas.Numbered: quip.StyleNumbered, canvas.Check: quip.StyleChecklist}[b.Kind]
			add(quip.Section{ID: quip.Ptr(list), Type: quip.Ptr(quip.TypeList), Style: quip.Ptr(style),
				Content: &quip.Content{List: &quip.Empty{}}}, fmt.Sprintf("list:%d", style))
			for k := i; k < j; k++ {
				it := &bs[k]
				m.listOf[it.ID] = list
				if keep(it.ID, blockSig(it)) {
					continue
				}
				rt := controls(it.ID, it.Spans)
				attrs := &quip.Attributes{Indentation: quip.Ptr(int32(it.Level)), VerticalMargin: quip.Ptr(false)}
				if b.Kind == canvas.Check {
					attrs.Checked = quip.Ptr(it.Checked)
				}
				add(quip.Section{ID: quip.Ptr(it.ID), Type: quip.Ptr(quip.TypeText), Style: quip.Ptr(quip.StylePlain),
					Content: &quip.Content{Text: &quip.ContentText{Text: quip.Ptr(rt)}},
					Parents: &quip.Parents{IDs: []string{list}, ContainerStyle: quip.Ptr(style)}, Attributes: attrs}, blockSig(it))
			}
			i = j - 1
		case b.Kind == canvas.Paragraph:
			text(b, quip.StylePlain)
		case b.Kind == canvas.Heading1:
			text(b, quip.StyleH1)
		case b.Kind == canvas.Heading2:
			text(b, quip.StyleH2)
		case b.Kind == canvas.Heading3:
			text(b, quip.StyleH3)
		case b.Kind == canvas.Code:
			text(b, quip.StyleCode)
		case b.Kind == canvas.Quote && quotesOnlyParagraphs(b):
			for k := range b.Children {
				text(&b.Children[k], quip.StyleBlockquote)
			}
		case b.Kind == canvas.Divider:
			add(quip.Section{ID: quip.Ptr(b.ID), Type: quip.Ptr(quip.TypeHorizontalRule), Style: quip.Ptr(quip.StyleHorizontalRule),
				Content: &quip.Content{HorizontalRule: &quip.HorizontalRule{}}}, blockSig(b))
		default: // a block Clack doesn't edit: a section of its type, kept whole
			m.opaque[b.ID] = *b
			add(quip.Section{ID: quip.Ptr(b.ID), Type: quip.Ptr(opaqueType(b.Kind)), Style: quip.Ptr(int32(0)),
				Content: &quip.Content{}}, blockSig(b))
		}
	}
	// positions: kept where they still sort, else a key between the
	// neighbours (0fa5's, without the client's :temp)
	seen := map[string]bool{}
	prev := ""
	for i, d := range want {
		id := *d.sec.ID
		seen[id] = true
		old := m.secs[id]
		pos := ""
		if old != nil && old.Position != nil && *old.Position > prev {
			pos = *old.Position
		} else {
			next := ""
			for _, e := range want[i+1:] {
				if o := m.secs[*e.sec.ID]; o != nil && o.Position != nil && *o.Position > prev {
					next = *o.Position
					break
				}
			}
			pos = quip.StripTemp(quip.Between(prev, next))
		}
		prev = pos
		switch {
		case old == nil || (old.Deleted != nil && *old.Deleted):
			sec := d.sec
			sec.Sequence, sec.Position, sec.PositionPath = quip.Ptr(m.nextSeq()), quip.Ptr(pos), quip.Ptr(pos)
			sec.ThreadID, sec.DocumentID, sec.SectionClass = quip.Ptr(m.thread), quip.Ptr(m.doc), quip.Ptr(sectionClass(&d.sec))
			m.secs[id], m.sig[id] = &sec, d.sig
		case m.sig[id] != d.sig || *old.Position != pos:
			sec := d.sec
			sec.Sequence, sec.Position, sec.PositionPath = quip.Ptr(m.nextSeq()), quip.Ptr(pos), quip.Ptr(pos)
			sec.ThreadID, sec.DocumentID, sec.SectionClass = quip.Ptr(m.thread), quip.Ptr(m.doc), quip.Ptr(sectionClass(&d.sec))
			if m.sig[id] == d.sig { // only moved: the content stays as saved
				sec.Content, sec.Parents, sec.Attributes = old.Content, old.Parents, old.Attributes
			}
			m.secs[id], m.sig[id] = &sec, d.sig
		}
	}
	// a gone block's section is deleted; a control no text names any more
	// stays, as Slack's client leaves it (eju0.remove changes only the
	// text; what the server does with it is unknown)
	for id, sec := range m.secs {
		if sec.SectionClass != nil && *sec.SectionClass == quip.ClassControl {
			continue
		}
		if !seen[id] && (sec.Deleted == nil || !*sec.Deleted) {
			c := *sec
			c.Deleted, c.Sequence = quip.Ptr(true), quip.Ptr(m.nextSeq())
			m.secs[id] = &c
		}
	}
}

func sectionClass(s *quip.Section) int32 {
	if s.Content != nil && (s.Content.SlackUser != nil || s.Content.SlackChannel != nil || s.Content.SlackEmoji != nil ||
		s.Content.SlackBroadcast != nil || s.Content.SlackDate != nil) {
		return quip.ClassControl
	}
	return quip.ClassSection
}

func quotesOnlyParagraphs(b *canvas.Block) bool {
	for _, c := range b.Children {
		if c.Kind != canvas.Paragraph {
			return false
		}
	}
	return len(b.Children) > 0
}

func opaqueType(k canvas.Kind) int32 {
	switch k {
	case canvas.Table:
		return quip.TypeLayoutTable
	case canvas.Image:
		return quip.TypeImage
	case canvas.Embed:
		return quip.TypeLinkUnfurlCard
	case canvas.File:
		return quip.TypeSlackFile
	case canvas.Columns:
		return quip.TypeLayoutFlexbox
	case canvas.Callout:
		return quip.TypeLayoutCallout
	case canvas.Quote:
		return quip.TypeLayoutBlockquote
	}
	return quip.TypeText
}

func assignIDs(b *canvas.Block, sec func() string) {
	if b.ID == "" {
		b.ID = sec()
	}
	for i := range b.Children {
		assignIDs(&b.Children[i], sec)
	}
}

func blockSig(b *canvas.Block) string {
	j, _ := json.Marshal(b)
	return string(j)
}

// controlSection is the control a mention, emoji, @here or date span is
// in Quip (30B3's slackObjectControlType and its contents).
func controlSection(id string, x richtext.Span) (quip.Section, bool) {
	sec := quip.Section{ID: quip.Ptr(id), SectionClass: quip.Ptr(quip.ClassControl)}
	switch x.Kind {
	case richtext.User, richtext.Usergroup:
		if x.ID == "" {
			return sec, false
		}
		sec.Type, sec.Content = quip.Ptr(quip.TypeSlackUser), &quip.Content{SlackUser: &quip.SlackUser{SlackUserID: quip.Ptr(quip.UserID(x.ID))}}
	case richtext.Channel:
		if x.ID == "" {
			return sec, false
		}
		sec.Type, sec.Content = quip.Ptr(quip.TypeSlackChannel), &quip.Content{SlackChannel: &quip.SlackChannel{SlackChannelID: quip.Ptr(quip.ChannelID(x.ID))}}
	case richtext.Broadcast:
		sec.Type, sec.Content = quip.Ptr(quip.TypeSlackBroadcast), &quip.Content{SlackBroadcast: &quip.SlackBroadcast{Range: quip.Ptr(x.ID)}}
	case richtext.Emoji:
		sec.Type, sec.Content = quip.Ptr(quip.TypeSlackEmoji), &quip.Content{SlackEmoji: &quip.SlackEmoji{SlackEmojiID: quip.Ptr(quip.EmojiID(x.ID, EnterpriseID))}}
		sec.AltRTML = quip.Ptr(":" + x.ID + ":")
	case richtext.Date:
		sec.Type, sec.Content = quip.Ptr(quip.TypeSlackDate), &quip.Content{SlackDate: &quip.SlackDate{DateFormat: quip.Ptr(x.Text)}}
	default:
		return sec, false
	}
	return sec, true
}

var rtmlTags = []struct {
	st  richtext.Style
	tag string
}{{richtext.Bold, "b"}, {richtext.Italic, "i"}, {richtext.Underline, "u"}, {richtext.Strike, "del"}, {richtext.Code, "code"}}

// spansRTML writes spans as Quip holds them: text escaped, formatting as
// tags around each span, links as <a>, mentions as controls (control
// gives the control's id, "" to write the span's text instead).
func spansRTML(sp []richtext.Span, control func(richtext.Span) string) string {
	var b strings.Builder
	for _, x := range sp {
		inner := ""
		switch x.Kind {
		case richtext.Break:
			b.WriteString("<br>")
			continue
		case richtext.Text:
			inner = quip.EscapeText(x.Text, false)
		case richtext.Link:
			inner = `<a href="` + strings.ReplaceAll(strings.ReplaceAll(x.URL, "&", "&amp;"), `"`, "&quot;") + `">` + quip.EscapeText(x.Text, false) + "</a>"
		default:
			if id := control(x); id != "" {
				inner = `<control id="` + id + `"></control>`
			} else {
				inner = quip.EscapeText(x.Text, false)
			}
		}
		for i := len(rtmlTags) - 1; i >= 0; i-- {
			if x.Style&rtmlTags[i].st != 0 {
				inner = "<" + rtmlTags[i].tag + ">" + inner + "</" + rtmlTags[i].tag + ">"
			}
		}
		b.WriteString(inner)
	}
	return b.String()
}

// codeRTML is a code block's text as Quip's code sections hold it: lines
// joined with <br>, leading spaces and runs of them as &nbsp;
// (32AJ.plainTextToCodeRtml).
func codeRTML(code string) string {
	var out []string
	for _, line := range strings.Split(code, "\n") {
		e := quip.EscapeText(line, false)
		var b strings.Builder
		for i := 0; i < len(e); i++ {
			if e[i] == ' ' && (i == 0 || e[i-1] == ' ' || i+1 < len(e) && e[i+1] == ' ') {
				b.WriteString("&nbsp;")
				continue
			}
			b.WriteByte(e[i])
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "<br>")
}

// quipBlocks writes the model back into c's blocks (after a save).
func (m *quipModel) writeBack(c *FakeCanvas) {
	live := func(id string) *quip.Section {
		if s := m.secs[id]; s != nil && (s.Deleted == nil || !*s.Deleted) {
			return s
		}
		return nil
	}
	var top []*quip.Section
	items := map[string][]*quip.Section{}
	for _, s := range m.secs {
		if s.Deleted != nil && *s.Deleted || s.SectionClass != nil && *s.SectionClass == quip.ClassControl {
			continue
		}
		if s.Parents != nil && len(s.Parents.IDs) > 0 {
			items[s.Parents.IDs[0]] = append(items[s.Parents.IDs[0]], s)
			continue
		}
		top = append(top, s)
	}
	byPos := func(ss []*quip.Section) {
		sort.Slice(ss, func(i, j int) bool {
			return pos(ss[i]) < pos(ss[j]) || pos(ss[i]) == pos(ss[j]) && *ss[i].ID < *ss[j].ID
		})
	}
	byPos(top)
	var blocks []canvas.Block
	for _, s := range top {
		id := *s.ID
		t := int32(0)
		if s.Type != nil {
			t = *s.Type
		}
		st := int32(0)
		if s.Style != nil {
			st = *s.Style
		}
		switch t {
		case quip.TypeTitle:
			if c.TitleSpans != nil && quip.SectionRTML(s) == m.titleRTML {
				continue // as TitleSpans made it: its spans and source stay
			}
			c.TitleSpans, c.TitleEmojiInfo = nil, nil
			if s.Content != nil && s.Content.Title != nil {
				c.Title = plainOfRTML(quip.SectionRTML(s))
			}
			m.sig[id] = "title:" + c.Title
			continue
		case quip.TypeText:
			b := canvas.Block{ID: id, Kind: canvas.Paragraph}
			switch st {
			case quip.StyleH1:
				b.Kind = canvas.Heading1
			case quip.StyleH2:
				b.Kind = canvas.Heading2
			case quip.StyleH3, quip.StyleH4, quip.StyleH5, quip.StyleH6:
				b.Kind = canvas.Heading3
			case quip.StyleCode:
				b.Kind = canvas.Code
			}
			if b.Kind == canvas.Code {
				b.Code = codeOfRTML(quip.SectionRTML(s))
			} else {
				b.Spans = m.spans(quip.SectionRTML(s), live)
			}
			if st == quip.StyleBlockquote {
				if n := len(blocks); n > 0 && blocks[n-1].Kind == canvas.Quote && blocks[n-1].ID == "" {
					blocks[n-1].Children = append(blocks[n-1].Children, b)
				} else {
					blocks = append(blocks, canvas.Block{Kind: canvas.Quote, Children: []canvas.Block{b}})
				}
				m.sig[id] = blockSig(&b)
				continue
			}
			blocks = append(blocks, b)
			m.sig[id] = blockSig(&blocks[len(blocks)-1])
		case quip.TypeList:
			kind := map[int32]canvas.Kind{quip.StyleBullet: canvas.Bullet, quip.StyleNumbered: canvas.Numbered, quip.StyleChecklist: canvas.Check}[st]
			its := items[id]
			byPos(its)
			numbers := map[int]int{}
			for _, it := range its {
				b := canvas.Block{ID: *it.ID, Kind: kind, Spans: m.spans(quip.SectionRTML(it), live)}
				if a := it.Attributes; a != nil {
					if a.Indentation != nil {
						b.Level = int(*a.Indentation)
					}
					if a.Checked != nil {
						b.Checked = *a.Checked
					}
				}
				for l := range numbers {
					if l > b.Level {
						delete(numbers, l)
					}
				}
				numbers[b.Level]++
				if kind == canvas.Numbered {
					b.Number = numbers[b.Level]
				}
				m.listOf[b.ID] = id
				blocks = append(blocks, b)
				m.sig[b.ID] = blockSig(&blocks[len(blocks)-1])
			}
		case quip.TypeHorizontalRule:
			blocks = append(blocks, canvas.Block{ID: id, Kind: canvas.Divider})
			m.sig[id] = blockSig(&blocks[len(blocks)-1])
		default:
			if b, ok := m.opaque[id]; ok {
				blocks = append(blocks, b)
				m.sig[id] = blockSig(&b)
			}
		}
	}
	c.Doc.Blocks = blocks
}

func pos(s *quip.Section) string {
	if s.Position == nil {
		return ""
	}
	return *s.Position
}

// spans reads RTML into the export's spans: marks to styles and links,
// controls to the mentions they hold.
func (m *quipModel) spans(rtml string, live func(string) *quip.Section) []richtext.Span {
	text, marks, ok := quip.ParseRTML(rtml)
	if !ok {
		return []richtext.Span{{Kind: richtext.Text, Text: plainOfRTML(rtml)}}
	}
	var out []richtext.Span
	styleAt := func(i int) (richtext.Style, string) {
		var st richtext.Style
		url := ""
		for _, mk := range marks {
			if mk.Start <= i && i < mk.End {
				switch mk.Name {
				case "b":
					st |= richtext.Bold
				case "i":
					st |= richtext.Italic
				case "u":
					st |= richtext.Underline
				case "del":
					st |= richtext.Strike
				case "code":
					st |= richtext.Code
				case "a":
					var l struct {
						Href string `json:"href"`
					}
					if json.Unmarshal([]byte(mk.Value), &l) == nil {
						url = l.Href
					}
				}
			}
		}
		return st, url
	}
	for i := 0; i < len(text); {
		st, url := styleAt(i)
		switch {
		case strings.HasPrefix(text[i:], "<br>"):
			out = append(out, richtext.Span{Kind: richtext.Break})
			i += len("<br>")
			continue
		case strings.HasPrefix(text[i:], `<control id="`):
			end := strings.Index(text[i:], `"></control>`)
			if end < 0 {
				break
			}
			id := text[i+len(`<control id="`) : i+end]
			i += end + len(`"></control>`)
			if c := live(id); c != nil {
				out = append(out, mentionSpan(c, st))
			}
			continue
		}
		// a run of text up to the next markup or style change
		j := i + 1
		for j < len(text) && !strings.HasPrefix(text[j:], "<br>") && !strings.HasPrefix(text[j:], "<control") {
			if s2, u2 := styleAt(j); s2 != st || u2 != url {
				break
			}
			j++
		}
		chunk := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(text[i:j])
		kind := richtext.Text
		if url != "" {
			kind = richtext.Link
		}
		if n := len(out); n > 0 && out[n-1].Kind == kind && out[n-1].Style == st && out[n-1].URL == url {
			out[n-1].Text += chunk
		} else {
			out = append(out, richtext.Span{Kind: kind, Text: chunk, Style: st, URL: url})
		}
		i = j
	}
	return out
}

// mentionSpan is a control's span as the export's parser reads it back.
func mentionSpan(c *quip.Section, st richtext.Style) richtext.Span {
	kind, id := quip.Control(c)
	switch kind {
	case quip.ControlUser:
		if strings.HasPrefix(id, "S") {
			return richtext.Span{Kind: richtext.Usergroup, Text: "@" + id, ID: id, Style: st}
		}
		return richtext.Span{Kind: richtext.User, Text: "@" + id, ID: id, Style: st}
	case quip.ControlChannel:
		return richtext.Span{Kind: richtext.Channel, Text: "#" + id, ID: id, Style: st}
	case quip.ControlBroadcast:
		return richtext.Span{Kind: richtext.Broadcast, Text: "@" + id, ID: id, Self: true, Style: st}
	case quip.ControlEmoji:
		return richtext.Span{Kind: richtext.Emoji, ID: id, Style: st}
	case quip.ControlDate:
		t := ""
		if c.Content.SlackDate.DateFormat != nil {
			t = *c.Content.SlackDate.DateFormat
		}
		return richtext.Span{Kind: richtext.Date, Text: t, Style: st}
	}
	return richtext.Span{Kind: richtext.Text, Style: st}
}

// plainOfRTML is RTML's text with the markup gone and entities read.
// codeOfRTML is a code block's text: its RTML's characters with each
// line break a newline and each no-break space a space. (A raw newline
// can't stand in for a <br> before the parse: RTML text holds none, and
// the parse makes one a space.)
func codeOfRTML(rtml string) string {
	plain, _ := quip.RTMLPlain(rtml)
	plain = strings.ReplaceAll(plain, string(quip.ControlMark), "")
	return strings.ReplaceAll(plain, "\u00a0", " ")
}

func plainOfRTML(rtml string) string {
	text, _, _ := quip.ParseRTML(rtml)
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '<' {
			if j := strings.IndexByte(text[i:], '>'); j >= 0 {
				i += j + 1
				continue
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(b.String())
}
