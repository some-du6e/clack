package quip

import (
	"errors"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
)

type applyIn struct {
	Base string `json:"base"`
	Runs []struct {
		Text      string `json:"text"`
		Break     bool   `json:"break"`
		Control   string `json:"control"`
		Bold      bool   `json:"bold"`
		Italic    bool   `json:"italic"`
		Underline bool   `json:"underline"`
		Strike    bool   `json:"strike"`
		Code      bool   `json:"code"`
		Href      string `json:"href"`
	} `json:"runs"`
	RTML string `json:"rtml"`
}

// TestQuipApplyRunsLikeSlack writes each case's edit onto its base and
// holds the RTML to Slack's save writer: CITi's DI over the DOM the RTML
// builds gives the same bytes back, so Slack's editor would save it as
// Clack wrote it. A refused edit compares as "(refused)".
func TestQuipApplyRunsLikeSlack(t *testing.T) {
	s := paritytest.Load[applyIn, string](t, "testdata/parity/quip-apply-runs.json")
	s.Run(t, paritytest.Check[applyIn, string]{Clack: func(t *testing.T, c applyIn) string {
		var runs []Run
		for _, r := range c.Runs {
			runs = append(runs, Run{Text: r.Text, Break: r.Break, Control: r.Control, Bold: r.Bold, Italic: r.Italic,
				Underline: r.Underline, Strike: r.Strike, Code: r.Code, Href: r.Href})
		}
		out, err := ApplyRuns(c.Base, runs)
		if errors.Is(err, ErrUnsupported) {
			return "(refused)"
		}
		if err != nil {
			t.Fatalf("ApplyRuns: %v", err)
		}
		if out != c.RTML {
			t.Errorf("wrote %q, the case says %q", out, c.RTML)
		}
		// no mark is ever empty (getCompactRtml would lose the text after it)
		if _, marks, ok := ParseRTML(out); !ok {
			t.Errorf("Slack can't parse %q", out)
		} else {
			for _, m := range marks {
				if m.Start == m.End {
					t.Errorf("an empty <%s> in %q", m.Name, out)
				}
			}
		}
		return out
	}})
}

// RTMLPlain and RTMLControls read a section as an editor shows it.
func TestQuipRTMLPlain(t *testing.T) {
	rtml := `Ask <control id="temp:C:X1"></control> &amp; <b>go</b><br>now`
	if p, ok := RTMLPlain(rtml); !ok || p != "Ask \ufffc & go\nnow" {
		t.Errorf("RTMLPlain = %q, %v", p, ok)
	}
	if ids, ok := RTMLControls(rtml); !ok || len(ids) != 1 || ids[0] != "temp:C:X1" {
		t.Errorf("RTMLControls = %v, %v", ids, ok)
	}
}
