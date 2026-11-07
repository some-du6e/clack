package jsrt

import (
	"bytes"
	"os"
	"testing"
)

// TestAssetPackFresh: assets.pack must match the files it is built from.
func TestAssetPackFresh(t *testing.T) {
	got, err := unpackAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range PackFiles {
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got[name], want) {
			t.Errorf("assets.pack is stale for %s: run go generate ./internal/jsrt and commit assets.pack", name)
		}
	}
}
