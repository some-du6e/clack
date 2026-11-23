package quip

// EditNew is a new text section: Slack's editor makes one on Enter (or
// when typing starts under the last section) with K2FO.createNewSection:
// createNewSectionAtPosition (a fresh entity, every field dirty, at
// sequence 0, a position after the section it follows, vertical_margin
// false), then initSibling (that section's parents, indentation and
// paragraph indentation).
//
// Its Edit holds the new section's id (NewSectionID), the section it
// follows (After: one in the model, or an earlier new section of the same
// save), its style, its position (PositionAfter), its RTML, and for a
// checklist item its box.
const EditNew EditKind = 100

// NewSection is the bundle section Slack saves for a section its editor
// just made: id, sequence 0, deleted false, position, type and style,
// section class SECTION, content, parents, attributes, modified, the first
// local sequence, the position path, the save's trace id, read_only
// false, and the activity row a new section with text gets (the field set
// Slack's own entity gives, quip-save-new-section). after is the section
// it follows as the bundle sees it, parent its first parent (nil when
// top-level).
func NewSection(e Edit, after, parent *Section, trace string, local int64) (*Section, error) {
	typ, ok := TypeOfStyle(e.Style)
	if !ok || typ != TypeText {
		return nil, unsupported("a new section of style %d", e.Style)
	}
	if after == nil {
		return nil, unsupported("a new section after nothing")
	}
	u := &Section{
		ID:            Ptr(e.SectionID),
		Sequence:      Ptr(int64(0)),
		Deleted:       Ptr(false),
		Position:      Ptr(e.Position),
		Type:          Ptr(typ),
		Style:         Ptr(e.Style),
		SectionClass:  Ptr(ClassSection),
		Content:       &Content{Text: &ContentText{Text: Ptr(e.RTML)}},
		Parents:       &Parents{},
		Attributes:    newAttributes(after, e),
		Modified:      Ptr(true),
		LocalSequence: Ptr(local),
		TraceID:       Ptr(trace),
		ReadOnly:      Ptr(false),
	}
	if after.Parents != nil && len(after.Parents.IDs) > 0 {
		u.Parents.IDs = append([]string(nil), after.Parents.IDs...)
		if parent != nil && parent.Style != nil {
			u.Parents.ContainerStyle = Ptr(*parent.Style)
		}
	}
	path := e.Position
	if parent != nil && parent.Position != nil {
		path = *parent.Position + "-" + e.Position // EKJU.positionPath: the ancestors' positions, then its own
	}
	u.PositionPath = Ptr(path)
	if a, ok := activityRow(u, nil); ok {
		u.ActivityStackUpdate = Ptr(a)
	}
	return u, nil
}

// newAttributes is what createNewSectionAtPosition and initSibling leave
// in a new section's attributes: vertical_margin false, and from the
// section it follows its indentation and paragraph indentation; a
// checklist item's box when it's ticked. initSibling also copies a text
// alignment other than left, but Slack's canvas editor suppresses text
// alignment (suppressTextAlignment = !autoTextAlignment, off), so
// getTextAlignment is always left there and nothing is copied.
func newAttributes(after *Section, e Edit) *Attributes {
	a := &Attributes{VerticalMargin: Ptr(false)}
	if p := after.Attributes; p != nil {
		if p.Indentation != nil {
			a.Indentation = Ptr(*p.Indentation)
		}
		if p.ParagraphIndentation != nil {
			a.ParagraphIndentation = Ptr(*p.ParagraphIndentation)
		}
	}
	if e.Checked {
		a.Checked = Ptr(true)
	}
	return a
}

// PositionAfter is the position Slack's generatePositionAfter gives a new
// section after after (a position, maybe a new one's): a key between it
// and the next entity in the document's position order (0fa5.generate,
// with :temp), past any position already taken in this save.
func (d *Doc) PositionAfter(after string, taken []string) string {
	next := ""
	consider := func(p string) {
		if p > after && (next == "" || p < next) {
			next = p
		}
	}
	for _, s := range d.sections {
		if s.Deleted != nil && *s.Deleted || s.Position == nil {
			continue
		}
		consider(*s.Position)
	}
	for _, p := range taken {
		consider(p)
	}
	return Between(after, next)
}

// NewSectionIDFor is a new section id for the document (ZRIn
// .generateEntityId with its shard).
func (d *Doc) NewSectionIDFor() string { return NewSectionID(ShardChars(&d.Document)) }

// isTextStyle: a style a new section can take now (a paragraph, a
// heading; a list item is a plain section under its list).
func isTextStyle(style int32) bool {
	switch style {
	case StylePlain, StyleH1, StyleH2, StyleH3:
		return true
	}
	return false
}
