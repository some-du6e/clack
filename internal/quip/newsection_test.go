package quip

import (
	"encoding/hex"
	"reflect"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
)

type newSectionIn struct {
	ID             string         `json:"id"`
	Style          int32          `json:"style"`
	Position       string         `json:"position"`
	RTML           string         `json:"rtml"`
	After          map[string]any `json:"after"`
	ParentStyle    *int32         `json:"parentStyle"`
	ParentPosition string         `json:"parentPosition"`
	Checked        bool           `json:"checked"`
	Session        string         `json:"session"`
	SaveSeq        int64          `json:"saveSeq"`
	Team           string         `json:"team"`
}

// TestQuipNewSectionLikeSlack builds each case's new section after the
// section it follows, against what Slack's own entity saved for one its
// editor made there (K2FO.createNewSection: setSectionAttribute,
// initSibling, then toUpdateBundle and the activity row).
func TestQuipNewSectionLikeSlack(t *testing.T) {
	s := paritytest.Load[newSectionIn, saveSectionOut](t, "testdata/parity/quip-save-new-section.json")
	s.Run(t, paritytest.Check[newSectionIn, saveSectionOut]{Clack: func(t *testing.T, c newSectionIn) saveSectionOut {
		var after Section
		build(t, reflect.ValueOf(&after).Elem(), c.After)
		var parent *Section
		if c.ParentStyle != nil {
			parent = &Section{Style: Ptr(*c.ParentStyle), Position: Ptr(c.ParentPosition)}
		}
		e := Edit{Kind: EditNew, SectionID: c.ID, RTML: c.RTML, Style: c.Style, Position: c.Position, Checked: c.Checked, After: str(after.ID)}
		u, err := NewSection(e, &after, parent, c.Session+"-"+strconv.FormatInt(c.SaveSeq, 10), 1)
		if err != nil {
			t.Fatalf("NewSection: %v", err)
		}
		return saveSectionOut{Hex: hex.EncodeToString(Marshal(u)), LocalSeq: 1}
	}})
}
