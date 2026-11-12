package huddle

import (
	"sync/atomic"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"github.com/thesyncim/goh264"
)

// fakeVideoIn is what the fake Chime's receiver saw of one of your video
// sends (the camera, the screen share's content attendee).
type fakeVideoIn struct {
	pkts    atomic.Int64
	frames  atomic.Int64 // pictures decoded
	keys    atomic.Int64
	keysPLI atomic.Int64 // keyframes after the fake's PLI
	pliSent atomic.Bool  // the PLI sent after a few pictures
	w, h    atomic.Int32

	undecodable atomic.Int64 // access units that didn't decode (no keyframe before them, a packet lost)
	keyReqs     atomic.Int64 // the PLIs those asked for

	// dropFirst (tests): the receiver never gets the first access unit,
	// as when the sender wrote its first keyframe while pion's DTLS
	// transport said connected but SRTP wasn't up yet: pion's
	// srtpWriterFuture drops those writes without an error.
	dropFirst atomic.Bool
}

// keyReqEvery is how many undecodable access units go by before the
// receiver asks for a keyframe again (its PLI or that keyframe was lost
// too).
const keyReqEvery = 15

// recv decodes track like a Chime receiver: an access unit it can't
// decode asks for a keyframe (PLI), as a browser's receiver does when its
// decoder needs one, so a lost first keyframe recovers instead of the
// pictures stopping for good (the senders send keyframes only when asked).
// After a few pictures it asks once more (pliSent), to check the sender
// answers. pli writes the PLI.
func (v *fakeVideoIn) recv(track *webrtc.TrackRemote, maxLate uint16, pli func()) {
	sb := samplebuilder.New(maxLate, &codecs.H264Packet{}, track.Codec().ClockRate, samplebuilder.WithMaxTimeDelay(time.Second))
	dec := goh264.NewDecoder()
	drop := v.dropFirst.Load()
	bad := 0 // undecodable access units since the last picture
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		v.pkts.Add(1)
		if drop {
			drop = !pkt.Marker // the access unit's last packet
			continue
		}
		sb.Push(pkt)
		for s := sb.Pop(); s != nil; s = sb.Pop() {
			key := hasIDR(s.Data)
			frames, err := dec.DecodeFrames(s.Data)
			if err != nil || len(frames) == 0 {
				v.undecodable.Add(1)
				if bad%keyReqEvery == 0 {
					v.keyReqs.Add(1)
					pli()
				}
				bad++
				continue
			}
			bad = 0
			for _, fr := range frames {
				v.frames.Add(1)
				v.w.Store(int32(fr.Width))
				v.h.Store(int32(fr.Height))
			}
			if key {
				v.keys.Add(1)
				if v.pliSent.Load() {
					v.keysPLI.Add(1)
				}
			}
			if v.frames.Load() >= 5 && v.pliSent.CompareAndSwap(false, true) {
				pli()
			}
		}
	}
}
