package huddle

import (
	"errors"
	"sync"
)

// More people in a Fake meeting, for the event log's tests: they join
// and leave Chime's audio (audio_stream_id_info), mute, and send the
// data-channel peer messages Slack's client sends (muteRequest,
// kickNotification). Everything stays on loopback.

type fakePeers struct {
	mu     sync.Mutex
	next   uint32
	stream map[string]uint32 // attendee → audio stream
}

func (f *fakeChime) peers() *fakePeers {
	ps := &f.others
	ps.mu.Lock()
	if ps.stream == nil {
		ps.next, ps.stream = 10, map[string]uint32{fakeRemoteAtt: 2}
	}
	ps.mu.Unlock()
	return ps
}

func (f *fakeChime) liveSend() (func(int, func(*pbw)), error) {
	f.aware.mu.Lock()
	send := f.aware.send
	f.aware.mu.Unlock()
	if send == nil {
		return nil, errors.New("fake chime: not joined")
	}
	return send, nil
}

// FakeAttendee is the Chime attendee id a Fake gives user (PeerJoin).
func FakeAttendee(user string) string {
	if user == fakeRemote {
		return fakeRemoteAtt
	}
	return "att-" + user
}

// FakeRemoteAttendee is the other person's attendee id.
const FakeRemoteAttendee = fakeRemoteAtt

// PeerJoin has user join the meeting's audio (on a stream of their own),
// as Chime's realtime presence reports it.
func (x *Fake) PeerJoin(user string) error {
	send, err := x.f.liveSend()
	if err != nil {
		return err
	}
	att := FakeAttendee(user)
	ps := x.f.peers()
	ps.mu.Lock()
	id, ok := ps.stream[att]
	if !ok {
		id = ps.next
		ps.next++
		ps.stream[att] = id
	}
	ps.mu.Unlock()
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.uint(1, uint64(id))
				w.str(2, att)
				w.str(4, "E1-"+FakeRoomID+"-"+user)
				w.boolean(3, false)
			})
		})
	})
	return nil
}

// PeerLeave has the attendee leave the meeting's audio: Chime drops their
// stream.
func (x *Fake) PeerLeave(att string) error {
	send, err := x.f.liveSend()
	if err != nil {
		return err
	}
	ps := x.f.peers()
	ps.mu.Lock()
	id, ok := ps.stream[att]
	delete(ps.stream, att)
	ps.mu.Unlock()
	if !ok {
		return errors.New("fake chime: no such attendee")
	}
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) { w.uint(1, uint64(id)) })
		})
	})
	return nil
}

// PeerMuted sets the attendee's microphone state as Chime reports it.
func (x *Fake) PeerMuted(att string, muted bool) error {
	send, err := x.f.liveSend()
	if err != nil {
		return err
	}
	ps := x.f.peers()
	ps.mu.Lock()
	id, ok := ps.stream[att]
	ps.mu.Unlock()
	if !ok {
		return errors.New("fake chime: no such attendee")
	}
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.uint(1, uint64(id))
				w.boolean(3, muted)
			})
		})
	})
	return nil
}

// PeerMessage sends m on the data topic from the attendee from (a
// muteRequest, a kickNotification…), as Slack's client sends them.
func (x *Fake) PeerMessage(from string, m *PeerMessage) error {
	send, err := x.f.liveSend()
	if err != nil {
		return err
	}
	if m.FromPeerIDString == "" {
		m.FromPeerIDString = from
	}
	b := m.Encode()
	send(ftDataMessage, func(w *pbw) {
		w.msg(23, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.str(1, TopicData)
				w.bytes(2, b)
				w.str(4, from)
			})
		})
	})
	return nil
}

// TranscriptionStatus sends Chime's transcription status event (started,
// interrupted, resumed, stopped, failed) on the transcript topic.
func (x *Fake) TranscriptionStatus(status string) error {
	send, err := x.f.liveSend()
	if err != nil {
		return err
	}
	n := 0
	for i, s := range [...]string{"", "started", "interrupted", "resumed", "stopped", "failed"} {
		if s == status {
			n = i
		}
	}
	if n == 0 {
		return errors.New("fake chime: unknown transcription status")
	}
	var tw pbw
	tw.msg(1, func(w *pbw) { // events
		w.msg(1, func(w *pbw) { w.uint(1, uint64(n)) }) // status.type
	})
	send(ftDataMessage, func(w *pbw) {
		w.msg(23, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.str(1, TopicTranscript)
				w.bytes(2, tw.b)
				w.str(4, "aws:chime")
			})
		})
	})
	return nil
}
