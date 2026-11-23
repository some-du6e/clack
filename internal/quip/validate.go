package quip

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
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
	for i := range data.Sections {
		u := &data.Sections[i]
		id := str(u.ID)
		e, ok := byID[id]
		if !ok {
			return refuse("section %s has no edit", id)
		}
		delete(byID, id)
		server := d.Section(id)
		if server == nil || server.Deleted != nil && *server.Deleted {
			return refuse("section %s isn't live in the model", id)
		}
		if p := pos[id]; p <= last {
			return refuse("section %s out of the model's order", id)
		} else {
			last = p
		}
		want := bundleFields[e.Kind]
		if e.Kind == EditText && e.Recheck { // the text and the box: content and attributes
			want = "1,2,9,12,13,16,19,20,33"
		}
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
		if e.Kind == EditCheck || e.Kind == EditText && e.Recheck {
			if u.Attributes == nil || u.Attributes.Checked == nil || *u.Attributes.Checked != e.Checked {
				return refuse("section %s: the box isn't set as edited", id)
			}
			want := bundleAttributes(server.Attributes)
			want.Checked = Ptr(e.Checked)
			if string(Marshal(want)) != string(Marshal(u.Attributes)) {
				return refuse("section %s: attributes other than checked changed", id)
			}
			if server.Parents == nil || len(server.Parents.IDs) == 0 {
				return refuse("section %s: a box outside a list", id)
			}
		}
		switch e.Kind {
		case EditText:
			if err := d.checkContent(u, server, e); err != nil {
				return err
			}
		case EditCheck: // the box, checked above
		case EditDelete:
			if server.Parents != nil && len(server.Parents.IDs) > 0 {
				return refuse("section %s: deleting a list item (its list's end isn't saved yet)", id)
			}
			if server.Type == nil || *server.Type != TypeText {
				return refuse("section %s: deleting a section of type %v", id, server.Type)
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
	// the section's mentions, emoji, dates and comment anchors stay as
	// they are: taking one out (Slack's editor changes only the text,
	// eju0.remove) comes with editing them
	old := SectionRTML(server)
	if !slices.Equal(ControlIDs(rtml), ControlIDs(old)) || !slices.Equal(AnnotationIDs(rtml), AnnotationIDs(old)) {
		return refuse("section %s: the edit takes out, adds or moves a mention, emoji, date or comment anchor", id)
	}
	return nil
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
