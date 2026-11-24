package quip

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

// Saving (docs/CANVASES.md "edit-document"): a save is the sections Slack's
// editor would put in its update bundle for the same edits, each built the
// way ZRIn.toUpdateBundle builds it over EKJU.toPb, with the title's
// metadata extractTitleMetadata adds (title, emoji info, rich text). The
// parity snapshots quip-save-section and quip-save-title hold these to
// Slack's own code.

// EditKind is one of the edits Clack saves.
type EditKind int

const (
	// EditText is typed text: the section's new RTML. Slack's input path
	// marks CONTENT and STATUS dirty (EKJU: setDirty([CONTENT, STATUS])).
	EditText EditKind = iota + 1
	// EditCheck ticks or unticks a checklist item: setSectionAttribute
	// ("checked") marks STATUS, then ATTRIBUTES.
	EditCheck
	// EditDelete deletes the section: deleteSection marks STATUS and
	// deleteEntity DELETED.
	EditDelete
	// EditBlock changes a section's style or attributes and not its text
	// (Edit's Restyle, Reindent, Recheck).
	EditBlock
)

func (k EditKind) String() string {
	switch k {
	case EditText:
		return "text"
	case EditCheck:
		return "check"
	case EditDelete:
		return "delete"
	case EditNew:
		return "new"
	case EditBlock:
		return "block"
	}
	return "edit(" + strconv.Itoa(int(k)) + ")"
}

// Edit is one change to one section.
type Edit struct {
	Kind      EditKind
	SectionID string
	RTML      string // EditText: the section's whole new RTML
	Checked   bool   // EditCheck; EditNew: a ticked checklist item
	// Base is the section's sequence the edit was made against. A save
	// goes only while the section is still there: one that moved since
	// (someone else's save) isn't overwritten, as Clack can't merge.
	Base int64
	// EditNew (newsection.go): the section it follows, its style and
	// position.
	After    string
	Style    int32
	Position string
	// with EditText, EditCheck or EditBlock: the section's style changes
	// to Style (setSectionStyle marks STYLE), its list level to Indent or
	// its box to Checked (setSectionAttribute marks STATUS and ATTRIBUTES)
	Restyle  bool
	Reindent bool
	Indent   int32
	Recheck  bool
}

// Counters are a client session's save counters, as 7yDq keeps them for
// a page: the session id, the save sequence (sessionSaveSequence, bumped
// once per save), and each entity's local sequence (ZRIn.localSequence_,
// bumped each time it goes into a bundle). One Counters serves every
// canvas of a session, as Slack's module-wide ones do.
type Counters struct {
	Session string

	mu    sync.Mutex
	saves int64
	local map[string]int64 // by section id
}

// NewCounters starts a session: a new 12-hex-digit id, nothing saved.
func NewCounters() *Counters { return &Counters{Session: SessionID(), local: map[string]int64{}} }

// Save is one save's numbers: the trace id its sections carry (the
// session and the save sequence before the bump) and the form's sequence
// (after it), with each bundled section's local sequence.
type Save struct {
	Trace    string
	Sequence int64
	local    map[string]int64
}

// Rename carries a new section's local sequence to the real id the server
// gave it: Slack's entity is the same one, renamed (entityIdDidChange), so
// its count goes on.
func (c *Counters) Rename(temp, real string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.local[temp]; ok && temp != real {
		c.local[real] = n
		delete(c.local, temp)
	}
}

// Begin reserves the next save: ids are the sections going into it, each
// counted once more. A save that fails is begun again, as Slack builds a
// new bundle (and a new local sequence) for each attempt.
func (c *Counters) Begin(ids []string) *Save {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := &Save{Trace: c.Session + "-" + strconv.FormatInt(c.saves, 10), local: map[string]int64{}}
	for _, id := range ids {
		c.local[id]++
		s.local[id] = c.local[id]
	}
	c.saves++
	s.Sequence = c.saves
	return s
}

// Section types and styles a save may touch, by what a bundle of B2's
// edits carries.
var (
	// ErrUnsupported is an edit this version doesn't save the way
	// Slack's client would; the save is refused, nothing is sent.
	ErrUnsupported = errors.New("quip: not saved the way Slack's client would yet")
)

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnsupported}, args...)...)
}

// UpdateSection is the bundle's section for edit e of server (the section
// as the server last sent it): ZRIn.toUpdateBundle's fields for the
// edit's dirty fields over EKJU.toPb's, then the activity row
// createUpdateBundle adds (WJQ2.shouldAddToActivityStack). parentStyle is
// the style of the section's parent (a list) when the model has it, nil
// when the parent is missing (Slack's missingParentIds_: the id without a
// container style). trace and local are the save's.
func UpdateSection(server *Section, e Edit, parentStyle *int32, trace string, local int64) (*Section, error) {
	if server == nil || server.ID == nil {
		return nil, errors.New("quip: no server section to update")
	}
	if server.Deleted != nil && *server.Deleted {
		return nil, unsupported("section %s is deleted", *server.ID)
	}
	typ := int32(0)
	if server.Type != nil {
		typ = *server.Type
	}
	u := &Section{
		ID:       Ptr(*server.ID),
		Sequence: Ptr(seq(server.Sequence)),
		// every edit here dirties two fields, never STATUS alone: TYPE
		Type:          Ptr(typ),
		Parents:       bundleParents(server.Parents, parentStyle),
		Modified:      Ptr(true),
		LocalSequence: Ptr(local),
		TraceID:       Ptr(trace),
	}
	switch e.Kind {
	case EditText:
		c, err := editedContent(server, e.RTML)
		if err != nil {
			return nil, err
		}
		u.Content = c
	case EditCheck:
		u.Attributes = bundleAttributes(server.Attributes)
		u.Attributes.Checked = Ptr(e.Checked)
	case EditDelete:
		u.Deleted = Ptr(true)
	case EditBlock:
		if !e.Restyle && !e.Reindent && !e.Recheck {
			return nil, unsupported("a block edit that changes nothing")
		}
	default:
		return nil, unsupported("edit kind %v", e.Kind)
	}
	if e.Restyle {
		u.Style = Ptr(e.Style)
	}
	if e.Reindent || e.Recheck {
		if u.Attributes == nil {
			u.Attributes = bundleAttributes(server.Attributes)
		}
		if e.Reindent {
			u.Attributes.Indentation = Ptr(e.Indent)
		}
		if e.Recheck {
			u.Attributes.Checked = Ptr(e.Checked)
		}
	}
	if a, ok := activityRow(u, server); ok {
		u.ActivityStackUpdate = Ptr(a)
	}
	return u, nil
}

// bundleParents is EKJU.toPb's parents: the parents' ids and, from the
// first parent the model has, its style; always set, empty for a
// top-level section.
func bundleParents(p *Parents, parentStyle *int32) *Parents {
	out := &Parents{}
	if p == nil {
		return out
	}
	out.IDs = append([]string(nil), p.IDs...)
	if len(out.IDs) > 0 && parentStyle != nil {
		out.ContainerStyle = Ptr(*parentStyle)
	}
	return out
}

// bundleAttributes is EKJU.toPb's attributes over the map update() loads
// (convertAttributesPbToMap): every attribute the server sent, and the two
// whose defaults aren't zero at those defaults when it didn't
// (vertical_margin true, vertical_alignment 5).
func bundleAttributes(a *Attributes) *Attributes {
	out := &Attributes{}
	if a != nil {
		c := *a
		c.Other = append([]byte(nil), a.Other...)
		out = &c
	}
	if out.VerticalMargin == nil {
		out.VerticalMargin = Ptr(true)
	}
	if out.VerticalAlignment == nil {
		out.VerticalAlignment = Ptr(int32(5))
	}
	return out
}

// editedContent is the content the section's toPb makes after typing:
// a text section's RTML (with the code language it holds), the title's
// RTML (the cover would go back too: refused, see below).
func editedContent(server *Section, rtml string) (*Content, error) {
	old := server.Content
	if old == nil {
		old = &Content{}
	}
	switch typ := int32(0); {
	case server.Type != nil && *server.Type == TypeTitle:
		t := &ContentTitle{Text: Ptr(rtml)}
		if old.Title != nil && old.Title.HeaderImage != nil {
			// Slack re-encodes the cover it decoded; Clack holds the
			// server's bytes and can't promise the same bytes yet
			return nil, unsupported("the title has a cover image")
		}
		return &Content{Title: t}, nil
	case server.Type == nil || *server.Type == typ:
		t := &ContentText{Text: Ptr(rtml)}
		if old.Text != nil && old.Text.Language != nil {
			t.Language = Ptr(*old.Text.Language)
		}
		return &Content{Text: t}, nil
	default:
		return nil, unsupported("section %s is of type %d", *server.ID, *server.Type)
	}
}

// activityRow is WJQ2.shouldAddToActivityStack for a bundle section
// against the server's copy.
func activityRow(u, server *Section) (int32, bool) {
	if u.Type == nil || *u.Type != TypeText {
		return 0, false
	}
	has := func(s *Section) (string, bool) {
		if s == nil || s.Content == nil || s.Content.Text == nil || s.Content.Text.Text == nil {
			return "", false
		}
		return *s.Content.Text.Text, true
	}
	text, set := has(u)
	nonEmpty := set && text != ""
	if seq(u.Sequence) == 0 && nonEmpty {
		return ActivityAddSection, true
	}
	old, _ := has(server)
	switch {
	case old == "" && nonEmpty:
		return ActivityAddSection, true
	case old != "" && set && text == "":
		return ActivityRemoveSection, true
	}
	checked := func(s *Section) (bool, bool) {
		if s == nil || s.Attributes == nil || s.Attributes.Checked == nil {
			return false, false
		}
		return *s.Attributes.Checked, true
	}
	was, _ := checked(server)
	now, set := checked(u)
	switch {
	case !was && set && now:
		return ActivityAddCheckbox, true
	case was && set && !now:
		return ActivityRemoveCheckbox, true
	}
	return 0, false
}

// TextRTML is the RTML Slack's save writer (CITi's DI over the editor's
// DOM) makes of plain typed text: zero-width spaces and byte order marks
// out, then escapeText. Canvases are pre-wrap (improvedWordWrapping), so
// typed spaces are in the DOM as typed.
func TextRTML(text string) (string, error) {
	t := strings.NewReplacer("\u200b", "", "\ufeff", "").Replace(text)
	if !utf8.ValidString(t) {
		return "", unsupported("invalid UTF-8")
	}
	return EscapeText(t, false), nil
}

// jsSpace is what String.prototype.trim takes off: WhiteSpace and
// LineTerminator.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// cutUTF16 is String.prototype.substring(0, n); false where that would
// split a surrogate pair.
func cutUTF16(s string, n int) (string, bool) {
	u := 0
	for i, r := range s {
		l := utf16.RuneLen(r)
		if u+l > n {
			return s[:i], u == n
		}
		u += l
	}
	return s, true
}

// Bundle is a save's DocumentData for edits, with Slack's order: sections
// as the document's entities were added (Doc.Sections' load order), then
// the title metadata. It refuses edits it can't make the way Slack's
// client would.
func (d *Doc) Bundle(edits []Edit, s *Save) (*DocumentData, *TitleMeta, error) {
	byID := map[string]Edit{}
	for _, e := range edits {
		if _, dup := byID[e.SectionID]; dup {
			return nil, nil, unsupported("two edits of section %s in one save", e.SectionID)
		}
		byID[e.SectionID] = e
	}
	data := &DocumentData{}
	for _, id := range d.inOrder() {
		e, ok := byID[id]
		if !ok {
			continue
		}
		server := d.Section(id)
		var parentStyle *int32
		if server.Parents != nil && len(server.Parents.IDs) > 0 {
			if p := d.Section(server.Parents.IDs[0]); p != nil && p.Style != nil {
				parentStyle = Ptr(*p.Style)
			}
		}
		u, err := UpdateSection(server, e, parentStyle, s.Trace, s.local[id])
		if err != nil {
			return nil, nil, err
		}
		data.Sections = append(data.Sections, *u)
		delete(byID, id)
	}
	// new sections, in the order they were made (entitiesById_: after
	// every entity the model had)
	made := map[string]*Section{}
	for _, e := range edits {
		if e.Kind != EditNew {
			continue
		}
		if d.Section(e.SectionID) != nil {
			return nil, nil, unsupported("new section %s is already in the document", e.SectionID)
		}
		after := made[e.After]
		if after == nil {
			after = d.Section(e.After)
		}
		if after == nil || after.Deleted != nil && *after.Deleted {
			return nil, nil, unsupported("a new section after %s, which isn't in the document", e.After)
		}
		var parent *Section
		if after.Parents != nil && len(after.Parents.IDs) > 0 {
			parent = d.Section(after.Parents.IDs[0])
		}
		u, err := NewSection(e, after, parent, s.Trace, s.local[e.SectionID])
		if err != nil {
			return nil, nil, err
		}
		made[e.SectionID] = u
		data.Sections = append(data.Sections, *u)
		delete(byID, e.SectionID)
	}
	for id := range byID {
		return nil, nil, unsupported("section %s isn't in the document", id)
	}
	title := d.titleSection()
	if title == nil {
		return nil, nil, unsupported("the canvas has no title section")
	}
	rtml := SectionRTML(title)
	if e, ok := find(edits, *title.ID); ok && e.Kind == EditText {
		rtml = e.RTML
	}
	if e, ok := find(edits, *title.ID); ok && e.Kind == EditDelete {
		return nil, nil, unsupported("deleting the title")
	}
	meta, err := TitleMetadata(rtml, d.TitleControls())
	if err != nil {
		return nil, nil, err
	}
	data.TitleRichText = meta.RichText
	data.TitleSlackEmojiInfo = meta.EmojiInfo
	return data, meta, nil
}

func find(edits []Edit, id string) (Edit, bool) {
	for _, e := range edits {
		if e.SectionID == id {
			return e, true
		}
	}
	return Edit{}, false
}

// titleSection is the document's title section (not deleted), nil when
// it has none.
func (d *Doc) titleSection() *Section {
	for _, s := range d.Sections() {
		if s.Type != nil && *s.Type == TypeTitle {
			return s
		}
	}
	return nil
}
