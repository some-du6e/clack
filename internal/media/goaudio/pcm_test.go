package goaudio

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media/mediatest"
)

// bitWriter writes fields most significant bit first, as MPEG audio's
// headers and side info are laid out.
type bitWriter struct {
	b []byte
	n int // bits written
}

func (w *bitWriter) put(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 != 0 {
			w.b[len(w.b)-1] |= 0x80 >> uint(w.n%8)
		}
		w.n++
	}
}

// mpeg2MixedBlocks is n MPEG-2 Layer III frames (ISO/IEC 13818-3: 22.05
// kHz, 64 kbps, mono, no CRC, 208 bytes each) whose one granule switches
// to mixed short blocks (window_switching_flag, block_type 2,
// mixed_block_flag) with scalefac_compress 0. go-mp3 v0.3.4's
// getScaleFactorsMpeg2 makes 6+9+9+9 zero scale factors and 5 more for
// that granule, 38, and then reads them as 13 bands of 3 windows: index
// 38 is past them. A file the decoder fails on, as many a cut or corrupt
// MP3 could be.
func mpeg2MixedBlocks(n int) []byte {
	var w bitWriter
	// the frame header
	w.put(0x7FF, 11) // syncword
	w.put(2, 2)      // ID and the version bit: MPEG-2
	w.put(1, 2)      // layer: III
	w.put(1, 1)      // protection_bit: no CRC
	w.put(8, 4)      // bitrate_index: 64 kbps
	w.put(0, 2)      // sampling_frequency: 22.05 kHz
	w.put(0, 1)      // padding_bit
	w.put(0, 1)      // private_bit
	w.put(3, 2)      // mode: single channel
	w.put(0, 2)      // mode_extension
	w.put(0, 1)      // copyright
	w.put(0, 1)      // original/home
	w.put(0, 2)      // emphasis: none
	// the side info: MPEG-2 mono, one granule (9 bytes)
	w.put(0, 8)  // main_data_begin
	w.put(0, 1)  // private_bits
	w.put(0, 12) // part2_3_length
	w.put(0, 9)  // big_values
	w.put(0, 8)  // global_gain
	w.put(0, 9)  // scalefac_compress
	w.put(1, 1)  // window_switching_flag
	w.put(2, 2)  // block_type: three short windows
	w.put(1, 1)  // mixed_block_flag: the two lowest bands long
	for range 2 {
		w.put(0, 5) // table_select
	}
	for range 3 {
		w.put(0, 3) // subblock_gain
	}
	w.put(0, 1) // scalefac_scale
	w.put(0, 1) // count1table_select
	if len(w.b) != 4+9 || w.n != 8*len(w.b) {
		panic("the header and side info aren't 13 whole bytes")
	}
	frame := append(w.b, make([]byte, 208-len(w.b))...) // the main data: zeros
	return bytes.Repeat(frame, n)
}

// TestDecodeSurvivesADecoderPanic: decodeAudioData's decode runs the
// decoders behind the decode boundary, as the player does (safe.go). On
// an MPEG-2 Layer III stream with mixed short blocks go-mp3 indexes past
// its scale factors; Decode is that file's error (decodeAudioData's
// EncodingError), not a panic taking the app down with it. The player
// shows the same file's error.
func TestDecodeSurvivesADecoderPanic(t *testing.T) {
	b := mpeg2MixedBlocks(4)
	if size, _, rate, valid, ok := mp3Header(b, 0); !valid || !ok || size != 208 || rate != 22050 {
		t.Fatalf("the frame's header reads as %d bytes at %d Hz (valid %v, decodable %v), want 208 at 22050", size, rate, valid, ok)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, rate := range []int{0, 48000} {
		p, err := Decode(ctx, b, "audio/mpeg", rate)
		if !errors.Is(err, errCrashed) {
			t.Errorf("Decode at %d Hz: %v, %v; want the decoder's failure (%v)", rate, p, err, errCrashed)
		}
	}
	// the player's path, behind the same boundary
	if _, err := safeOpen(ctx, b, "audio/mpeg"); !errors.Is(err, errCrashed) {
		t.Errorf("the player's open: %v, want the decoder's failure", err)
	}
}

// TestDecodeStopsAtItsCap: past maxDecodeSamples (frames × channels) a
// decode is ErrDecodeTooLong, read from the file or made by converting it
// to a higher rate; a file right at the cap decodes.
func TestDecodeStopsAtItsCap(t *testing.T) {
	ctx := context.Background()
	const samples = 48000 // TonesWAV: 3 s at 16 kHz, mono
	setDecodeCap(t, samples)
	p, err := Decode(ctx, mediatest.TonesWAV, "audio/wav", 0)
	if err != nil || p.Frames() != samples {
		t.Fatalf("at the cap: %d frames, %v; want %d", p.Frames(), err, samples)
	}
	setDecodeCap(t, samples-1)
	if p, err := Decode(ctx, mediatest.TonesWAV, "audio/wav", 0); !errors.Is(err, ErrDecodeTooLong) {
		t.Errorf("a sample past the cap: %d frames, %v; want ErrDecodeTooLong", p.Frames(), err)
	}
	// 16 kHz to 48 kHz makes 144000: past a cap the file itself fits
	setDecodeCap(t, 100000)
	if p, err := Decode(ctx, mediatest.TonesWAV, "audio/wav", 48000); !errors.Is(err, ErrDecodeTooLong) {
		t.Errorf("converted past the cap: %d frames, %v; want ErrDecodeTooLong", p.Frames(), err)
	}
	if p, err := Decode(ctx, mediatest.TonesWAV, "audio/wav", 32000); err != nil || p.Frames() < 2*samples-64 || p.Frames() > 2*samples {
		t.Errorf("converted to under the cap: %d frames, %v; want about %d", p.Frames(), err, 2*samples)
	}
}
