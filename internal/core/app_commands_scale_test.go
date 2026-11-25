package core

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// hcCommands is Hack Club's client.appCommands size on 2026-11-24: 8,870
// commands (8,826 apps', 24 core, 13 untyped, 5 service, 2 custom) in
// 5 MB, every one with its teams.
const hcCommands = 8870

// syntheticAppCommands is a client.appCommands answer of n commands
// shaped like Hack Club's (synthetic names: apps of a few commands each,
// a description, usage, four icons, the team; the core commands with
// their aliases), about 570 bytes a command as the live answer.
func syntheticAppCommands(n int) string {
	builtins := []map[string]any{}
	for _, c := range []string{"/active", "/apps", "/away", "/dnd", "/feed", "/me", "/mute", "/remind", "/shrug", "/status", "/who",
		"/archive", "/feedback", "/invite", "/leave", "/msg", "/open", "/remove", "/topic"} {
		builtins = append(builtins, map[string]any{"name": c, "canonical_name": c, "desc": "A core command", "usage": "", "type": "core",
			"teams": []string{"T1"}, "name_normalized": strings.TrimPrefix(c, "/"), "is_workspace_agnostic": false})
	}
	for alias, of := range map[string]string{"/close": "/leave", "/part": "/leave", "/dm": "/msg", "/join": "/open", "/kick": "/remove"} {
		builtins = append(builtins, map[string]any{"name": alias, "canonical_name": alias, "alias_of": of, "alias_of_normalized": strings.TrimPrefix(of, "/"),
			"type": "core", "teams": []string{"T1"}})
	}
	cmds := make([]map[string]any, 0, n)
	cmds = append(cmds, builtins...)
	for i := 0; len(cmds) < n; i++ {
		app := fmt.Sprintf("A0%09d", i/3)
		name := fmt.Sprintf("/syn%dapp-%s", i/3, []string{"start", "status", "help"}[i%3])
		icon := func(px int) string {
			return fmt.Sprintf("https://avatars.slack-edge.com/2026-11-24/%010d_%08x_%d.png", i/3, i*2654435761%(1<<32), px)
		}
		cmds = append(cmds, map[string]any{"name": name, "canonical_name": name, "name_normalized": strings.TrimPrefix(name, "/"),
			"desc": fmt.Sprintf("Synthetic command %d of a test app that does one small thing", i), "usage": "[what] [when]",
			"type": "app", "app": app, "app_name": fmt.Sprintf("Synthetic App %d", i/3), "teams": []string{"T1"},
			"is_workspace_agnostic": false,
			"icons":                 map[string]any{"image_32": icon(32), "image_36": icon(36), "image_48": icon(48), "image_72": icon(72)}})
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "cache_ts": "1764000000", "app_actions": []any{}, "commands": cmds})
	return string(b)
}

func heapHeld() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// TestAppCommandsScale measures the slash command store at Hack Club's
// size: the answer's bytes, the load (the call over loopback, its decode,
// bulkAddServerCommands), the heap the store holds afterwards, a reload
// once the 30 minutes are up, and a composer's read of a fresh store
// (SlashCommands, which every "/" menu takes once). It logs the numbers
// and fails only far past them.
func TestAppCommandsScale(t *testing.T) {
	a, f := testApp(t)
	body := syntheticAppCommands(hcCommands)
	f.reply["client.appCommands"] = body
	ctx := context.Background()
	before := heapHeld()
	t0 := time.Now()
	cs, err := a.Commands(ctx, "")
	load := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	listed := len(cs)
	cs = nil
	held := int64(heapHeld()) - int64(before)
	a.mu.Lock()
	stored := len(a.cmdCache)
	a.mu.Unlock()
	if stored < hcCommands-30 {
		t.Fatalf("%d commands stored", stored)
	}
	t.Logf("client.appCommands: %d commands, %.2f MB answer; load (call, decode, ingest) %v; %d stored, %d listed; heap held %.2f MB",
		hcCommands, float64(len(body))/1e6, load, stored, listed, float64(held)/1e6)
	a.mu.Lock()
	a.cmdAt = time.Now().Add(-time.Hour) // the 30 minutes are up
	a.mu.Unlock()
	t1 := time.Now()
	if _, err := a.Commands(ctx, ""); err != nil {
		t.Fatal(err)
	}
	reload := time.Since(t1)
	t2 := time.Now()
	menu, _ := a.SlashCommands(ctx, "C1")
	cached := time.Since(t2)
	t.Logf("reload after the cache expires %v; a composer's menu list from the fresh store %v (%d commands)", reload, cached, len(menu))
	if load > 30*time.Second || reload > 30*time.Second || cached > 5*time.Second {
		t.Errorf("load %v, reload %v, cached read %v", load, reload, cached)
	}
}
