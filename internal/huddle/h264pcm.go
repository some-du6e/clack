package huddle

// A tiny pure-Go H.264 encoder for synthetic video: constrained baseline,
// CAVLC, every changed macroblock sent raw as I_PCM and every unchanged one
// skipped (P_Skip copies it from the previous picture, motion vector 0).
// Lossless, so skipping is exact, and simple enough to get right; but an
// I_PCM picture costs 1.5 bytes a pixel, so it only suits small,
// mostly-still pictures like the test camera. Real cameras go through the
// OS encoder (VideoToolbox on macOS). goh264 is decode-only and no pure-Go
// H.264 encoder is maintained, so this is what the fake Chime tests and
// cgo-less builds publish.

import (
	"errors"
	"fmt"
)

// maxPCMPixels caps the I_PCM encoder at 640x360 (a keyframe is ~350 KB).
const maxPCMPixels = 640 * 360

type pcmEncoder struct {
	w, h     int // picture size (even)
	mbw, mbh int
	full     bool
	cur      [3][]byte // MB-aligned Y, Cb, Cr planes
	prev     [3][]byte
	have     bool
	frameNum int
	idrID    int
	bw       bitWriter
}

func newPCMEncoder(w, h int) (*pcmEncoder, error) {
	if w <= 0 || h <= 0 || w%2 != 0 || h%2 != 0 {
		return nil, fmt.Errorf("h264pcm: bad size %dx%d", w, h)
	}
	if w*h > maxPCMPixels {
		return nil, fmt.Errorf("h264pcm: %dx%d is too big for I_PCM", w, h)
	}
	e := &pcmEncoder{w: w, h: h, mbw: (w + 15) / 16, mbh: (h + 15) / 16}
	for i := range e.cur {
		n := e.mbw * 16 * e.mbh * 16
		if i > 0 {
			n /= 4
		}
		e.cur[i], e.prev[i] = make([]byte, n), make([]byte, n)
	}
	return e, nil
}

func (e *pcmEncoder) Name() string   { return "pure-Go I_PCM" }
func (e *pcmEncoder) SetBitrate(int) {}
func (e *pcmEncoder) Close()         {}

// load copies f into the MB-aligned planes, replicating the right and
// bottom edges into the padding. PCM samples are kept ≥ 1 (H.264 2003
// forbade 0 there; some decoders still check).
func (e *pcmEncoder) load(f *CameraFrame) {
	pw, ph := e.mbw*16, e.mbh*16
	Y := e.cur[0]
	for y := 0; y < ph; y++ {
		sy := min(y, f.H-1)
		src := f.Y[sy*f.YStride:]
		dst := Y[y*pw:]
		for x := 0; x < pw; x++ {
			dst[x] = max(1, src[min(x, f.W-1)])
		}
	}
	cw, ch := pw/2, ph/2
	U, V := e.cur[1], e.cur[2]
	for y := 0; y < ch; y++ {
		sy := min(y, f.H/2-1)
		src := f.UV[sy*f.UVStride:]
		for x := 0; x < cw; x++ {
			sx := min(x, f.W/2-1)
			U[y*cw+x] = max(1, src[2*sx])
			V[y*cw+x] = max(1, src[2*sx+1])
		}
	}
}

func (e *pcmEncoder) mbSame(mx, my int) bool {
	pw := e.mbw * 16
	for y := 0; y < 16; y++ {
		o := (my*16+y)*pw + mx*16
		if string(e.cur[0][o:o+16]) != string(e.prev[0][o:o+16]) {
			return false
		}
	}
	cw := pw / 2
	for p := 1; p < 3; p++ {
		for y := 0; y < 8; y++ {
			o := (my*8+y)*cw + mx*8
			if string(e.cur[p][o:o+8]) != string(e.prev[p][o:o+8]) {
				return false
			}
		}
	}
	return true
}

func (e *pcmEncoder) writePCM(mx, my int) {
	b := &e.bw
	b.align()
	pw := e.mbw * 16
	for y := 0; y < 16; y++ {
		o := (my*16+y)*pw + mx*16
		b.bytes(e.cur[0][o : o+16])
	}
	cw := pw / 2
	for p := 1; p < 3; p++ {
		for y := 0; y < 8; y++ {
			o := (my*8+y)*cw + mx*8
			b.bytes(e.cur[p][o : o+8])
		}
	}
}

// Encode returns one Annex B access unit; a keyframe (the first picture,
// or when asked) is SPS + PPS + an IDR picture of I_PCM macroblocks.
func (e *pcmEncoder) Encode(f *CameraFrame, key bool) ([]byte, error) {
	if f == nil || f.W != e.w || f.H != e.h || len(f.Y) < f.YStride*(f.H-1)+f.W || len(f.UV) < f.UVStride*(f.H/2-1)+f.W {
		return nil, errors.New("h264pcm: frame doesn't match the encoder")
	}
	if f.FullRange != e.full {
		e.full, key = f.FullRange, true // the range is in the SPS
	}
	e.load(f)
	key = key || !e.have
	var out []byte
	if key {
		out = appendNAL(out, 0x67, e.sps())
		out = appendNAL(out, 0x68, e.pps())
		out = appendNAL(out, 0x65, e.slice(true))
		e.frameNum = 1
		e.idrID = (e.idrID + 1) & 0xffff
	} else {
		out = appendNAL(out, 0x41, e.slice(false))
		e.frameNum = (e.frameNum + 1) % 16
	}
	e.cur, e.prev = e.prev, e.cur
	e.have = true
	return out, nil
}

func (e *pcmEncoder) sps() []byte {
	b := &e.bw
	b.reset()
	b.u(8, 66)   // profile_idc: baseline
	b.u(8, 0xe0) // constraint_set0..2: constrained baseline (42e0..)
	b.u(8, 31)   // level 3.1
	b.ue(0)      // seq_parameter_set_id
	b.ue(0)      // log2_max_frame_num_minus4: frame_num is 4 bits
	b.ue(2)      // pic_order_cnt_type 2: output order = decode order
	b.ue(1)      // max_num_ref_frames
	b.u(1, 0)    // gaps_in_frame_num_value_allowed_flag
	b.ue(uint32(e.mbw - 1))
	b.ue(uint32(e.mbh - 1))
	b.u(1, 1) // frame_mbs_only_flag
	b.u(1, 1) // direct_8x8_inference_flag
	cropR, cropB := (e.mbw*16-e.w)/2, (e.mbh*16-e.h)/2
	if cropR > 0 || cropB > 0 {
		b.u(1, 1)
		b.ue(0)
		b.ue(uint32(cropR))
		b.ue(0)
		b.ue(uint32(cropB))
	} else {
		b.u(1, 0)
	}
	b.u(1, 1) // vui_parameters_present_flag
	b.u(1, 0) // aspect_ratio_info_present_flag
	b.u(1, 0) // overscan_info_present_flag
	b.u(1, 1) // video_signal_type_present_flag
	b.u(3, 5) // video_format: unspecified
	if e.full {
		b.u(1, 1)
	} else {
		b.u(1, 0)
	}
	b.u(1, 0) // colour_description_present_flag
	b.u(1, 0) // chroma_loc_info_present_flag
	b.u(1, 0) // timing_info_present_flag
	b.u(1, 0) // nal_hrd_parameters_present_flag
	b.u(1, 0) // vcl_hrd_parameters_present_flag
	b.u(1, 0) // pic_struct_present_flag
	b.u(1, 1) // bitstream_restriction_flag
	b.u(1, 1) // motion_vectors_over_pic_boundaries_flag
	b.ue(0)   // max_bytes_per_pic_denom
	b.ue(0)   // max_bits_per_mb_denom
	b.ue(10)  // log2_max_mv_length_horizontal
	b.ue(10)  // log2_max_mv_length_vertical
	b.ue(0)   // max_num_reorder_frames: show each picture at once
	b.ue(1)   // max_dec_frame_buffering
	b.trailing()
	return b.done()
}

func (e *pcmEncoder) pps() []byte {
	b := &e.bw
	b.reset()
	b.ue(0)   // pic_parameter_set_id
	b.ue(0)   // seq_parameter_set_id
	b.u(1, 0) // entropy_coding_mode_flag: CAVLC
	b.u(1, 0) // bottom_field_pic_order_in_frame_present_flag
	b.ue(0)   // num_slice_groups_minus1
	b.ue(0)   // num_ref_idx_l0_default_active_minus1
	b.ue(0)   // num_ref_idx_l1_default_active_minus1
	b.u(1, 0) // weighted_pred_flag
	b.u(2, 0) // weighted_bipred_idc
	b.se(0)   // pic_init_qp_minus26
	b.se(0)   // pic_init_qs_minus26
	b.se(0)   // chroma_qp_index_offset
	b.u(1, 1) // deblocking_filter_control_present_flag
	b.u(1, 0) // constrained_intra_pred_flag
	b.u(1, 0) // redundant_pic_cnt_present_flag
	b.trailing()
	return b.done()
}

func (e *pcmEncoder) slice(idr bool) []byte {
	b := &e.bw
	b.reset()
	b.ue(0) // first_mb_in_slice
	if idr {
		b.ue(7) // slice_type: I (all slices)
	} else {
		b.ue(5) // P
	}
	b.ue(0) // pic_parameter_set_id
	if idr {
		b.u(4, 0) // frame_num
		b.ue(uint32(e.idrID))
	} else {
		b.u(4, uint32(e.frameNum))
		b.u(1, 0) // num_ref_idx_active_override_flag
		b.u(1, 0) // ref_pic_list_modification_flag_l0
	}
	// dec_ref_pic_marking (every picture is a reference)
	if idr {
		b.u(1, 0) // no_output_of_prior_pics_flag
		b.u(1, 0) // long_term_reference_flag
	} else {
		b.u(1, 0) // adaptive_ref_pic_marking_mode_flag: sliding window
	}
	b.se(0) // slice_qp_delta
	b.ue(1) // disable_deblocking_filter_idc: off (PCM is exact)
	if idr {
		for my := 0; my < e.mbh; my++ {
			for mx := 0; mx < e.mbw; mx++ {
				b.ue(25) // mb_type I_PCM
				e.writePCM(mx, my)
			}
		}
	} else {
		skip := 0
		for my := 0; my < e.mbh; my++ {
			for mx := 0; mx < e.mbw; mx++ {
				if e.mbSame(mx, my) {
					skip++
					continue
				}
				b.ue(uint32(skip)) // mb_skip_run
				skip = 0
				b.ue(30) // mb_type I_PCM in a P slice (5 + 25)
				e.writePCM(mx, my)
			}
		}
		if skip > 0 {
			b.ue(uint32(skip))
		}
	}
	b.trailing()
	return b.done()
}

// appendNAL appends a start code, the NAL header byte and rbsp with
// emulation prevention bytes.
func appendNAL(out []byte, hdr byte, rbsp []byte) []byte {
	out = append(out, 0, 0, 0, 1, hdr)
	zeros := 0
	for _, c := range rbsp {
		if zeros >= 2 && c <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, c)
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

// bitWriter writes MSB-first bits and Exp-Golomb codes.
type bitWriter struct {
	b    []byte
	cur  uint64
	bits uint
}

func (w *bitWriter) reset() { w.b, w.cur, w.bits = w.b[:0], 0, 0 }

func (w *bitWriter) u(n uint, v uint32) {
	w.cur = w.cur<<n | uint64(v)&(1<<n-1)
	w.bits += n
	for w.bits >= 8 {
		w.bits -= 8
		w.b = append(w.b, byte(w.cur>>w.bits))
	}
	w.cur &= 1<<w.bits - 1
}

func (w *bitWriter) ue(v uint32) {
	x := uint64(v) + 1
	n := uint(0)
	for t := x; t > 1; t >>= 1 {
		n++
	}
	w.u(n, 0)
	if n+1 > 32 {
		w.u(n+1-32, uint32(x>>32))
		w.u(32, uint32(x))
		return
	}
	w.u(n+1, uint32(x))
}

func (w *bitWriter) se(v int32) {
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

func (w *bitWriter) align() {
	if w.bits > 0 {
		w.u(8-w.bits, 0)
	}
}

// bytes writes whole bytes (the writer must be aligned).
func (w *bitWriter) bytes(p []byte) { w.b = append(w.b, p...) }

func (w *bitWriter) trailing() {
	w.u(1, 1)
	w.align()
}

// done returns a copy of the written bytes.
func (w *bitWriter) done() []byte { return append([]byte(nil), w.b...) }
