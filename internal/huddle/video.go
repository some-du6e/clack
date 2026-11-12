package huddle

import (
	"image"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"github.com/thesyncim/goh264"
)

// VideoFrame is the newest decoded picture of one remote video. Img is
// owned by the session; treat it as read-only. Seq grows with each frame
// (a new Img pointer every time, so Gio re-uploads it).
type VideoFrame struct {
	Img *image.RGBA
	Seq uint64
	At  time.Time
}

// videoSink decodes one remote H.264 track into RGBA frames off the UI
// goroutine. Pictures are converted only while someone looks at them
// (Session.Frame marks the stream wanted) and at most maxFPS times a second.
type videoSink struct {
	key    string // attendee id, with "#content" for a screen share
	ssrc   uint32
	pc     *webrtc.PeerConnection
	notify func()
	logf   func(string, ...any)
	nudged bool // show asked for a redraw since the stream was last wanted (run's goroutine)

	mu     sync.Mutex
	frame  VideoFrame
	pool   [3]*image.RGBA
	next   int
	wanted atomic.Int64 // unix nanos of the last Frame() call

	keyframe  atomic.Bool
	decoded   atomic.Int64
	packets   atomic.Int64
	lastPLI   time.Time
	lastShown time.Time
}

const maxFPS = 30

func (v *videoSink) pli() {
	if v.pc == nil || time.Since(v.lastPLI) < time.Second {
		return
	}
	v.lastPLI = time.Now()
	_ = v.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: v.ssrc}})
}

func (v *videoSink) run(track *webrtc.TrackRemote, done <-chan struct{}) {
	sb := samplebuilder.New(512, &codecs.H264Packet{}, track.Codec().ClockRate, samplebuilder.WithMaxTimeDelay(time.Second))
	dec := goh264.NewDecoder()
	v.pli()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			if !v.keyframe.Load() { // ask until the first picture decodes
				v.lastPLI = time.Time{}
				v.pli()
			}
		default:
		}
		_ = track.SetReadDeadline(time.Now().Add(2 * time.Second))
		pkt, _, err := track.ReadRTP()
		if err != nil {
			select {
			case <-done:
				return
			default:
			}
			if isTimeout(err) {
				continue
			}
			return
		}
		v.packets.Add(1)
		sb.Push(pkt)
		for s := sb.Pop(); s != nil; s = sb.Pop() {
			if s.PrevDroppedPackets > 0 {
				v.pli()
			}
			frames, err := dec.DecodeFrames(s.Data)
			if err != nil {
				v.pli()
				continue
			}
			for _, f := range frames {
				v.decoded.Add(1)
				if f.KeyFrame {
					v.keyframe.Store(true)
				}
				v.show(f)
			}
		}
	}
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	t, ok := err.(timeout)
	return ok && t.Timeout()
}

func (v *videoSink) show(f *goh264.Frame) {
	now := time.Now()
	if now.UnixNano()-v.wanted.Load() > int64(2*time.Second) && !v.lastShown.IsZero() {
		// Nobody drew it for 2 s: decode (for references) but don't
		// convert. The window draws only when something changes, so a
		// stream that paused that long while on screen would stay
		// frozen: ask for one redraw, which marks it wanted again
		// (latest) if a tile still shows it.
		if !v.nudged {
			v.nudged = true
			if v.notify != nil {
				v.notify()
			}
		}
		return
	}
	v.nudged = false
	if now.Sub(v.lastShown) < time.Second/maxFPS {
		return
	}
	if f.BitDepthLuma > 8 || len(f.Y) == 0 || f.ChromaFormatIDC != 1 {
		return
	}
	w, h := f.Width, f.Height
	if w <= 0 || h <= 0 || f.YStride <= 0 || f.CStride <= 0 ||
		(f.CropTop+h-1)*f.YStride+f.CropLeft+w > len(f.Y) ||
		((f.CropTop+h-1)/2)*f.CStride+(f.CropLeft+w-1)/2 >= min(len(f.Cb), len(f.Cr)) {
		return
	}
	v.lastShown = now
	img := v.pool[v.next]
	if img == nil || img.Rect.Dx() != w || img.Rect.Dy() != h {
		img = image.NewRGBA(image.Rect(0, 0, w, h))
		v.pool[v.next] = img
	}
	v.next = (v.next + 1) % len(v.pool)
	yuv420ToRGBA(img, f.Y, f.Cb, f.Cr, f.YStride, f.CStride, f.CropLeft, f.CropTop, f.VideoFullRangeFlag != 0)
	// A fresh header over the pooled pixels: Gio keys textures by image.
	out := &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
	v.mu.Lock()
	v.frame = VideoFrame{Img: out, Seq: v.frame.Seq + 1, At: now}
	v.mu.Unlock()
	if v.notify != nil {
		v.notify()
	}
}

func (v *videoSink) latest() VideoFrame {
	v.wanted.Store(time.Now().UnixNano())
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.frame
}

func (v *videoSink) size() (w, h int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.frame.Img == nil {
		return 0, 0
	}
	return v.frame.Img.Rect.Dx(), v.frame.Img.Rect.Dy()
}

func clamp8(x int32) uint8 {
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return uint8(x)
}

// yuv420ToRGBA converts BT.601 (limited or full range) 4:2:0 planes.
func yuv420ToRGBA(dst *image.RGBA, Y, Cb, Cr []byte, ys, cs, cropX, cropY int, full bool) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := 0; y < h; y++ {
		sy := y + cropY
		yrow := Y[sy*ys+cropX:]
		crow := (sy / 2) * cs
		pix := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			sx := x + cropX
			yy := int32(yrow[x])
			cb := int32(Cb[crow+sx/2]) - 128
			cr := int32(Cr[crow+sx/2]) - 128
			var r, g, b int32
			if full {
				yy <<= 16
				r = (yy + 91881*cr) >> 16
				g = (yy - 22554*cb - 46802*cr) >> 16
				b = (yy + 116130*cb) >> 16
			} else {
				yy = (yy - 16) * 76309
				r = (yy + 104597*cr) >> 16
				g = (yy - 25675*cb - 53279*cr) >> 16
				b = (yy + 132201*cb) >> 16
			}
			i := x * 4
			pix[i] = clamp8(r)
			pix[i+1] = clamp8(g)
			pix[i+2] = clamp8(b)
			pix[i+3] = 0xff
		}
	}
}
