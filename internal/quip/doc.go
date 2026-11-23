package quip

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Doc is a canvas's Quip document as Clack holds it to edit (Slack's
// syncer model of one thread): load-data's answer, then each fetch-latest's
// changes, under the rule of Slack's update pass (7DQG.update): an update
// older than what is held (a lower sequence) is dropped. Sections are
// replaced whole, never changed in place, so a Section a caller got from
// the Doc stays as it was.
type Doc struct {
	Thread   Thread
	Document Document
	sections map[string]*Section
	// order is the Section/<document id> index: section id -> sort value
	// (the position_path).
	order map[string]string
	// added is the ids in the order they came into the model, the order
	// of Slack's entitiesById_ map, which a save's bundle follows.
	added []string
}

// ErrNotFound is load-data's answer without a response, which Slack's
// handleLoadDataResponseBinary_ fails with "Not found" (g47m).
var ErrNotFound = errors.New("quip: not found")

// LoadError is a load-data answer that refuses the thread: its error
// string, or its failures' reasons (FailAuth, FailNotFound, …).
type LoadError struct {
	Message string
	Reasons []int32
}

func (e *LoadError) Error() string {
	if e.Message != "" {
		return "quip: load-data: " + e.Message
	}
	return fmt.Sprintf("quip: load-data failed (reasons %v)", e.Reasons)
}

// Auth reports whether the load failed on access (AUTH, its no-request
// form, or the allowlist), not on the document.
func (e *LoadError) Auth() bool {
	for _, r := range e.Reasons {
		if r == FailAuth || r == FailAuthNoRequest || r == FailNotAllowed {
			return true
		}
	}
	return false
}

// NewLoadRequest is load-data's request for a canvas: its thread as an
// object id plus one THREAD bulk load (srhK.reloadThreadBulkLoad). Slack
// sends no user action without a tracer, and neither does Clack.
func NewLoadRequest(thread string) *LoadRequest {
	return &LoadRequest{ObjectIDs: []string{thread}, BulkLoads: []BulkLoad{{Type: Ptr(BulkThread), Arg0: Ptr(thread)}}}
}

// SectionIndex is the index load-data and fetch-latest keep a document's
// sections in.
func SectionIndex(document string) string { return "Section/" + document }

// ReadLoad makes a Doc of load-data's answer for thread: the thread, its
// document, the sections (in objects and preserialized, which
// M455.updateResponseWithPreserialized appends) and the section index.
func ReadLoad(thread string, w *LoadWrapper) (*Doc, error) {
	if w == nil {
		return nil, ErrNotFound
	}
	if w.Error != nil {
		return nil, &LoadError{Message: *w.Error}
	}
	r := w.Response
	if r == nil {
		return nil, ErrNotFound
	}
	if len(r.Failures) > 0 {
		e := &LoadError{}
		for _, f := range r.Failures {
			if f.Reason != nil {
				e.Reasons = append(e.Reasons, *f.Reason)
			} else {
				e.Reasons = append(e.Reasons, FailAuth) // the reason's default
			}
		}
		return nil, e
	}
	var p Payload
	if r.Objects != nil {
		p = *r.Objects
	}
	d := &Doc{sections: map[string]*Section{}, order: map[string]string{}}
	found := false
	for _, t := range p.Threads {
		if str(t.ID) == thread {
			d.Thread, found = t, true
		}
	}
	if !found {
		return nil, &LoadError{Message: "no thread " + thread + " in the answer"}
	}
	docID := str(d.Thread.DocumentID)
	found = false
	for _, doc := range p.Documents {
		if id := str(doc.ID); id != "" && (id == docID || docID == "" && str(doc.ThreadID) == thread) {
			d.Document, found = doc, true
		}
	}
	if !found {
		return nil, &LoadError{Message: "no document for thread " + thread + " in the answer"}
	}
	for i := range p.Sections {
		d.put(&p.Sections[i])
	}
	if w.Preserialized != nil {
		for _, it := range w.Preserialized.Items {
			if it.IDType == nil || *it.IDType != IDTypeSection {
				continue // spreadsheet cells
			}
			for _, v := range it.Values {
				var s Section
				if err := Unmarshal(v, &s); err != nil {
					return nil, fmt.Errorf("quip: a preserialized section: %w", err)
				}
				d.put(&s)
			}
		}
	}
	for _, ix := range r.Indexes {
		if str(ix.Name) != SectionIndex(str(d.Document.ID)) {
			continue
		}
		for _, e := range ix.Entries {
			if id := str(e.ObjectID); id != "" {
				d.order[id] = str(e.SortValue)
			}
		}
	}
	return d, nil
}

// put holds s unless the Doc has a newer one; false when dropped.
func (d *Doc) put(s *Section) bool {
	id := str(s.ID)
	if id == "" {
		return false
	}
	if old := d.sections[id]; old != nil && seq(s.Sequence) < seq(old.Sequence) {
		return false
	}
	c := *s
	if _, known := d.sections[id]; !known {
		d.added = append(d.added, id)
	}
	d.sections[id] = &c
	return true
}

// inOrder is every section id as it came into the model.
func (d *Doc) inOrder() []string { return d.added }

// ApplySaved takes edit-document's answer: the stored versions of the
// saved sections replace the model's (they are newer), as Slack applies a
// save answer (document.update with the POST response). changed false is
// a save that changed nothing.
func (d *Doc) ApplySaved(a *EditAnswer) []string {
	if a == nil || a.FinalData == nil {
		return nil
	}
	var out []string
	for i := range a.FinalData.Sections {
		s := &a.FinalData.Sections[i]
		if d.put(s) {
			out = append(out, str(s.ID))
		}
		if s.PositionPath != nil && (s.Deleted == nil || !*s.Deleted) {
			d.order[str(s.ID)] = *s.PositionPath
		} else if s.Deleted != nil && *s.Deleted {
			delete(d.order, str(s.ID))
		}
	}
	return out
}

func seq(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// FetchRequest is fetch-latest's request for what changed since the Doc
// (HVRg.fetchLatest_): the thread's sequence and the document's
// max_section_sequence.
func (d *Doc) FetchRequest() *FetchRequest {
	return &FetchRequest{Requests: []FetchThread{{
		ThreadID:                 Ptr(str(d.Thread.ID)),
		ClientThreadSequence:     Ptr(seq(d.Thread.Sequence)),
		ClientMaxSectionSequence: Ptr(seq(d.Document.MaxSectionSequence)),
	}}}
}

// Apply takes fetch-latest's answer. It reports the sections it changed
// (in the answer's order), and reload when Slack would load the thread
// again instead (the payload's backlog_reload_model_data, or the section
// index asking for a reload); then nothing is applied.
func (d *Doc) Apply(r *FetchResponse) (changed []string, reload bool) {
	if r == nil || r.Changes == nil {
		return nil, false
	}
	c := r.Changes
	if c.Data != nil && c.Data.BacklogReloadModelData != nil && *c.Data.BacklogReloadModelData {
		return nil, true
	}
	index := SectionIndex(str(d.Document.ID))
	for _, ix := range c.Indexes {
		if str(ix.IndexID) == index && ix.Reload != nil && *ix.Reload {
			return nil, true
		}
	}
	for _, t := range c.TempIDs {
		d.rename(str(t.TempID), str(t.RealID))
	}
	if c.Data != nil {
		for _, t := range c.Data.Threads {
			if str(t.ID) == str(d.Thread.ID) && seq(t.Sequence) >= seq(d.Thread.Sequence) {
				d.Thread = t
			}
		}
		for _, doc := range c.Data.Documents {
			if str(doc.ID) == str(d.Document.ID) && seq(doc.Sequence) >= seq(d.Document.Sequence) {
				d.Document = doc
			}
		}
		for i := range c.Data.Sections {
			s := &c.Data.Sections[i]
			if dd := str(s.DocumentID); dd != "" && dd != str(d.Document.ID) {
				continue
			}
			if d.put(s) {
				changed = append(changed, str(s.ID))
			}
		}
	}
	for _, ix := range c.Indexes {
		if str(ix.IndexID) != index {
			continue
		}
		for _, e := range ix.ChangedEntries {
			id := str(e.ID)
			if e.Remove != nil && *e.Remove {
				delete(d.order, id)
			} else if id != "" {
				d.order[id] = str(e.SortValue)
			}
		}
	}
	return changed, false
}

// rename moves a section the server gave a real id (ChangesData.temp_ids).
func (d *Doc) rename(temp, real string) {
	if temp == "" || real == "" || temp == real {
		return
	}
	if s := d.sections[temp]; s != nil {
		c := *s
		c.ID = Ptr(real)
		delete(d.sections, temp)
		if old := d.sections[real]; old == nil || seq(old.Sequence) <= seq(c.Sequence) {
			if old == nil {
				// Slack's entityIdDidChange deletes the old key and sets the
				// new one: the entity moves to the end of the map
				d.added = append(d.added, real)
			}
			d.sections[real] = &c
		}
		for i, x := range d.added {
			if x == temp {
				d.added = append(d.added[:i], d.added[i+1:]...)
				break
			}
		}
	}
	if v, ok := d.order[temp]; ok {
		delete(d.order, temp)
		if _, ok := d.order[real]; !ok {
			d.order[real] = v
		}
	}
}

// Section is the section with id, deleted or not (nil: unknown).
func (d *Doc) Section(id string) *Section { return d.sections[id] }

// Sections are the document's sections that aren't deleted, in document
// order: the section index's sort value, else the section's own
// position_path, then the id.
func (d *Doc) Sections() []*Section {
	var out []*Section
	for _, s := range d.sections {
		if s.Deleted == nil || !*s.Deleted {
			out = append(out, s)
		}
	}
	key := func(s *Section) string {
		if v, ok := d.order[str(s.ID)]; ok {
			return v
		}
		return str(s.PositionPath)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := key(out[i]), key(out[j])
		if a != b {
			return a < b
		}
		return str(out[i].ID) < str(out[j].ID)
	})
	return out
}

// Controls are the control sections a text section's RTML refers to, in
// order (a missing one is left out).
func (d *Doc) Controls(s *Section) []*Section {
	var out []*Section
	for _, id := range ControlIDs(SectionRTML(s)) {
		if c := d.sections[id]; c != nil {
			out = append(out, c)
		}
	}
	return out
}

// SectionRTML is a text section's RTML (content.text) or the title's
// (content.title); "" for any other section.
func SectionRTML(s *Section) string {
	if s == nil || s.Content == nil {
		return ""
	}
	switch {
	case s.Content.Text != nil:
		return str(s.Content.Text.Text)
	case s.Content.Title != nil:
		return str(s.Content.Title.Text)
	}
	return ""
}

// ControlKind says what a control section is.
type ControlKind int

const (
	ControlNone ControlKind = iota // not a control Clack knows
	ControlUser                    // a person or a group (su:U…, su:S…)
	ControlChannel
	ControlEmoji
	ControlBroadcast // @here, @channel, @everyone
	ControlDate
)

// Control reports a control section's kind and its Slack id: the user,
// group or channel id without its syncer prefix, the emoji's name, the
// broadcast's range; a date has none.
func Control(s *Section) (ControlKind, string) {
	if s == nil || s.Content == nil || s.SectionClass == nil || *s.SectionClass != ClassControl {
		return ControlNone, ""
	}
	c := s.Content
	switch {
	case c.SlackUser != nil:
		return ControlUser, SlackID(str(c.SlackUser.SlackUserID))
	case c.SlackChannel != nil:
		return ControlChannel, SlackID(str(c.SlackChannel.SlackChannelID))
	case c.SlackEmoji != nil:
		if n := str(c.SlackEmoji.EmojiName); n != "" {
			return ControlEmoji, n
		}
		id := strings.TrimPrefix(str(c.SlackEmoji.SlackEmojiID), "se:")
		if i := strings.LastIndexByte(id, '/'); i >= 0 {
			id = id[:i]
		}
		return ControlEmoji, id
	case c.SlackBroadcast != nil:
		return ControlBroadcast, str(c.SlackBroadcast.Range)
	case c.SlackDate != nil:
		return ControlDate, ""
	}
	return ControlNone, ""
}

// ControlIDs are the ids of the controls in rtml, in order, and
// AnnotationIDs its annotations', as W77Y's lz handler collects them (a
// control's data-id, else its id).
func ControlIDs(rtml string) []string {
	c := &idCollector{}
	if root := rtmlRoot(rtml); root != nil {
		walkRTML(root, c)
	}
	return c.controls
}

// AnnotationIDs: see ControlIDs.
func AnnotationIDs(rtml string) []string {
	c := &idCollector{}
	if root := rtmlRoot(rtml); root != nil {
		walkRTML(root, c)
	}
	return c.annotations
}

// idCollector is W77Y's lz: the ids of controls and annotations.
type idCollector struct {
	controls, annotations []string
}

func (c *idCollector) text(string)                          {}
func (c *idCollector) lineBreak()                           {}
func (c *idCollector) control(id string)                    { c.controls = append(c.controls, id) }
func (c *idCollector) annotationStart(id string)            { c.annotations = append(c.annotations, id) }
func (c *idCollector) linkStart(string, bool, string)       {}
func (c *idCollector) formattingStart(string, string, bool) {}
func (c *idCollector) markEnd(string) bool                  { return true }
