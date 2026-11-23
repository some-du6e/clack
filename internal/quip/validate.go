package quip

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrRefused wraps every reason Validate refuses a save.
var ErrRefused = errors.New("quip: save refused")

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRefused}, args...)...)
}

// bundleFields are the fields each edit's bundle section carries, as
// ZRIn.toUpdateBundle copies them (the activity row, 39, only when it
// applies): nothing a server writes (thread_id, document_id, status,
// temp_id, merged_state, crdt_session, …) and nothing an edit didn't touch.
var bundleFields = map[EditKind]string{
	EditText:   "1,2,9,12,13,19,20,33",
	EditCheck:  "1,2,9,13,16,19,20,33",
	EditDelete: "1,2,3,9,13,19,20,33",
}

// fieldsFor is bundleFields for e with its restyle (style, 10) and its
// level or box (attributes, 16): "" for a kind that has none.
func fieldsFor(e Edit) string {
	base, ok := bundleFields[e.Kind]
	if e.Kind == EditBlock {
		base, ok = "1,2,9,13,19,20,33", true
	}
	if !ok {
		return ""
	}
	set := map[int]bool{}
	for _, f := range strings.Split(base, ",") {
		n, _ := strconv.Atoi(f)
		set[n] = true
	}
	if e.Restyle {
		set[10] = true
	}
	if e.Reindent || e.Recheck {
		set[16] = true
	}
	var out []int
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	var b strings.Builder
	for i, n := range out {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// maxIndent is the deepest list level a save sets: the canvas editor's
// maxListIndentationLevel (changeIndentationLevel caps at it).
const maxIndent = 4

// savedTags are the tags Slack's save writer (CITi's DI) writes in a
// canvas: sub, sup and f are the canvas's disallowedFormattingTags.
var savedTags = map[string]bool{"a": true, "annotation": true, "b": true, "i": true, "u": true, "del": true, "code": true}

// Validate checks a save before it leaves Clack, against the model and
// the edits it was made for, independently of the code that built it: one
// section per edit in the model's order, each with exactly the fields its
// edit carries, the model's sequence, type and parents, the save's local
// sequence and trace, RTML Slack's writer would write (it parses, only its
// tags, safe links, no empty mark, no raw control characters, every
// control in the model), valid UTF-8, a byte-for-byte round trip, and the
// title metadata the model's title gives. Any failure refuses the save.
func (d *Doc) Validate(data *DocumentData, edits []Edit, meta *TitleMeta, s *Save) error {
	if data == nil || s == nil || meta == nil {
		return refuse("nothing to check")
	}
	if len(data.Sections) != len(edits) {
		return refuse("%d sections for %d edits", len(data.Sections), len(edits))
	}
	byID := map[string]Edit{}
	for _, e := range edits {
		byID[e.SectionID] = e
	}
	pos := map[string]int{}
	for i, id := range d.inOrder() {
		pos[id] = i
	}
	last := -1
	made := map[string]*Section{} // the new sections checked so far
	deleted := map[string]bool{}  // the sections this save deletes
	for _, e := range edits {
		if e.Kind == EditDelete {
			deleted[e.SectionID] = true
		}
	}
	for i := range data.Sections {
		u := &data.Sections[i]
		id := str(u.ID)
		e, ok := byID[id]
		if !ok {
			return refuse("section %s has no edit", id)
		}
		delete(byID, id)
		if e.Kind == EditNew {
			if err := d.checkNew(u, e, s, made); err != nil {
				return err
			}
			made[id] = u
			continue
		}
		if len(made) > 0 {
			return refuse("section %s after a new section (entitiesById_: new ones come last)", id)
		}
		server := d.Section(id)
		if server == nil || server.Deleted != nil && *server.Deleted {
			return refuse("section %s isn't live in the model", id)
		}
		if p := pos[id]; p <= last {
			return refuse("section %s out of the model's order", id)
		} else {
			last = p
		}
		want := fieldsFor(e)
		got := FieldsString(u)
		if u.ActivityStackUpdate != nil {
			got = strings.Replace(got, ",39", "", 1)
		}
		if want == "" || got != want {
			return refuse("section %s: fields %s, a %v edit carries %s", id, FieldsString(u), e.Kind, want)
		}
		switch {
		case seq(u.Sequence) != seq(server.Sequence):
			return refuse("section %s: sequence %d, the model's is %d", id, seq(u.Sequence), seq(server.Sequence))
		case e.Base == 0 || e.Base != seq(server.Sequence):
			// never over someone else's save: Slack would merge it in
			return refuse("section %s: edited at sequence %d, the model's is %d", id, e.Base, seq(server.Sequence))
		case !reflect.DeepEqual(u.Type, server.Type) && !(u.Type != nil && *u.Type == 0 && server.Type == nil):
			return refuse("section %s: type changed", id)
		case str(u.TraceID) != s.Trace:
			return refuse("section %s: trace %q, the save's is %q", id, str(u.TraceID), s.Trace)
		case u.LocalSequence == nil || *u.LocalSequence != s.local[id] || *u.LocalSequence < 1:
			return refuse("section %s: local sequence %v", id, u.LocalSequence)
		case u.Modified == nil || !*u.Modified:
			return refuse("section %s: not marked modified", id)
		}
		if err := d.checkParents(u, server); err != nil {
			return err
		}
		if a, ok := activityRow(u, server); ok != (u.ActivityStackUpdate != nil) || ok && a != *u.ActivityStackUpdate {
			return refuse("section %s: activity row %v, Slack's would be %v (%v)", id, u.ActivityStackUpdate, a, ok)
		}
		inList := server.Parents != nil && len(server.Parents.IDs) > 0
		if e.Kind == EditCheck || e.Recheck || e.Reindent {
			// the server's attributes as update() loads them, with the box
			// or the level the edit set (setSectionAttribute)
			want := bundleAttributes(server.Attributes)
			if e.Kind == EditCheck || e.Recheck {
				want.Checked = Ptr(e.Checked)
			}
			if e.Reindent {
				want.Indentation = Ptr(e.Indent)
			}
			if u.Attributes == nil || string(Marshal(want)) != string(Marshal(u.Attributes)) {
				return refuse("section %s: attributes other than the box or level edited", id)
			}
			if !inList {
				return refuse("section %s: a box or level outside a list", id)
			}
		}
		if e.Reindent && (e.Indent < 0 || e.Indent > maxIndent) {
			return refuse("section %s: list level %d", id, e.Indent)
		}
		if e.Restyle {
			// between a paragraph and the headings of a top-level text
			// section: the content's type stays (setSectionStyle marks
			// STYLE alone)
			old := int32(0)
			if server.Style != nil {
				old = *server.Style
			}
			if u.Style == nil || *u.Style != e.Style || inList || !isTextStyle(e.Style) || !isTextStyle(old) || e.Style == old {
				return refuse("section %s: style %d to %d", id, old, e.Style)
			}
		}
		switch e.Kind {
		case EditText:
			if err := d.checkContent(u, server, e); err != nil {
				return err
			}
		case EditCheck:
			if u.Attributes == nil || u.Attributes.Checked == nil || *u.Attributes.Checked != e.Checked {
				return refuse("section %s: the box isn't set as edited", id)
			}
		case EditBlock:
			if !e.Restyle && !e.Reindent && !e.Recheck {
				return refuse("section %s: a block edit that changes nothing", id)
			}
		case EditDelete:
			switch {
			case inList:
				// a list item: its list keeps an item, or goes with the
				// last one (notifyDescendantDeleted_ deletes it)
				list, left := server.Parents.IDs[0], false
				for _, c := range d.ListChildren(server.Parents.IDs[0]) {
					left = left || c != id && !deleted[c]
				}
				if !left && !deleted[list] {
					return refuse("section %s: the last item of list %s deleted without the list", id, list)
				}
				if server.Type == nil || *server.Type != TypeText {
					return refuse("section %s: deleting a list item of type %v", id, server.Type)
				}
			case server.Type != nil && *server.Type == TypeList:
				// a list goes only with all its items
				for _, c := range d.ListChildren(id) {
					if !deleted[c] {
						return refuse("section %s: deleting a list with item %s left", id, c)
					}
				}
			case server.Type == nil || *server.Type != TypeText:
				return refuse("section %s: deleting a section of type %v", id, server.Type)
			case d.DeleteMerges(id):
				return refuse("section %s: deleting it leaves two sections Slack's editor would merge", id)
			}
		}
	}
	if len(byID) > 0 {
		return refuse("%d edits have no section", len(byID))
	}
	if !ValidUTF8(data) {
		return refuse("invalid UTF-8")
	}
	// the bytes read back to the same save, with nothing unknown
	var back DocumentData
	if err := Unmarshal(Marshal(data), &back); err != nil {
		return refuse("the save doesn't read back: %v", err)
	}
	for _, sec := range back.Sections {
		if len(sec.Unknown) > 0 {
			return refuse("unknown fields %v", sec.Unknown)
		}
	}
	if string(Marshal(&back)) != string(Marshal(data)) {
		return refuse("the save doesn't write back to the same bytes")
	}
	return d.checkTitle(data, edits, meta)
}

// newFields are a new section's bundle fields (the activity row, 39, when
// it has text): Slack's own entity's, quip-save-new-section.
const newFields = "1,2,3,8,9,10,11,12,13,16,19,20,21,33,35"

// checkNew holds a new section to what Slack's editor saves for one it
// just made after e.After (K2FO.createNewSectionAtPosition, initSibling).
func (d *Doc) checkNew(u *Section, e Edit, s *Save, made map[string]*Section) error {
	id := str(u.ID)
	if !IsNewSectionID(id, ShardChars(&d.Document)) {
		return refuse("new section %s: not a new section id for the document's shard", id)
	}
	if d.Section(id) != nil {
		return refuse("new section %s is in the model already", id)
	}
	got := FieldsString(u)
	if u.ActivityStackUpdate != nil {
		got = strings.Replace(got, ",39", "", 1)
	}
	if got != newFields {
		return refuse("new section %s: fields %s, a new section carries %s", id, FieldsString(u), newFields)
	}
	after := made[e.After]
	if after == nil {
		if a := d.Section(e.After); a != nil && (a.Deleted == nil || !*a.Deleted) {
			after = a
		}
	}
	if after == nil {
		return refuse("new section %s: after %s, which isn't in the model or this save", id, e.After)
	}
	var parent *Section
	if after.Parents != nil && len(after.Parents.IDs) > 0 {
		parent = d.Section(after.Parents.IDs[0])
	}
	switch {
	case seq(u.Sequence) != 0 || *u.Deleted || *u.ReadOnly || *u.SectionClass != ClassSection || !*u.Modified:
		return refuse("new section %s: sequence, deleted, read_only, class or modified isn't a new section's", id)
	case str(u.TraceID) != s.Trace:
		return refuse("new section %s: trace %q, the save's is %q", id, str(u.TraceID), s.Trace)
	case *u.LocalSequence < 1 || *u.LocalSequence != s.local[id]:
		return refuse("new section %s: local sequence %d", id, *u.LocalSequence)
	case *u.Type != TypeText:
		return refuse("new section %s: type %d", id, *u.Type)
	case parent != nil && *u.Style != StylePlain, parent == nil && !isTextStyle(*u.Style):
		return refuse("new section %s: style %d after a section with parents %v", id, *u.Style, parent != nil)
	}
	// its position: a :temp key right after the one it follows, before
	// any other entity
	p, ap := str(u.Position), str(after.Position)
	if !IsTemp(p) || p != e.Position || !(p > ap) {
		return refuse("new section %s: position %q after %q", id, p, ap)
	}
	for _, x := range d.sections {
		if x.Position != nil && (x.Deleted == nil || !*x.Deleted) && *x.Position > ap && *x.Position < p {
			return refuse("new section %s: position %q isn't next to %q (%s sits between)", id, p, ap, str(x.ID))
		}
	}
	wantPath := p
	if parent != nil && parent.Position != nil {
		wantPath = *parent.Position + "-" + p
	}
	if str(u.PositionPath) != wantPath {
		return refuse("new section %s: position path %q, want %q", id, str(u.PositionPath), wantPath)
	}
	// initSibling's parents, and the attributes it and
	// createNewSectionAtPosition set
	var wantIDs []string
	if after.Parents != nil {
		wantIDs = after.Parents.IDs
	}
	if !slices.Equal(u.Parents.IDs, wantIDs) || u.Parents.ContainerID != nil {
		return refuse("new section %s: parents %v, the section before has %v", id, u.Parents.IDs, wantIDs)
	}
	if parent != nil && parent.Style != nil {
		if u.Parents.ContainerStyle == nil || *u.Parents.ContainerStyle != *parent.Style {
			return refuse("new section %s: container style", id)
		}
	} else if u.Parents.ContainerStyle != nil {
		return refuse("new section %s: a container style without a parent the model has", id)
	}
	want := &Attributes{VerticalMargin: Ptr(false)} // no text alignment: the canvas editor suppresses it
	if a := after.Attributes; a != nil {
		want.Indentation, want.ParagraphIndentation = a.Indentation, a.ParagraphIndentation
	}
	if e.Checked {
		want.Checked = Ptr(true)
	}
	if string(Marshal(want)) != string(Marshal(u.Attributes)) {
		return refuse("new section %s: attributes other than a new section's", id)
	}
	// typed text's content, nothing else
	c := u.Content
	if c == nil || FieldsString(c) != "1" || c.Text == nil || FieldsString(c.Text) != "1" {
		return refuse("new section %s: content other than text", id)
	}
	rtml := str(c.Text.Text)
	if rtml != e.RTML {
		return refuse("new section %s: not the edited RTML", id)
	}
	if err := d.checkRTML(id, rtml); err != nil {
		return err
	}
	if len(ControlIDs(rtml)) > 0 || len(AnnotationIDs(rtml)) > 0 {
		return refuse("new section %s: a mention, emoji, date or comment anchor in a new section", id)
	}
	if a, ok := activityRow(u, nil); ok != (u.ActivityStackUpdate != nil) || ok && a != *u.ActivityStackUpdate {
		return refuse("new section %s: activity row", id)
	}
	return nil
}

func (d *Doc) checkParents(u, server *Section) error {
	id := str(u.ID)
	if u.Parents == nil {
		return refuse("section %s: no parents (toPb always sets them)", id)
	}
	var ids []string
	if server.Parents != nil {
		ids = server.Parents.IDs
	}
	if !reflect.DeepEqual(append([]string{}, u.Parents.IDs...), append([]string{}, ids...)) || u.Parents.ContainerID != nil {
		return refuse("section %s: parents changed", id)
	}
	if len(ids) == 0 {
		if u.Parents.ContainerStyle != nil {
			return refuse("section %s: a container style without a parent", id)
		}
		return nil
	}
	p := d.Section(ids[0])
	switch {
	case p == nil && u.Parents.ContainerStyle != nil:
		return refuse("section %s: a container style for a parent the model lacks", id)
	case p != nil && (p.Style == nil) != (u.Parents.ContainerStyle == nil):
		return refuse("section %s: its parent's style", id)
	case p != nil && p.Style != nil && *p.Style != *u.Parents.ContainerStyle:
		return refuse("section %s: container style %d, its list's is %d", id, *u.Parents.ContainerStyle, *p.Style)
	}
	return nil
}

func (d *Doc) checkContent(u, server *Section, e Edit) error {
	id := str(u.ID)
	c := u.Content
	if c == nil || len(c.Other) > 0 {
		return refuse("section %s: content", id)
	}
	var rtml string
	isTitle := server.Type != nil && *server.Type == TypeTitle
	switch {
	case isTitle:
		if c.Title == nil || c.Text != nil || FieldsString(c) != "58" || FieldsString(c.Title) != "1" {
			return refuse("section %s: the title's content is only its text", id)
		}
		rtml = str(c.Title.Text)
		if utf8.RuneCountInString(rtml) > 2000 {
			return refuse("section %s: the title is over 2000 characters", id)
		}
	default:
		if c.Text == nil || FieldsString(c) != "1" {
			return refuse("section %s: a text section's content is its text", id)
		}
		f := FieldsString(c.Text)
		var old *string
		if server.Content != nil && server.Content.Text != nil {
			old = server.Content.Text.Language
		}
		if (old == nil && f != "1") || (old != nil && (f != "1,2" || *c.Text.Language != *old)) {
			return refuse("section %s: the code language changed", id)
		}
		rtml = str(c.Text.Text)
	}
	if rtml != e.RTML {
		return refuse("section %s: not the edited RTML", id)
	}
	if err := d.checkRTML(id, rtml); err != nil {
		return err
	}
	// an edit may take the section's mentions, emoji, dates and comment
	// anchors out (Slack's editor changes only the text, eju0.remove) or
	// move them, but adds none: a new one is a control section of its own
	old := SectionRTML(server)
	if !subsetOnce(ControlIDs(rtml), ControlIDs(old)) || !subsetOnce(AnnotationIDs(rtml), AnnotationIDs(old)) {
		return refuse("section %s: the edit adds a mention, emoji, date or comment anchor, or names one twice", id)
	}
	return nil
}

// subsetOnce reports whether every id of got is one of have's, none twice.
func subsetOnce(got, have []string) bool {
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] || !slices.Contains(have, id) {
			return false
		}
		seen[id] = true
	}
	return true
}

// checkRTML holds a section's RTML to what Slack's save writer writes.
func (d *Doc) checkRTML(id, rtml string) error {
	for _, r := range rtml {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return refuse("section %s: a raw control character (escapeText takes them out)", id)
		}
	}
	text, marks, ok := ParseRTML(rtml)
	if !ok {
		return refuse("section %s: RTML Slack can't parse (a tag inside the same tag)", id)
	}
	for _, m := range marks {
		if !savedTags[m.Name] {
			return refuse("section %s: <%s> (Slack's writer drops it in a canvas)", id, m.Name)
		}
		if m.Start == m.End {
			// the editor's getCompactRtml loses the text after an empty
			// mark: never send one, whatever Slack's own writer would
			return refuse("section %s: an empty <%s> (getCompactRtml would drop the text after it)", id, m.Name)
		}
		if m.Name == "a" {
			l, ok := parseLinkValue(m.Value)
			if !ok || !IsSafeURI(l.href, false) {
				return refuse("section %s: a link Slack wouldn't keep", id)
			}
		}
	}
	_ = text
	for _, cid := range ControlIDs(rtml) {
		if c := d.Section(cid); c == nil || c.Deleted != nil && *c.Deleted {
			return refuse("section %s: control %s isn't in the model", id, cid)
		}
	}
	return nil
}

// checkTitle recomputes the title metadata from the model (and this
// save's title edit) and holds the save to it.
func (d *Doc) checkTitle(data *DocumentData, edits []Edit, meta *TitleMeta) error {
	title := d.titleSection()
	if title == nil {
		return refuse("no title section")
	}
	rtml := SectionRTML(title)
	if e, ok := find(edits, str(title.ID)); ok && e.Kind == EditText {
		rtml = e.RTML
	}
	want, err := TitleMetadata(rtml)
	if err != nil {
		return refuse("the title: %v", err)
	}
	switch {
	case meta.Title != want.Title || utf16Len(meta.Title) > MaxTitleLength:
		return refuse("title %q, the title section gives %q", meta.Title, want.Title)
	case string(Marshal(data.TitleRichText)) != string(Marshal(want.RichText)):
		return refuse("the title's rich text isn't the title section's")
	case len(data.TitleSlackEmojiInfo) != len(want.EmojiInfo) || data.DocumentID != nil || len(data.Other) > 0:
		return refuse("the document data carries more than the sections and the title")
	}
	return nil
}
