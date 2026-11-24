package emoji

import "testing"

// The plugins' upsertEmoji and the server's list fill one store, as
// Slack's custom emoji store has them: whichever wrote a name last keeps
// it, a list reload keeps the upserts it doesn't name, a removal takes
// both, an upserted alias resolves through the server's emoji; and the
// server's part (what the disk cache keeps) never holds an upsert.
func TestCustomUpsertLikeSlacksStore(t *testing.T) {
	var c Custom
	c.Set(map[string]string{"party": "https://e/party.png", "old": "https://e/old.png"})
	sv := c.ServerVersion()
	v := c.Version()
	c.Upsert(map[string]string{"hm_secret": "blob:clack/1", "old": "blob:clack/2"})
	if c.Version() == v {
		t.Error("an upsert left the version: what draws emoji wouldn't see it")
	}
	if c.ServerVersion() != sv {
		t.Error("an upsert changed the server's part's version: the disk cache would save it again")
	}
	resolve := func(name string) string {
		u, _, ok := c.Resolve(name, "apple")
		if !ok {
			return ""
		}
		return u
	}
	if got := resolve("hm_secret"); got != "blob:clack/1" {
		t.Errorf("hm_secret resolves to %q", got)
	}
	if got := resolve("old"); got != "blob:clack/2" {
		t.Errorf("old resolves to %q, want the upsert, written last", got)
	}
	if s := c.ServerSnapshot(); s["hm_secret"] != "" || s["old"] != "https://e/old.png" {
		t.Errorf("the server's part is %v", s)
	}
	if n := c.Len(); n != 3 || len(c.Snapshot()) != 3 {
		t.Errorf("Len %d, Snapshot %v: want the 3 names", n, c.Snapshot())
	}

	// a reload: the server wrote "old" again, hm_secret it doesn't name
	c.Set(map[string]string{"party": "https://e/party2.png", "old": "https://e/old3.png"})
	if got := resolve("hm_secret"); got != "blob:clack/1" {
		t.Errorf("after a list reload hm_secret resolves to %q, want the upsert kept", got)
	}
	if got := resolve("old"); got != "https://e/old3.png" {
		t.Errorf("after a list reload old resolves to %q, want the server's, written last", got)
	}
	// emoji_changed adds a name a plugin upserted: the server's
	c.Upsert(map[string]string{"hm2": "blob:clack/3"})
	c.Put("hm2", "https://e/hm2.png")
	if got := resolve("hm2"); got != "https://e/hm2.png" {
		t.Errorf("hm2 resolves to %q, want the server's add", got)
	}
	c.Set(map[string]string{"party": "https://e/party2.png"})
	if got := resolve("hm2"); got != "" {
		t.Errorf("hm2 resolves to %q after the list dropped it: the upsert came back", got)
	}
	// emoji_changed removes a name: gone, upsert and all
	c.Upsert(map[string]string{"gone": "blob:clack/4"})
	c.Remove("gone")
	if got := resolve("gone"); got != "" {
		t.Errorf("a removed name resolves to %q", got)
	}
	// an upserted alias resolves through the server's emoji
	c.Upsert(map[string]string{"yay": "alias:party"})
	if got := resolve("yay"); got != "https://e/party2.png" {
		t.Errorf("the upserted alias resolves to %q", got)
	}
	if chain := c.AliasChain("yay"); len(chain) != 1 || chain[0] != "party" {
		t.Errorf("its alias chain is %v", chain)
	}
	if s := c.ServerSnapshot(); len(s) != 1 || s["party"] == "" {
		t.Errorf("the server's part is %v, want party alone", s)
	}
}

// An upsert before the server's list has loaded stays out of the server's
// part, and the list installs under it.
func TestCustomUpsertBeforeTheList(t *testing.T) {
	var c Custom
	c.Upsert(map[string]string{"hm_secret": "blob:clack/1"})
	if s := c.ServerSnapshot(); len(s) != 0 {
		t.Errorf("before the list the server's part is %v, want empty", s)
	}
	c.Set(nil)
	if s := c.ServerSnapshot(); len(s) != 0 {
		t.Errorf("after an empty list the server's part is %v, want empty", s)
	}
	c.Set(map[string]string{"party": "https://e/party.png"})
	if u, _, _ := c.Resolve("hm_secret", "apple"); u != "blob:clack/1" {
		t.Errorf("hm_secret resolves to %q after the list", u)
	}
	if s := c.ServerSnapshot(); len(s) != 1 || s["party"] == "" {
		t.Errorf("the server's part is %v, want party alone", s)
	}
}
