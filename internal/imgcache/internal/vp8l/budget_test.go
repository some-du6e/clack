package vp8l

// Clack: the sizes budget.go charges must be at least what Go allocates
// for each type, or the budget would under-count. These check them against
// unsafe.Sizeof so a change to a type (or a platform) that grew them can't
// slip past. Nothing here comes from Slack.

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"unsafe"
)

// readerOnly hides a reader's ReadByte, so decodeHeader sees a plain
// io.Reader (not an io.ByteReader) and must wrap it in a bufio.Reader —
// the path production takes, where the VP8L chunk arrives through a riff
// chunk reader that has only Read. bytes.Reader has ReadByte, so passing
// one directly would skip this path.
type readerOnly struct{ r io.Reader }

func (x readerOnly) Read(p []byte) (int, error) { return x.r.Read(p) }

func TestChargedSizes(t *testing.T) {
	if got := unsafe.Sizeof(hNode{}); int(got) != hNodeBytes {
		t.Errorf("hNode is %d bytes, hNodeBytes charges %d", got, hNodeBytes)
	}
	// hTreeBytes = the slice header charged for h.nodes plus the look-up
	// table (the nodes it points at are charged per node by newNodes).
	wantTree := int(unsafe.Sizeof(hTree{}.lut)) + int(unsafe.Sizeof([]hNode(nil)))
	if wantTree != hTreeBytes {
		t.Errorf("an hTree's lut+header is %d bytes, hTreeBytes charges %d", wantTree, hTreeBytes)
	}
	if int(unsafe.Sizeof(hGroup{})) > hGroupBytes {
		t.Errorf("hGroup is %d bytes, hGroupBytes charges only %d", unsafe.Sizeof(hGroup{}), hGroupBytes)
	}
	if hGroupBytes != nHuff*hTreeBytes {
		t.Errorf("hGroupBytes %d != %d trees of %d", hGroupBytes, nHuff, hTreeBytes)
	}
}

// TestBudgetCharge covers the allowance arithmetic: a nil budget allows
// nothing, Charge takes exactly what fits, and an over-charge takes
// nothing so a later smaller charge can still succeed.
func TestBudgetCharge(t *testing.T) {
	if err := (*Budget)(nil).Charge(1); err == nil {
		t.Error("a nil budget allowed a charge")
	}
	b := NewBudget(100)
	if err := b.Charge(60); err != nil {
		t.Fatalf("charging 60 of 100: %v", err)
	}
	if err := b.Charge(50); err == nil {
		t.Error("charging 50 more of 100 succeeded")
	}
	if b.Used() != 60 {
		t.Errorf("a refused charge changed Used to %d, want 60", b.Used())
	}
	if err := b.Charge(40); err != nil {
		t.Errorf("charging the last 40: %v", err)
	}
	if err := b.Charge(0); err != nil {
		t.Errorf("charging 0 at the limit: %v", err)
	}
	if err := b.Charge(-1); err == nil {
		t.Error("a negative charge was allowed")
	}
}

// TestDecodeConfigBudget: DecodeConfig reads the header under its own small
// budget, which must cover the bufio.Reader it wraps a non-io.ByteReader
// in — the production path, where the VP8L chunk comes through a riff chunk
// reader that has only Read. The reader here hides ReadByte so that path is
// taken (a bytes.Reader would skip it, which is why the old test, passing
// one, still passed when the budget dropped readerBytes).
func TestDecodeConfigBudget(t *testing.T) {
	stream := solidVP8L(4, 4, 0x11, 0x22, 0x33) // header + one group, no meta
	cfg, err := DecodeConfig(readerOnly{bytes.NewReader(stream)})
	if err != nil {
		t.Fatalf("DecodeConfig on a non-ByteReader: %v", err)
	}
	if cfg.Width != 4 || cfg.Height != 4 {
		t.Errorf("DecodeConfig %dx%d, want 4x4", cfg.Width, cfg.Height)
	}
	// Pin the budget size for the non-ByteReader path: fixedBytes alone can't
	// cover the bufio.Reader (readerBytes), so decodeHeader must be given
	// fixedBytes+readerBytes, which is what DecodeConfig allots.
	if _, _, _, err := decodeHeader(readerOnly{bytes.NewReader(stream)}, NewBudget(fixedBytes)); !errors.Is(err, ErrBudget) {
		t.Errorf("decodeHeader on a non-ByteReader under a fixedBytes-only budget: %v, want ErrBudget", err)
	}
	if _, _, _, err := decodeHeader(readerOnly{bytes.NewReader(stream)}, NewBudget(fixedBytes+readerBytes)); err != nil {
		t.Errorf("decodeHeader on a non-ByteReader under fixedBytes+readerBytes: %v, want success", err)
	}
	// a bytes.Reader is an io.ByteReader, so no bufio wrap and fixedBytes alone suffices
	if _, _, _, err := decodeHeader(bytes.NewReader(stream), NewBudget(fixedBytes)); err != nil {
		t.Errorf("decodeHeader on an io.ByteReader under fixedBytes: %v, want success", err)
	}
}
