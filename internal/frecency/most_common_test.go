package frecency

import (
	"fmt"
	"testing"
	"time"
)

// MostCommonWithPrefix is getMostCommonWithPrefixFromCache: records with
// the prefix summed by id (an id's first visit adds the record under its
// own id, which counts again when the traversal gets there), never a
// _reduced record or another prefix's, highest first, at most n.
func TestMostCommonWithPrefix(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	v := now.Add(-time.Minute).UnixMilli()
	rec := func(id string, n int, reduced bool) string {
		r := fmt.Sprintf(`{"id":%q,"count":%d,"visits":[%d]`, id, n, v)
		if reduced {
			r += `,"_reduced":true`
		}
		return r + "}"
	}
	c := Parse([]byte(`{"MAa":` + rec("MAa", 2, false) + `,"MAb":` + rec("MAb", 5, false) +
		`,"tr":[` + rec("MAa", 1, false) + `,` + rec("Cother", 9, false) + `],"MAc":` + rec("MAc", 7, true) +
		`,"E1f44d":` + rec("E1f44d", 9, false) + `}`))
	got := c.MostCommonWithPrefix("MA", 10, now)
	if len(got) != 2 || got[0].ID != "MAb" || got[1].ID != "MAa" {
		t.Fatalf("got %+v, want MAb then MAa (no _reduced MAc, no other prefix)", got)
	}
	w := Score(&Record{ID: "x", Count: 1, Visits: []int64{v}}, now)
	// MAb: its record twice (the first visit's own record, then itself) =
	// 10w; MAa: twice 2w, then the query's 1w = 5w
	if got[0].Score != 10*w || got[1].Score != 5*w {
		t.Errorf("scores %v %v, want %v %v", got[0].Score, got[1].Score, 10*w, 5*w)
	}
	if one := c.MostCommonWithPrefix("MA", 1, now); len(one) != 1 || one[0].ID != "MAb" {
		t.Errorf("n=1: %+v", one)
	}
}
