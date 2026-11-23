package quip

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
)

type saveSectionIn struct {
	Server map[string]any `json:"server"`
	Edit   struct {
		Kind    string `json:"kind"`
		RTML    string `json:"rtml"`
		Checked bool   `json:"checked"`
	} `json:"edit"`
	LocalSeq    int64  `json:"localSeq"`
	Session     string `json:"session"`
	SaveSeq     int64  `json:"saveSeq"`
	Team        string `json:"team"`
	ParentStyle *int32 `json:"parentStyle"`
}

type saveSectionOut struct {
	Hex      string `json:"hex"`
	LocalSeq int64  `json:"localSeq"`
}

// TestQuipSaveSectionLikeSlack builds each case's bundle section from the
// server's section and the edit, against what Slack's own section entity
// (EKJU over ZRIn) put in its bundle for the same state.
func TestQuipSaveSectionLikeSlack(t *testing.T) {
	s := paritytest.Load[saveSectionIn, saveSectionOut](t, "testdata/parity/quip-save-section.json")
	s.Run(t, paritytest.Check[saveSectionIn, saveSectionOut]{Clack: func(t *testing.T, c saveSectionIn) saveSectionOut {
		var server Section
		build(t, reflect.ValueOf(&server).Elem(), c.Server)
		e := Edit{SectionID: *server.ID, RTML: c.Edit.RTML, Checked: c.Edit.Checked}
		switch c.Edit.Kind {
		case "text":
			e.Kind = EditText
		case "check":
			e.Kind = EditCheck
		case "delete":
			e.Kind = EditDelete
		default:
			t.Fatalf("edit kind %q", c.Edit.Kind)
		}
		trace := c.Session + "-" + strconv.FormatInt(c.SaveSeq, 10)
		u, err := UpdateSection(&server, e, c.ParentStyle, trace, c.LocalSeq+1)
		if err != nil {
			t.Fatalf("UpdateSection: %v", err)
		}
		return saveSectionOut{Hex: hex.EncodeToString(Marshal(u)), LocalSeq: c.LocalSeq + 1}
	}})
}

type saveTitleOut struct {
	Title    string `json:"title"`
	RichText string `json:"richText"`
}

// TestQuipSaveTitleLikeSlack is the title metadata every save sends.
func TestQuipSaveTitleLikeSlack(t *testing.T) {
	type in struct {
		RTML string `json:"rtml"`
	}
	s := paritytest.Load[in, saveTitleOut](t, "testdata/parity/quip-save-title.json")
	s.Run(t, paritytest.Check[in, saveTitleOut]{Clack: func(t *testing.T, c in) saveTitleOut {
		m, err := TitleMetadata(c.RTML)
		if errors.Is(err, ErrUnsupported) {
			return saveTitleOut{Title: "(refused)"} // a known divergence names the case
		}
		if err != nil {
			t.Fatalf("TitleMetadata: %v", err)
		}
		if len(m.EmojiInfo) != 0 {
			t.Errorf("emoji info %v for a title without emoji", m.EmojiInfo)
		}
		return saveTitleOut{Title: m.Title, RichText: hex.EncodeToString(Marshal(m.RichText))}
	}})
}
