package acrank

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// Rank matches a long list on several cores (parallelMatch): the results
// are the one loop's, row for row.
func TestRankParallelMatchesSequential(t *testing.T) {
	var cands []Cand
	for i := 0; i < 3*parallelMatch/2; i++ {
		name := fmt.Sprintf("/syn%dapp-%s", i/3, []string{"start", "status", "help"}[i%3])
		cands = append(cands, Cand{Kind: Command, ID: name, Name: name,
			Desc: fmt.Sprintf("Synthetic command %d of a test app", i), AppName: fmt.Sprintf("Synthetic App %d", i/3)})
	}
	o := Options{Source: SourceSlash, Now: time.Unix(1700000000, 0)}
	for _, q := range []string{"", "s", "sta", "status", "syn12app", "zzq"} {
		prev := runtime.GOMAXPROCS(1)
		seq := Rank(q, cands, o)
		runtime.GOMAXPROCS(max(prev, 4))
		par := Rank(q, cands, o)
		runtime.GOMAXPROCS(prev)
		if !reflect.DeepEqual(seq, par) {
			t.Errorf("%q: on one core %v, on several %v", q, seq, par)
		}
	}
}
