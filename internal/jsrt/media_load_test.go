package jsrt

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestMediaElementSecondPlayDuringLoad: a second play() while the
// element's first load is still opening (two notifications of one sound
// close together, the first time the runtime plays it) doesn't start a
// load of its own, as Chromium doesn't fetch the resource again; both
// play() calls settle with that load. And a load the element left behind
// closes only its own player: with the host's opens released newest
// first, the element keeps a live player, and a third play() plays it.
// (Before, the second play opened a second player under the same element
// id; the first load's late continuation closed by id, dropping the live
// one, and that sound played nothing again until a restart.)
func TestMediaElementSecondPlayDuringLoad(t *testing.T) {
	host := &fakeMedia{gate: make(chan chan struct{}, 4)}
	r := newTest(t, func(o *Options) { withMedia(o, host) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	evalString(t, r, `
globalThis.twoPlays = () => {
  const a = globalThis.el = new Audio('https://a.slack-edge.com/bv1-13-br/knock_brush-ac2b6e8.mp3');
  globalThis.settled = [];
  a.play().then(() => settled.push('first'), (e) => settled.push('first: ' + e.name));
  a.play().then(() => settled.push('second'), (e) => settled.push('second: ' + e.name));
  return true;
};
globalThis.third = async () => {
  const r = await Promise.race([el.play().then(() => 'played', (e) => e.name), new Promise((ok) => setTimeout(() => ok('timeout'), 3000))]);
  return { third: r, settled, paused: el.paused };
};
0`)
	if _, err := r.Call(ctx, "twoPlays"); err != nil {
		t.Fatal(err)
	}
	// the opens the two plays started (one, now; two before)
	var opens []chan struct{}
	for wait := time.After(2 * time.Second); ; {
		select {
		case c := <-host.gate:
			opens = append(opens, c)
			continue
		case <-time.After(300 * time.Millisecond):
		case <-wait:
		}
		break
	}
	for i := len(opens) - 1; i >= 0; i-- { // newest first: the late one is the older load
		close(opens[i])
		time.Sleep(150 * time.Millisecond)
	}
	got, err := r.Call(ctx, "third")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Third   string
		Settled []string
		Paused  bool
	}
	if err := json.Unmarshal(got, &res); err != nil {
		t.Fatalf("%s: %v", got, err)
	}
	host.mu.Lock()
	opened, players := len(host.opened), append([]*fakePlayer(nil), host.players...)
	host.mu.Unlock()
	var live []*fakePlayer
	for _, p := range players {
		p.mu.Lock()
		if !p.closed {
			live = append(live, p)
		}
		p.mu.Unlock()
	}
	if opened != 1 {
		t.Errorf("%d opens for two plays of one src during its load, want 1", opened)
	}
	if len(live) != 1 {
		t.Fatalf("%d live players after the loads settled (of %d opened), want the element's one", len(live), opened)
	}
	live[0].mu.Lock()
	plays := live[0].plays
	live[0].mu.Unlock()
	if res.Third != "played" || plays < 2 || res.Paused {
		t.Errorf("the third play(): %q, the player told to play %d times, paused %v; want it playing again", res.Third, plays, res.Paused)
	}
	if len(res.Settled) != 2 || res.Settled[0] != "first" || res.Settled[1] != "second" {
		t.Errorf("the first two play() calls settled %v, want both resolved, in order", res.Settled)
	}
}
