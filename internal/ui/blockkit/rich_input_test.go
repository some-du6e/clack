package blockkit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// Karim, 2026-11-13: an app's "Quick check" modal asked for a word
// struck through in a rich_text_input, and Clack drew a plain box with no
// way to format it. Slack's RichTextInputElement is the composer's
// WysiwygContainer (formatting bar, Aa and emoji buttons, the keyboard
// and markdown shortcuts). Typing "mango", selecting it and striking it
// (Mod+Shift+X) submits a rich_text_value whose text carries style strike,
// as Slack's view_submission state does.
func TestRichTextInputStrikeSubmits(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) { richStrikeSubmits(t, dark, mode) })
	}
}

func richStrikeSubmits(t *testing.T, dark bool, mode string) {
	e := newBKEnv(t, dark, true)
	v, err := core.ParseView(mustJSON(fakeslack.SurfQuickCheck()))
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(e.h, v)
	e.h.ShowOverlay(s)
	e.frame()
	e.frame()
	field := func() FieldInfo {
		for _, f := range s.fields() {
			if f.BlockID == "check" {
				return f
			}
		}
		t.Fatalf("no check field (%+v)", s.fields())
		return FieldInfo{}
	}
	f := field()
	if f.Type != "rich_text_input" || f.R.Empty() {
		t.Fatalf("check field %+v", f)
	}
	e.click(center(f.R))
	e.frame()
	e.frame()
	if !field().Focused {
		t.Fatalf("a click in the box (%v) didn't focus the field", f.R)
	}
	typeIn(e, "mango")
	e.keyMods("A", key.ModShortcut)
	e.keyMods("X", key.ModShortcut|key.ModShift)
	e.frame()
	e.shot("rich-text-input-struck-" + mode) // the formatting bar, the struck word (BK_ELEMENTS_SHOTS_DIR)

	n := len(submittedState(e))
	s.doSubmit()
	e.until("views.submit", 10*time.Second, func() bool { return len(submittedState(e)) > n })
	raw := submittedState(e)[n]
	var st struct {
		Values map[string]map[string]struct {
			Type  string `json:"type"`
			Value *struct {
				Type     string `json:"type"`
				Elements []struct {
					Type     string `json:"type"`
					Elements []struct {
						Type  string          `json:"type"`
						Text  string          `json:"text"`
						Style map[string]bool `json:"style"`
					} `json:"elements"`
				} `json:"elements"`
			} `json:"rich_text_value"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		// the state param is {"values": …} or the values map itself
		var vals map[string]map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &vals) != nil {
			t.Fatalf("state %s: %v", raw, err)
		}
	}
	if st.Values == nil {
		_ = json.Unmarshal([]byte(`{"values":`+raw+`}`), &st)
	}
	val := st.Values["check"]["v"]
	if val.Type != "rich_text_input" || val.Value == nil || val.Value.Type != "rich_text" {
		t.Fatalf("state %s: no rich_text_value for check.v", raw)
	}
	var struck []string
	for _, sec := range val.Value.Elements {
		for _, el := range sec.Elements {
			if el.Type == "text" && el.Style["strike"] {
				struck = append(struck, el.Text)
			}
		}
	}
	if strings.Join(struck, "") != "mango" {
		t.Errorf("state %s: struck text %q, want mango", raw, struck)
	}
	e.closeOverlays()
}
