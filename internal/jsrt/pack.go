package jsrt

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"fmt"
	"io"
	"strconv"
)

// The engine (jsrt.wasm, 1 MB) and the boot scripts (js/, vendor/; 1.2 MB)
// are embedded as one deflate-compressed pack: 2.2 MB of the binary becomes
// ~0.6 MB, and nothing is decompressed (or held in memory) unless a plugin
// runtime starts. The source files stay in the repo for editing; after
// changing any of them (or rebuilding jsrt.wasm) run
//
//	go generate ./internal/jsrt
//
// TestAssetPackFresh fails while the pack is stale.
//
//go:generate go run ./genpack
//go:embed assets.pack
var assetPack []byte

// PackFiles are the files in assets.pack, in pack order.
var PackFiles = []string{
	"jsrt.wasm",
	"js/env.js", "js/web.js", "js/intl.js", "js/dom.js", "js/react-host.js",
	"vendor/react.production.js", "vendor/react-jsx-runtime.production.js", "vendor/scheduler.production.js",
	"vendor/react-reconciler.production.js", "vendor/react-reconciler-constants.production.js",
}

// unpackAssets decompresses the pack (~10 ms). Callers keep only what they need.
func unpackAssets() (map[string][]byte, error) { return ReadPack(assetPack) }

// ReadPack parses a pack: for each file, "<name>\x00<length>\n<bytes>".
func ReadPack(pack []byte) (map[string][]byte, error) {
	raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(pack)))
	if err != nil {
		return nil, fmt.Errorf("jsrt: assets.pack: %w", err)
	}
	out := map[string][]byte{}
	for len(raw) > 0 {
		i := bytes.IndexByte(raw, 0)
		j := bytes.IndexByte(raw, '\n')
		if i < 0 || j < i {
			return nil, fmt.Errorf("jsrt: assets.pack: bad header")
		}
		n, err := strconv.Atoi(string(raw[i+1 : j]))
		if err != nil || j+1+n > len(raw) {
			return nil, fmt.Errorf("jsrt: assets.pack: bad length")
		}
		out[string(raw[:i])] = raw[j+1 : j+1+n : j+1+n]
		raw = raw[j+1+n:]
	}
	return out, nil
}

// WritePack builds a pack from files (name -> contents) in PackFiles order.
// Deterministic: the same inputs give the same bytes.
func WritePack(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	for _, name := range PackFiles {
		b, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("jsrt: pack: missing %s", name)
		}
		fmt.Fprintf(w, "%s\x00%d\n", name, len(b))
		w.Write(b)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
