package fakeslack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

func quipFake(t *testing.T) (*Server, *slack.Client) {
	t.Helper()
	s, err := Start(Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, slack.New(s.Creds())
}

func quipLoad(t *testing.T, c *slack.Client, thread string) *quip.Doc {
	t.Helper()
	b, err := c.CanvasCall(context.Background(), "load-data", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(quip.NewLoadRequest(thread))}})
	if err != nil {
		t.Fatalf("load-data: %v", err)
	}
	var w quip.LoadWrapper
	if err := quip.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	d, err := quip.ReadLoad(thread, &w)
	if err != nil {
		t.Fatalf("ReadLoad: %v", err)
	}
	return d
}

func quipFetch(t *testing.T, c *slack.Client, d *quip.Doc) (changed []string, reload bool) {
	t.Helper()
	b, err := c.CanvasCall(context.Background(), "fetch-latest", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(d.FetchRequest())}})
	if err != nil {
		t.Fatalf("fetch-latest: %v", err)
	}
	var r quip.FetchResponse
	if err := quip.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return d.Apply(&r)
}

// findText is the live text section whose plain text starts with prefix.
func findText(t *testing.T, d *quip.Doc, prefix string) *quip.Section {
	t.Helper()
	for _, s := range d.Sections() {
		if s.Type != nil && *s.Type == quip.TypeText && strings.HasPrefix(plainOfRTML(quip.SectionRTML(s)), prefix) {
			return s
		}
	}
	t.Fatalf("no text section starting %q", prefix)
	return nil
}

func TestQuipLoadData(t *testing.T) {
	s, c := quipFake(t)
	thread := s.QuipThreadID(CanvasShared)
	d := quipLoad(t, c, thread)
	if got := *d.Document.ID; got != quipDocID(&FakeCanvas{ID: CanvasShared}) || quip.ShardChars(&d.Document) != "FAK" {
		t.Errorf("document %q", got)
	}
	secs := d.Sections()
	if len(secs) == 0 || *secs[0].Type != quip.TypeTitle || plainOfRTML(quip.SectionRTML(secs[0])) != "Weekly sync notes" {
		t.Fatalf("first section %+v", secs[0])
	}
	if h := findText(t, d, "Weekly sync"); *h.Style != quip.StyleH1 {
		t.Errorf("heading style %d", *h.Style)
	}
	p := findText(t, d, "Agenda")
	if rt := quip.SectionRTML(p); !strings.Contains(rt, "<b>bold</b>") || !strings.Contains(rt, "<control id=") {
		t.Errorf("paragraph RTML %q", rt)
	}
	ctl := d.Controls(p)
	if len(ctl) != 1 {
		t.Fatalf("controls %v", ctl)
	}
	if k, id := quip.Control(ctl[0]); k != quip.ControlEmoji || id != "tada" || *ctl[0].AltRTML != ":tada:" {
		t.Errorf("control %v %q", k, id)
	}
	ship := findText(t, d, "Ship the beta")
	if ship.Parents == nil || len(ship.Parents.IDs) != 1 || *ship.Parents.ContainerStyle != quip.StyleChecklist || !*ship.Attributes.Checked {
		t.Errorf("checklist item %+v", ship)
	}
	if list := d.Section(ship.Parents.IDs[0]); list == nil || *list.Type != quip.TypeList || *list.Style != quip.StyleChecklist {
		t.Errorf("its list %+v", list)
	}
	if shots := findText(t, d, "Screenshots"); *shots.Attributes.Indentation != 1 {
		t.Errorf("nested item indentation %d", *shots.Attributes.Indentation)
	}
	if q := findText(t, d, "Keep it short"); *q.Style != quip.StyleBlockquote {
		t.Errorf("quote style %d", *q.Style)
	}
	// the export's section ids are the model's
	fc, _ := s.Canvas(CanvasShared)
	fc.Doc.Walk(func(b *canvas.Block) bool {
		if b.Kind.IsText() && d.Section(b.ID) == nil {
			t.Errorf("block %s (%q) has no section", b.ID, b.Text())
		}
		return true
	})
	if rs := s.QuipRequests(); len(rs) != 1 || rs[0].Path != "load-data" || rs[0].Status != 200 ||
		strings.Join(rs[0].Fields, ",") != "request_binary,token" || rs[0].Load == nil {
		t.Errorf("requests %+v", rs)
	}

	// preserialized: the same sections, as bytes
	s.PreserializeQuipSections(true)
	b, err := c.CanvasCall(context.Background(), "load-data", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(quip.NewLoadRequest(thread))}})
	if err != nil {
		t.Fatal(err)
	}
	var w quip.LoadWrapper
	if err := quip.Unmarshal(b, &w); err != nil || len(w.Response.Objects.Sections) != 0 || w.Preserialized == nil {
		t.Fatalf("preserialized answer: %v", err)
	}
	d2, err := quip.ReadLoad(thread, &w)
	if err != nil || len(d2.Sections()) != len(secs) {
		t.Errorf("preserialized: %d sections, %v (want %d)", len(d2.Sections()), err, len(secs))
	}

	// a thread nobody has: Quip's NOT_FOUND
	b, _ = c.CanvasCall(context.Background(), "load-data", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(quip.NewLoadRequest("NOSUCHTHRD"))}})
	_ = quip.Unmarshal(b, &w)
	var le *quip.LoadError
	if _, err := quip.ReadLoad("NOSUCHTHRD", &w); !errors.As(err, &le) || len(le.Reasons) != 1 || le.Reasons[0] != quip.FailNotFound {
		t.Errorf("unknown thread: %v", err)
	}
}

// rawCanvasCall posts body to /canvas/-/path as it is.
func rawCanvasCall(t *testing.T, s *Server, method, path, query, body string, h map[string]string) (int, string) {
	t.Helper()
	r, _ := http.NewRequest(method, s.URL+"canvas/-/"+path+"?"+query, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", "d=xoxd-fake")
	for k, v := range h {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestQuipRefusesWhatSlackWouldNotSend(t *testing.T) {
	s, _ := quipFake(t)
	thread := s.QuipThreadID(CanvasShared)
	good := func(m any) string {
		return "request_binary=" + strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(quip.MarshalBase64(m)) + "&token=" + OrgToken
	}
	load := quip.NewLoadRequest(thread)
	if code, _ := rawCanvasCall(t, s, "POST", "load-data", "_x_version_ts=noversion", good(load), nil); code != 200 {
		t.Fatalf("Slack's own request: %d", code)
	}
	raw := quip.Marshal(load)
	unpacked := append(append([]byte{}, raw...), 0x22, 0x00) // an empty parcel (field 4): Slack's request has none
	b64 := func(b []byte) string {
		return strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(base64.StdEncoding.EncodeToString(b))
	}
	two := &quip.LoadRequest{ObjectIDs: []string{thread, thread}, BulkLoads: load.BulkLoads}
	action := quip.NewLoadRequest(thread)
	action.UserAction = quip.Ptr("open")
	for _, c := range []struct {
		name, method, path, query, body string
		h                               map[string]string
		code                            int
		json                            string
	}{
		{"GET", "GET", "load-data", "_x_version_ts=noversion", good(load), nil, 405, ""},
		{"no version", "POST", "load-data", "", good(load), nil, 400, ""},
		{"a page version", "POST", "load-data", "_x_version_ts=123", good(load), nil, 400, ""},
		{"another call", "POST", "run-document-fixers", "_x_version_ts=noversion", good(load), nil, 404, ""},
		{"an Origin", "POST", "load-data", "_x_version_ts=noversion", good(load), map[string]string{"Origin": "https://app.slack.com"}, 400, ""},
		{"JSON body", "POST", "load-data", "_x_version_ts=noversion", good(load), map[string]string{"Content-Type": "application/json"}, 400, ""},
		{"no cookie", "POST", "load-data", "_x_version_ts=noversion", good(load), map[string]string{"Cookie": ""}, 200, "invalid_auth"},
		{"a plus", "POST", "load-data", "_x_version_ts=noversion", good(load) + "&x=a+b", nil, 400, ""},
		{"token first", "POST", "load-data", "_x_version_ts=noversion", "token=" + OrgToken + "&request_binary=" + b64(raw), nil, 400, ""},
		{"no token", "POST", "load-data", "_x_version_ts=noversion", "request_binary=" + b64(raw), nil, 400, ""},
		{"the team token", "POST", "load-data", "_x_version_ts=noversion", "request_binary=" + b64(raw) + "&token=" + TeamToken, nil, 200, "invalid_auth"},
		{"an extra field", "POST", "load-data", "_x_version_ts=noversion", good(load) + "&x=1", nil, 400, ""},
		// 26 bytes: standard base64 ends in "=" (Go's decoder skips line breaks even when strict)
		{"base64 without its padding", "POST", "load-data", "_x_version_ts=noversion", "request_binary=" + base64.RawStdEncoding.EncodeToString(unpacked) + "&token=" + OrgToken, nil, 400, ""},
		{"bytes Slack doesn't write", "POST", "load-data", "_x_version_ts=noversion", "request_binary=" + b64(unpacked) + "&token=" + OrgToken, nil, 400, ""},
		{"two threads", "POST", "load-data", "_x_version_ts=noversion", good(two), nil, 400, ""},
		{"a user action", "POST", "load-data", "_x_version_ts=noversion", good(action), nil, 400, ""},
		{"a fetch for nothing", "POST", "fetch-latest", "_x_version_ts=noversion", good(&quip.FetchRequest{}), nil, 400, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, body := rawCanvasCall(t, s, c.method, c.path, c.query, c.body, c.h)
			if code != c.code {
				t.Errorf("status %d, want %d (%s)", code, c.code, body)
			}
			if c.json != "" && !strings.Contains(body, `"error":"`+c.json+`"`) {
				t.Errorf("answer %q, want the %s error", body, c.json)
			}
			if rs := s.QuipRequests(); rs[len(rs)-1].Why == "" {
				t.Errorf("logged without a reason: %+v", rs[len(rs)-1])
			}
		})
	}
}

func TestQuipFetchFollowsOtherEdits(t *testing.T) {
	s, c := quipFake(t)
	d := quipLoad(t, c, s.QuipThreadID(CanvasShared))
	if changed, reload := quipFetch(t, c, d); len(changed) != 0 || reload {
		t.Errorf("nothing changed, yet %v %v", changed, reload)
	}
	max0 := *d.Document.MaxSectionSequence
	s.EditCanvasAsOther(CanvasShared, "Added by someone else")
	changed, _ := quipFetch(t, c, d)
	added := findText(t, d, "Added by someone else")
	if len(changed) != 1 || changed[0] != *added.ID || *added.Sequence <= max0 || *d.Document.MaxSectionSequence != *added.Sequence {
		t.Errorf("changed %v, section %+v, max %d -> %d", changed, added, max0, *d.Document.MaxSectionSequence)
	}
	if secs := d.Sections(); secs[len(secs)-1] != added {
		t.Errorf("the appended paragraph isn't last")
	}
	s.InsertCanvasAsOther(CanvasShared, 0, "First now")
	quipFetch(t, c, d)
	if secs := d.Sections(); plainOfRTML(quip.SectionRTML(secs[1])) != "First now" {
		t.Errorf("the inserted paragraph isn't right after the title: %q", plainOfRTML(quip.SectionRTML(secs[1])))
	}
	// another client removes a block: a deleted section, out of the index
	cs := s.canvasStore()
	cs.mu.Lock()
	fc := cs.byID[CanvasShared]
	gone := fc.Doc.Blocks[len(fc.Doc.Blocks)-1].ID
	fc.Doc.Blocks = fc.Doc.Blocks[:len(fc.Doc.Blocks)-1]
	cs.mu.Unlock()
	quipFetch(t, c, d)
	if sec := d.Section(gone); sec == nil || sec.Deleted == nil || !*sec.Deleted {
		t.Errorf("removed block's section %+v", sec)
	}
	for _, sec := range d.Sections() {
		if *sec.ID == gone {
			t.Error("a deleted section is still listed")
		}
	}
}

// save posts edit-document as Slack's client does.
func save(t *testing.T, c *slack.Client, d *quip.Doc, seq int, title string, data *quip.DocumentData) (*quip.EditAnswer, error) {
	t.Helper()
	form := []slack.CanvasField{{Name: "thread", Value: *d.Thread.ID}, {Name: "document", Value: *d.Document.ID}, {Name: "retry_count", Value: "0"},
		{Name: "session", Value: "0123456789ab"}, {Name: "sequence", Value: strconv.Itoa(seq)}, {Name: "data_binary", Value: quip.MarshalBase64(data)}}
	if title != "" {
		form = append(form, slack.CanvasField{Name: "title", Value: title})
	}
	b, err := c.CanvasCall(context.Background(), "edit-document", form)
	if err != nil {
		return nil, err
	}
	var a quip.EditAnswer
	if err := quip.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	return &a, nil
}

func TestQuipSaveShowsInTheExport(t *testing.T) {
	s, c := quipFake(t)
	d := quipLoad(t, c, s.QuipThreadID(CanvasShared))
	p := findText(t, d, "Agenda")
	edit := quip.Section{ID: p.ID, Sequence: p.Sequence, Content: &quip.Content{Text: &quip.ContentText{Text: quip.Ptr("Agenda <i>changed</i> &amp; kept")}},
		Modified: quip.Ptr(true), LocalSequence: quip.Ptr(int64(1)), TraceID: quip.Ptr("0123456789ab-0")}
	a, err := save(t, c, d, 1, "Weekly sync notes", &quip.DocumentData{Sections: []quip.Section{edit}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Changed == nil || !*a.Changed || a.FinalData == nil || len(a.FinalData.Sections) != 1 || !*a.IsNewEditSession {
		t.Fatalf("answer %+v", a)
	}
	got := a.FinalData.Sections[0]
	if *got.Sequence <= *p.Sequence || *got.ThreadID != *d.Thread.ID || *got.DocumentID != *d.Document.ID || quip.SectionRTML(&got) != "Agenda <i>changed</i> &amp; kept" {
		t.Errorf("saved section %+v", got)
	}
	fc, _ := s.Canvas(CanvasShared)
	b := fc.Doc.Find(*p.ID)
	if b == nil || b.Text() != "Agenda changed & kept" || len(b.Spans) != 3 || b.Spans[1].Style != richtext.Italic {
		t.Errorf("the export's block %+v", b)
	}
	// load again: the section is exactly as saved, once
	d2 := quipLoad(t, c, *d.Thread.ID)
	again := d2.Section(*p.ID)
	if quip.SectionRTML(again) != "Agenda <i>changed</i> &amp; kept" || *again.Sequence != *got.Sequence {
		t.Errorf("reloaded %q at %d, saved at %d", quip.SectionRTML(again), *again.Sequence, *got.Sequence)
	}
	// the client that saved sees it through fetch-latest too
	if changed, _ := quipFetch(t, c, d); len(changed) != 1 || changed[0] != *p.ID {
		t.Errorf("fetched %v", changed)
	}
	// a new paragraph after it, with a new mention
	ctl := quip.NewSectionID("FAK")
	np := quip.NewSectionID("FAK")
	next := ""
	secs := d2.Sections()
	for i, x := range secs {
		if *x.ID == *p.ID && i+1 < len(secs) {
			next = *secs[i+1].Position
		}
	}
	paraPos := quip.Between(*again.Position, next)
	// a control sits between its section's previous sibling and the
	// section (EKJU.positionPathForControl)
	ctrl := quip.Section{ID: quip.Ptr(ctl), Sequence: quip.Ptr(int64(0)), Type: quip.Ptr(quip.TypeSlackUser), SectionClass: quip.Ptr(quip.ClassControl),
		Content:  &quip.Content{SlackUser: &quip.SlackUser{SlackUserID: quip.Ptr(quip.UserID("U0FAKE00001"))}},
		Position: quip.Ptr(quip.Between(*again.Position, quip.StripTemp(paraPos)))}
	para := quip.Section{ID: quip.Ptr(np), Sequence: quip.Ptr(int64(0)), Type: quip.Ptr(quip.TypeText), Style: quip.Ptr(quip.StylePlain),
		SectionClass: quip.Ptr(quip.ClassSection), Position: quip.Ptr(paraPos),
		Content: &quip.Content{Text: &quip.ContentText{Text: quip.Ptr(`Ping <control id="` + ctl + `"></control>`)}}, Parents: &quip.Parents{}}
	if _, err := save(t, c, d2, 2, "Weekly sync notes", &quip.DocumentData{Sections: []quip.Section{ctrl, para}}); err != nil {
		t.Fatal(err)
	}
	fc, _ = s.Canvas(CanvasShared)
	nb := fc.Doc.Find(np)
	if nb == nil || len(nb.Spans) != 2 || nb.Spans[1].Kind != richtext.User || nb.Spans[1].ID != "U0FAKE00001" {
		t.Fatalf("the new paragraph %+v", nb)
	}
	d3 := quipLoad(t, c, *d.Thread.ID)
	if x := d3.Section(ctl); x == nil || x.Deleted != nil && *x.Deleted || len(d3.Controls(d3.Section(np))) != 1 {
		t.Errorf("the new control after a reload: %+v", x)
	}
}

func TestQuipSaveRefusals(t *testing.T) {
	s, c := quipFake(t)
	d := quipLoad(t, c, s.QuipThreadID(CanvasShared))
	p := findText(t, d, "Agenda")
	text := func(rt string) *quip.Content { return &quip.Content{Text: &quip.ContentText{Text: quip.Ptr(rt)}} }
	before, _ := json.Marshal(s.QuipSections(CanvasShared))
	newID := quip.NewSectionID("FAK")
	for _, k := range []struct {
		name  string
		sec   quip.Section
		title string
		seq   int
		code  int
	}{
		{"stale sequence", quip.Section{ID: p.ID, Sequence: quip.Ptr(*p.Sequence - 1), Content: text("x")}, "T", 1, 409},
		{"sequence ahead", quip.Section{ID: p.ID, Sequence: quip.Ptr(*p.Sequence + 1), Content: text("x")}, "T", 2, 400},
		{"no sequence", quip.Section{ID: p.ID, Content: text("x")}, "T", 3, 400},
		{"unknown id", quip.Section{ID: quip.Ptr("temp:C:XYZ0123"), Sequence: quip.Ptr(int64(0)), Content: text("x")}, "T", 4, 400},
		{"new id, sequence", quip.Section{ID: quip.Ptr(newID), Sequence: quip.Ptr(int64(5)), Position: quip.Ptr("zz:temp"), Content: text("x")}, "T", 5, 400},
		{"new id, no temp position", quip.Section{ID: quip.Ptr(newID), Sequence: quip.Ptr(int64(0)), Position: quip.Ptr("zz"), Content: text("x")}, "T", 6, 400},
		{"thread id", quip.Section{ID: p.ID, Sequence: p.Sequence, ThreadID: d.Thread.ID, Content: text("x")}, "T", 7, 400},
		{"merged state", quip.Section{ID: p.ID, Sequence: p.Sequence, MergedState: quip.Ptr(quip.MergedFromLocal)}, "T", 8, 400},
		{"type and style", quip.Section{ID: p.ID, Sequence: p.Sequence, Type: quip.Ptr(quip.TypeList), Style: quip.Ptr(quip.StyleH1)}, "T", 9, 400},
		{"missing control", quip.Section{ID: p.ID, Sequence: p.Sequence, Content: text(`<control id="temp:C:FAKnone"></control>`)}, "T", 10, 400},
		{"RTML Slack gives up on", quip.Section{ID: p.ID, Sequence: p.Sequence, Content: text("<b>a<b>b</b></b>")}, "T", 11, 400},
		{"invalid UTF-8", quip.Section{ID: p.ID, Sequence: p.Sequence, Content: text("a\xffb")}, "T", 12, 400},
		{"no title", quip.Section{ID: p.ID, Sequence: p.Sequence, Content: text("x")}, "", 13, 400},
	} {
		t.Run(k.name, func(t *testing.T) {
			_, err := save(t, c, d, k.seq, k.title, &quip.DocumentData{Sections: []quip.Section{k.sec}})
			var he *slack.HTTPError
			if !errors.As(err, &he) || he.Status != k.code {
				t.Errorf("save = %v, want HTTP %d", err, k.code)
			}
		})
	}
	// the session's sequence has to go up
	ok := quip.Section{ID: p.ID, Sequence: p.Sequence, Content: text("fine")}
	if _, err := save(t, c, d, 50, "T", &quip.DocumentData{Sections: []quip.Section{ok}}); err != nil {
		t.Fatalf("a good save: %v", err)
	}
	after, _ := json.Marshal(s.QuipSections(CanvasShared))
	if string(before) == string(after) {
		t.Error("the good save changed nothing")
	}
	var he *slack.HTTPError
	if _, err := save(t, c, d, 50, "T", &quip.DocumentData{Sections: []quip.Section{ok}}); !errors.As(err, &he) || he.Status != 409 {
		t.Errorf("the same session sequence again: %v", err)
	}
	// a canvas the viewer can only read
	cs := s.canvasStore()
	cs.mu.Lock()
	cs.byID[CanvasShared].ViewerAccess = "read"
	cs.mu.Unlock()
	if _, err := save(t, c, d, 60, "T", &quip.DocumentData{Sections: []quip.Section{ok}}); !errors.As(err, &he) || he.Status != 403 {
		t.Errorf("a read-only canvas: %v", err)
	}
}
