package goaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
)

// Ogg Opus (RFC 3533 pages, RFC 7845 Opus in them), read here rather than
// by opus-go's reader, whose OpusTags parser allocates the comment count
// the file states: the first logical stream's packets in order (laced
// across pages; one spread over pages is copied, the rest are slices of
// the file), OpusHead's channels and pre-skip, OpusTags skipped unread,
// and the last granule position, the stream's end. A page that doesn't
// fit is the end of the file; bytes that aren't a page are skipped to the
// next "OggS", as a demuxer resyncs.

// maxOggPacket is the most a packet spread over pages may take: an Opus
// packet is at most 120 ms of 1275-byte frames (61 KB).
const maxOggPacket = 1 << 20

var oggCapture = []byte("OggS")

// oggReader is the packet assembly's state between pages.
type oggReader struct {
	headers int // packets seen of OpusHead, OpusTags (2: audio from here)
	ch, pre int
	pk      opusPackets
	// cur is the packet under way when it spans pages (inPacket); dropping:
	// it's skipped (OpusTags, or past any Opus packet's size)
	cur      []byte
	inPacket bool
	dropping bool
}

// packet takes a whole packet (a slice of the file, or a copy).
func (r *oggReader) packet(p []byte) error {
	switch r.headers {
	case 0:
		r.headers++
		if len(p) >= 7 && string(p[:7]) == "\x01vorbis" {
			return fmt.Errorf("%w: Ogg Vorbis", errUnsupported)
		}
		if len(p) < 19 || string(p[:8]) != "OpusHead" || p[8]&0xF0 != 0 {
			return fmt.Errorf("%w: Ogg without OpusHead", errUnsupported)
		}
		if p[18] != 0 { // a channel mapping family past mono and stereo
			return fmt.Errorf("%w: Ogg Opus mapping family %d", errUnsupported, p[18])
		}
		r.ch, r.pre = int(p[9]), int(binary.LittleEndian.Uint16(p[10:]))
		return nil
	case 1:
		r.headers++ // OpusTags, unread
		return nil
	}
	return r.pk.add(p)
}

// page takes one page's lacing values and body.
func (r *oggReader) page(flags byte, segs, body []byte) error {
	cont := flags&0x01 != 0
	// a continued page with no packet under way begins with the tail of
	// one that began before the file (or before a resync): skip to its end
	skipTail := cont && !r.inPacket
	if !cont && r.inPacket {
		r.cur, r.inPacket, r.dropping = r.cur[:0], false, false // a packet that never ended
	}
	start, p := 0, 0
	for k, s := range segs {
		p += int(s)
		if s == 255 {
			if k == len(segs)-1 && !skipTail { // it goes on in the next page
				if !r.dropping {
					if r.headers == 1 || len(r.cur)+p-start > maxOggPacket {
						r.dropping, r.cur = true, r.cur[:0]
					} else {
						r.cur = append(r.cur, body[start:p]...)
					}
				}
				r.inPacket = true
			}
			continue
		}
		// a packet ends at p
		if skipTail {
			skipTail, start = false, p
			continue
		}
		var pkt []byte
		switch {
		case r.dropping:
		case r.inPacket:
			r.cur = append(r.cur, body[start:p]...)
			pkt = append([]byte(nil), r.cur...)
		default:
			pkt = body[start:p]
		}
		dropped := r.dropping
		r.cur, r.inPacket, r.dropping = r.cur[:0], false, false
		start = p
		if dropped {
			if r.headers == 1 {
				r.headers++ // OpusTags, read past
			}
			continue
		}
		if err := r.packet(pkt); err != nil {
			return err
		}
	}
	return nil
}

func openOgg(ctx context.Context, b []byte) (stream, error) {
	var (
		r      oggReader
		serial uint32
		have   bool  // the first stream's serial is known
		end    int64 = -1
	)
	for off, step := 0, 0; off < len(b); step++ {
		if done(ctx, step) {
			return nil, ctx.Err()
		}
		if len(b)-off < 27 || !bytes.Equal(b[off:off+4], oggCapture) || b[off+4] != 0 {
			k := bytes.Index(b[off+1:], oggCapture)
			if k < 0 {
				break
			}
			off += 1 + k
			continue
		}
		h := b[off:]
		flags := h[5]
		granule := int64(binary.LittleEndian.Uint64(h[6:]))
		ser := binary.LittleEndian.Uint32(h[14:])
		nseg := int(h[26])
		if len(h)-27 < nseg {
			break // a page cut off: the file ends here
		}
		segs := h[27 : 27+nseg]
		size := 0
		for _, s := range segs {
			size += int(s)
		}
		if size > len(h)-27-nseg {
			break
		}
		body := h[27+nseg : 27+nseg+size]
		off += 27 + nseg + size
		if !have {
			if flags&0x02 == 0 {
				continue // not a stream's first page: wait for one
			}
			serial, have = ser, true
		} else if ser != serial {
			continue // another logical stream
		}
		if err := r.page(flags, segs, body); err != nil {
			return nil, err
		}
		if granule >= 0 { // -1: no packet ends on the page
			end = granule
		}
		if flags&0x04 != 0 {
			break // the stream's last page
		}
	}
	if r.headers == 0 {
		return nil, fmt.Errorf("%w: Ogg without an Opus stream", errUnsupported)
	}
	s, err := newOpus("ogg-opus", &r.pk, r.ch, r.pre, end)
	if err != nil {
		return nil, err
	}
	return s, nil
}
