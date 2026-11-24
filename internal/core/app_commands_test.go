package core

import (
	"context"
	"testing"
)

// Commands loads client.appCommands through the ingest: the server's
// /kick and /remove are gone, so /kick is the client's /remove.
func TestCommandsFromAppCommands(t *testing.T) {
	a, f := testApp(t)
	f.reply["client.appCommands"] = `{"ok":true,"cache_ts":"1","app_actions":[],"commands":[` +
		`{"name":"/remove","canonical_name":"/remove","type":"core","teams":["T1"]},` +
		`{"name":"/kick","canonical_name":"/kick","alias_of":"/remove","type":"core","teams":["T1"]},` +
		`{"name":"/away","canonical_name":"/away","type":"core","teams":["T1"]}]}`
	cs, err := a.Commands(context.Background(), "")
	if err != nil || len(cs) != 1 || cs[0].Name != "/away" {
		t.Fatalf("commands: %+v %v", cs, err)
	}
	if f.last("client.appCommands") == nil {
		t.Error("not read from client.appCommands")
	}
	if got := a.LocalCommand("/kick"); got != "/remove" {
		t.Errorf("/kick runs as %q", got)
	}
}
