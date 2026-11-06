package emoji

import (
	"sync"
	"testing"
)

func TestCustomCopyOnWrite(t *testing.T) {
	var c Custom
	c.Set(map[string]string{"a": "https://e.example/a.png", "b": "alias:a"})
	snap := c.Snapshot()
	c.Put("c", "https://e.example/c.png")
	c.Remove("a")
	if len(snap) != 2 || snap["a"] == "" {
		t.Errorf("snapshot mutated: %v", snap)
	}
	if c.Len() != 2 {
		t.Errorf("len = %d", c.Len())
	}
	// Concurrent iteration of snapshots while writing must not race/crash.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				for range c.Snapshot() {
				}
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.Put("x", "y")
				c.Remove("x")
			}
		}(i)
	}
	wg.Wait()
	if u, _, ok := c.Resolve("b", "apple"); !ok || u != "" && u == "alias:a" {
		t.Errorf("alias resolve: %q %v", u, ok)
	}
}

func TestStandard(t *testing.T) {
	img, text, ok := Standard("+1::skin-tone-3")
	if !ok || img != "1f44d-1f3fc" || text == "" {
		t.Errorf("Standard = %q %q %v", img, text, ok)
	}
	if _, _, ok := Standard("definitely-not-an-emoji"); ok {
		t.Error("unknown emoji resolved")
	}
}
