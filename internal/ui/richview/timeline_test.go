package richview

import (
	"image"
	"strings"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

func tlRich(s string) string {
	return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
}

// usedTools is a finished timeline of three points, the first two with
// steps, the first section with sources.
var usedTools = `[{"type":"timeline","block_id":"tl1","points":[
	{"id":"p1","status":"complete","text":"Searched","icon":"search","steps":[{"id":"s1","sections":[
		{"rich_text_title":` + tlRich("Looked around") + `,"sources":[{"type":"web","url":"https://a.test/x"}]},
		{"rich_text_title":` + tlRich("Read a doc") + `}]}]},
	{"id":"p2","status":"complete","text":"Checked","icon":"calendar"},
	{"id":"p3","status":"complete","text":"Summed up","icon":"search"}]}]`

type tlHandler struct {
	recHandler
	secs []*rt.TimelineSection
}

func (h *tlHandler) TimelineSources(sec *rt.TimelineSection, _ image.Rectangle) {
	h.secs = append(h.secs, sec)
}

// TimelineCondensed's header: "Used N tools" for a finished timeline of
// several points, the only point's title for one, the summary when there
// is one; the running one's last point's title while it's closed; the
// last three distinct icons before the text; the caret when it opens.
func TestTimelineHeader(t *testing.T) {
	st := newTestStyle()
	for _, c := range []struct {
		name, blocks, text string
		icons             int
		caret             bool
	}{
		{"used", usedTools, "Used 3 tools", 2, true},
		{"one point", `[{"type":"timeline","points":[{"id":"a","status":"complete","text":"Looked it up","icon":"clock"}]}]`, "Looked it up", 1, false},
		{"summary", `[{"type":"timeline","summary":` + tlRich("Planned it") + `,"points":[{"id":"a","status":"complete","text":"x"},{"id":"b","status":"error","text":"y","icon":"error"}]}]`, "Planned it", 1, true},
		{"running", `[{"type":"timeline","points":[{"id":"a","status":"complete","text":"Read"},{"id":"b","status":"in_progress","text":"Drafting"}]}]`, "Drafting", 0, true},
	} {
		v := layoutAt(st, kitDoc(t, c.blocks), Options{}, 600)
		got := strings.Join(fragStrings(&v.d), " ")
		if got != c.text {
			t.Errorf("%s: header %q, want %q", c.name, got, c.text)
		}
		icons, carets := 0, 0
		for i := range v.d.imgs {
			switch u := v.d.imgs[i].url; {
			case u == iconURL+"chevron-right" || u == iconURL+"chevron-down":
				carets++
			case strings.HasPrefix(u, iconURL):
				icons++
			}
		}
		if icons != c.icons || (carets == 1) != c.caret {
			t.Errorf("%s: %d icons, %d carets, want %d icons, caret %v", c.name, icons, carets, c.icons, c.caret)
		}
	}
	// the message's first block: no margin above
	v := layoutAt(st, kitDoc(t, usedTools), Options{}, 600)
	if f := findFrag(t, &v.d, "Used"); f.top > 2 {
		t.Errorf("first block's header at y %d, want 0", f.top)
	}
}

// A click on the header opens the point list: each point (more than one)
// and its sections, a dot on a point's first and last section, the line
// down the column; "(Sources)" opens the section's sources.
func TestTimelineOpen(t *testing.T) {
	h := &tlHandler{}
	d := kitDoc(t, usedTools)
	hs := newHarness(d, h)
	hs.w = 600
	hs.frame()
	tl := d.Blocks[0].Timeline
	if tl == nil {
		t.Fatalf("no timeline: %+v", d.Blocks)
	}
	hs.clickRect(image.Rect(10, 4, 60, 14), 0)
	if !hs.v.TimelineOpen(tl) {
		t.Fatal("the header's click didn't open it")
	}
	got := strings.Join(fragStrings(&hs.v.d), " ")
	for _, s := range []string{"Used 3 tools", "Searched", "Looked around", "(Sources)", "Read a doc", "Checked", "Summed up"} {
		if !strings.Contains(got, s) {
			t.Errorf("open: %q lacks %q", got, s)
		}
	}
	dots := 0
	for i := range hs.v.d.boxes {
		if b := &hs.v.d.boxes[i]; b.r.Dx() == 5 && b.r.Dy() == 5 && b.radius == 2 {
			dots++
		}
	}
	if dots != 2 {
		t.Errorf("%d dots, want 2 (the first point's first and last sections)", dots)
	}
	src := findFrag(t, &hs.v.d, "(Sources)")
	hs.clickRect(image.Rect(int(src.x)+2, src.top+2, int(src.x)+6, src.top+6), time.Second)
	if len(h.secs) != 1 || h.secs[0].Sources != 1 || len(h.secs[0].SourceList) != 1 {
		t.Errorf("(Sources): %+v", h.secs)
	}
	// and closed again
	hs.clickRect(image.Rect(10, 4, 60, 14), 2*time.Second)
	if hs.v.TimelineOpen(tl) {
		t.Errorf("a second click left it open")
	}
}

// A task attribution timeline draws nothing; the blocks around it do.
func TestTimelineHidden(t *testing.T) {
	v := layoutAt(newTestStyle(), kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},{"type":"timeline","block_id":"task_attribution_1","points":[{"id":"a","status":"complete","text":"Hidden"}]}]`), Options{}, 600)
	if got := strings.Join(fragStrings(&v.d), " "); got != "before" {
		t.Errorf("frags %q", got)
	}
}
