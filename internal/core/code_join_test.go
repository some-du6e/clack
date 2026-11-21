package core

import (
	"context"
	"testing"
)

// A private code channel's Join (boot-shared kR8w joinCodeChannel): one
// client.codeChannels.join {channel_id, origin_channel_id} and nothing
// else; the answer's channel stored as it came and announced (a conv
// event); a refused join is the caller's error and stores nothing; a Join
// right after a refusal is sent again (the viewer's click, past the
// failure backoff); a channel the viewer is already in asks nothing.
func TestJoinCodeChannel(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	if err := joinCodeChannel(a, ctx, "", "C1"); err == nil {
		t.Error("a join without the channel reported no error")
	}
	if err := joinCodeChannel(a, ctx, "GCODE1", ""); err == nil {
		t.Error("a join without the origin channel reported no error")
	}
	if n := f.count("client.codeChannels.join"); n != 0 {
		t.Fatalf("joins without their params sent %d requests, want none", n)
	}

	f.reply["client.codeChannels.join"] = `{"ok":false,"error":"not_allowed"}`
	if err := joinCodeChannel(a, ctx, "GCODE1", "C1"); err == nil {
		t.Fatal("a refused join reported no error")
	}
	if _, ok := a.Conv("GCODE1"); ok {
		t.Error("a refused join stored the channel")
	}

	evs, stop := a.Subscribe()
	defer stop()
	f.reply["client.codeChannels.join"] = `{"ok":true,"channel":{"id":"GCODE1","name":"hotfix-payments","is_channel":true,"is_private":true,"is_member":true,` +
		`"properties":{"record_channel":{"record_id":"AC:1:2:GCODE1","record_type":"agent_channel"},"agent_session":{"status":"active"}}}}`
	if err := joinCodeChannel(a, ctx, "GCODE1", "C1"); err != nil {
		t.Fatalf("Join again right after the refusal: %v, want it sent and joined", err)
	}
	if n := f.count("client.codeChannels.join"); n != 2 {
		t.Errorf("client.codeChannels.join sent %d times, want 2 (the refused one, then the retry)", n)
	}
	v := f.last("client.codeChannels.join")
	if v.Get("channel_id") != "GCODE1" || v.Get("origin_channel_id") != "C1" {
		t.Errorf("join args %v, want channel_id GCODE1, origin_channel_id C1", v)
	}
	for _, k := range []string{"channel", "reason", "_x_reason"} {
		if v.Has(k) {
			t.Errorf("join sent %s=%q; joinCodeChannel's args are channel_id and origin_channel_id", k, v.Get(k))
		}
	}
	ev := next(t, evs, "conv")
	if ev.Channel != "GCODE1" || ev.Conv == nil || !ev.Conv.Member {
		t.Errorf("conv event %+v, want GCODE1 with the viewer in it", ev)
	}
	cv, ok := a.Conv("GCODE1")
	if !ok || !cv.Member || cv.Kind != "private" || cv.Code == nil || cv.Name != "hotfix-payments" {
		t.Errorf("stored %+v (ok %v), want the private code channel the answer named, the viewer in it", cv, ok)
	}

	if err := joinCodeChannel(a, ctx, "GCODE1", "C1"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("client.codeChannels.join"); n != 2 {
		t.Errorf("a Join of a channel the viewer is in sent a request (%d in all), want none", n)
	}
}
