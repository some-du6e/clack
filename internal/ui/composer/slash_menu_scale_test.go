package composer

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
)

// syntheticMenu is the "/" menu's list at a store of n server commands:
// Slack's 15 client commands, then n app commands shaped like Hack
// Club's (synthetic: three commands an app, a description, an app name).
func syntheticMenu(n int) []core.Command {
	var out []core.Command
	for _, c := range []string{"/archive", "/collapse", "/expand", "/invite", "/invite_people", "/leave", "/msg", "/open", "/remove",
		"/rename", "/search", "/shortcuts", "/topic", "/feedback", "/gif"} {
		out = append(out, core.Command{Name: c, Desc: "A client command", Type: "client"})
	}
	for i := 0; i < n; i++ {
		out = append(out, core.Command{Name: fmt.Sprintf("/syn%dapp-%s", i/3, []string{"start", "status", "help"}[i%3]),
			Desc: fmt.Sprintf("Synthetic command %d of a test app that does one small thing", i), Usage: "[what] [when]",
			Type: "app", AppID: fmt.Sprintf("A0%09d", i/3), AppName: fmt.Sprintf("Synthetic App %d", i/3)})
	}
	return out
}

// rankCost is rankCommands' median time over three runs for q.
func rankCost(q string, cmds []core.Command) (time.Duration, int) {
	var ds []time.Duration
	n := 0
	for range 3 {
		t0 := time.Now()
		n = len(rankCommands(q, cmds, rankCtx{}))
		ds = append(ds, time.Since(t0))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[1], n
}

// TestSlashMenuScale measures the "/" menu at Hack Club's size (8,870
// server commands, 2026-11-24): rankCommands' cost for each keystroke of
// a typed command, and at acSyncCommands, the longest list ranked on the
// UI goroutine. A longer list ranks off it: the Update that sees the new
// list returns with the rows it had (none here), the ranked rows land
// after, and a Tab before they do takes the live query's rows. It logs
// the numbers and fails only far past them.
func TestSlashMenuScale(t *testing.T) {
	big := syntheticMenu(hcMenuCommands)
	for _, q := range []string{"", "s", "st", "sta", "stat", "status", "syn12app", "zzq"} {
		d, rows := rankCost(q, big)
		t.Logf("rank %d commands for %q: %v (%d rows)", len(big), q, d, rows)
		if d > 2*time.Second {
			t.Errorf("rank %q: %v", q, d)
		}
	}
	small := syntheticMenu(acSyncCommands - 15)
	for _, q := range []string{"", "s", "sta"} {
		d, _ := rankCost(q, small)
		t.Logf("rank %d commands (acSyncCommands, on the UI goroutine) for %q: %v", len(small), q, d)
	}

	ac := newAutocomplete(nil, "C1")
	d := acDocOf("/sta")
	ac.Update(d, d.Len())
	ac.mu.Lock()
	ac.commands, ac.cmdVer = big, ac.cmdVer+1
	ac.gen++
	ac.mu.Unlock()
	t0 := time.Now()
	ac.Update(d, d.Len()) // compute holds mu: the rank can't land before it returns
	upd := time.Since(t0)
	if len(ac.items) != 0 {
		t.Fatalf("the Update that saw %d commands ranked them on the UI goroutine (%d rows)", len(big), len(ac.items))
	}
	t.Logf("Update with %d commands: %v (the rank runs on a goroutine)", len(big), upd)
	end := time.Now().Add(10 * time.Second)
	for len(ac.items) == 0 && time.Now().Before(end) {
		time.Sleep(5 * time.Millisecond)
		ac.Update(d, d.Len())
	}
	if len(ac.items) == 0 {
		t.Fatal("the ranked rows never landed")
	}
	t.Logf("rows landed %v after the list", time.Since(t0))
	// a Tab on a newer query before its rank lands takes that query's rows
	d2 := acDocOf("/syn12app-help")
	ac.Live = func() (*Doc, int) { return d2, d2.Len() }
	var got []string
	ac.OnAccept = func(it acItem, start, end int) { got = append(got, it.ID) }
	if !ac.Key(key.NameTab, 0) || len(got) != 1 || got[0] != "syn12app-help" {
		t.Errorf("Tab took %v, want syn12app-help", got)
	}
}

// hcMenuCommands is Hack Club's server command count (client.appCommands,
// 2026-11-24).
const hcMenuCommands = 8870
