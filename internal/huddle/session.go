// Package huddle joins Slack huddles natively: rooms.join for the Amazon
// Chime meeting, Chime's signaling websocket, and a pion WebRTC peer
// connection for audio (Opus) in and out and remote video (H.264) in.
// It has no UI; internal/ui/huddleui draws the bar and the window.
package huddle

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/dtls/v3"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle/avperm"
)

// Phase is where the session is.
type Phase int

const (
	Connecting Phase = iota
	Connected
	Reconnecting
	Ended
)

func (p Phase) String() string {
	return [...]string{"connecting", "connected", "reconnecting", "ended"}[p]
}

// Participant is one person in the huddle.
type Participant struct {
	UserID     string
	AttendeeID string
	InMedia    bool // Chime has their audio
	Muted      bool
	Volume     float64 // 0..1, from Chime's audio metadata
	SpokeAt    time.Time
	Camera     bool // sending camera video
	Sharing    bool // sharing their screen
	Reaction   string
	ReactedAt  time.Time
	Self       bool
}

// Speaking reports whether the speaking ring should show at now.
func (p Participant) Speaking(now time.Time) bool {
	return !p.Muted && !p.SpokeAt.IsZero() && now.Sub(p.SpokeAt) < 450*time.Millisecond
}

// Video is a remote video the session receives.
type Video struct {
	Key        string // pass to Session.Frame
	AttendeeID string
	UserID     string
	Content    bool // screen share
	W, H       int
}

// Caption is one live-caption line.
type Caption struct {
	ID      string
	UserID  string
	Text    string
	Partial bool
	At      time.Time
}

// State is a snapshot of the session.
type State struct {
	Phase         Phase
	Err           error
	RoomID        string
	Channel       string
	ThreadChannel string
	ThreadTS      string
	Name          string
	Started       time.Time
	SelfAttendee  string
	Muted         bool
	Deafened      bool
	CaptionsOn    bool
	Mic           MicStatus     // MicStatus.Problem: tell the user
	MicErr        string        // why, for MicFailed and MicSilent
	Participants  []Participant // you first, then by join order
	Videos        []Video
	Captions      []Caption    // most recent last
	Camera        CameraStatus // yours (SetCamera)
	// Invites are the people the huddle rang who aren't in it (yet), in
	// the order they were rung: Slack draws them as pending tiles.
	Invites []Invite
}

// Invite is one rung person: Status is Slack's invite status ("pending",
// "joining_soon", "declined"; "missed" and "accepted" don't show).
type Invite struct {
	UserID string
	Status string
}

// Options configure a session.
type Options struct {
	Client Caller // *slack.Client
	SelfID string
	Region string // Chime media region for rooms.join ("" = us-east-2)
	// Device is the sound card; nil opens the system default (or a test
	// tone when CLACK_HUDDLE_TONE=<hz> is set).
	Device Device
	// MicAccess replaces the OS permission request for the default
	// microphone, and OpenAudio replaces opening the default sound card
	// (once for the speakers, once for the microphone): tests, the demo.
	MicAccess func() <-chan avperm.Status
	OpenAudio func(logf func(string, ...any)) (Device, error)
	Muted     bool
	// OpenCamera replaces the system camera for SetCamera (tests, the demo:
	// NewTestCamera); nil = the default camera behind the OS permission.
	OpenCamera func(logf func(string, ...any)) (Camera, error)
	Logf       func(format string, args ...any)
	// OnChange is called (from any goroutine, coalesced) when State or a
	// video frame changed.
	OnChange func()
	// OnPeer receives every peer message (data-channel, awareness and
	// transcript catch-up topics), for Taut's api.huddles.onPeerMessage.
	OnPeer func(PeerEvent)
	// OnEnd is called once when the session ends (err nil = you left).
	OnEnd func(err error)
}

// Session is a joined huddle. Its methods are safe for concurrent use.
type Session struct {
	o      Options
	join   *joinInfo
	dev    Device // speakers (and the microphone for Options.Device)
	micDev Device // the default microphone, once the OS allowed it
	play   playBuffer
	mic    *micFramer

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	phase     Phase
	err       error
	muted     bool
	deafened  bool
	captions  bool
	micStatus MicStatus
	micErr    string
	people    map[string]*Participant // by user id
	order     []string
	invites   map[string]string // user -> invite status (Invite)
	inviteOrd []string
	byAtt     map[string]string // attendee id -> people key (user id, or "U…/<attendee>" for a second device)
	streamAt  map[uint32]string // audio stream id -> attendee id
	sources   map[uint32]streamDesc
	lines     []Caption
	sinks     map[string]*videoSink // by video key
	cam       camPipe               // your camera (videosend.go)
	ended     bool
	leaving   bool // LEAVE sent: a closing socket is expected, not a drop

	// connection (replaced on reconnect)
	conn     *websocket.Conn
	wmu      sync.Mutex
	pc       *webrtc.PeerConnection
	audio    *webrtc.TrackLocalStaticSample
	waiters  map[int][]chan *signalFrame
	negMu    sync.Mutex
	slots    []uint32          // video m-section -> subscribed stream id
	ssrcOf   map[uint32]uint32 // ssrc -> stream id (SUBSCRIBE_ACK tracks)
	slotSSRC map[int]uint32
	connDone chan struct{}

	changeMu sync.Mutex
	changeT  *time.Timer

	stats struct {
		audioIn, audioOut, audioLost int64
		inLevel                      float64
	}
	doneCh   chan struct{}
	doneOnce sync.Once
}

// ErrEnded is the end reason when the huddle itself ended (Chime 4410).
var ErrEnded = errors.New("huddle: the huddle ended")

// Join starts or joins the huddle in channel (a channel or DM id). It
// returns after rooms.join; media connects in the background (watch
// State().Phase). Call it off the UI goroutine.
func Join(ctx context.Context, o Options, channel string) (*Session, error) {
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	ji, err := roomsJoin(ctx, o.Client, channel, o.Region)
	if err != nil {
		return nil, err
	}
	s := &Session{o: o, join: ji, muted: o.Muted, people: map[string]*Participant{}, byAtt: map[string]string{},
		streamAt: map[uint32]string{}, sources: map[uint32]streamDesc{}, sinks: map[string]*videoSink{},
		waiters: map[int][]chan *signalFrame{}, doneCh: make(chan struct{}), mic: newMicFramer()}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if o.SelfID != "" {
		s.person(o.SelfID).Self = true
	}
	s.person(o.SelfID).AttendeeID = ji.Attendee.AttendeeID
	s.byAtt[ji.Attendee.AttendeeID] = o.SelfID
	for _, u := range ji.Participants {
		s.person(u)
	}
	s.setInvites(ji.Invites)
	o.Logf("huddle: joined room %s in %s (meeting %s, region %s)", ji.RoomID, channel, ji.Meeting.MeetingID, ji.Meeting.MediaRegion)
	s.openDevice()
	go s.run()
	return s, nil
}

// person returns (creating) the participant for user id. Callers hold mu
// (or own s exclusively).
func (s *Session) person(uid string) *Participant { return s.personKey(uid, uid) }

func (s *Session) personKey(key, uid string) *Participant {
	p := s.people[key]
	if p == nil {
		p = &Participant{UserID: uid, Self: key == s.o.SelfID}
		s.people[key] = p
		s.order = append(s.order, key)
	}
	return p
}

// keyFor maps a Chime attendee to its participant key. One person in the
// huddle from two devices has two attendees and, like Slack, two tiles.
// Callers hold mu.
func (s *Session) keyFor(att, uid string) string {
	if k, ok := s.byAtt[att]; ok {
		return k
	}
	if uid == "" {
		return ""
	}
	k := uid
	if p := s.people[uid]; p != nil && p.AttendeeID != "" && p.AttendeeID != att {
		k = uid + "/" + att
	}
	s.byAtt[att] = k
	if p := s.personKey(k, uid); p.AttendeeID == "" {
		p.AttendeeID = att
	}
	return k
}

// userOf is the Slack user behind an attendee ("" if unknown). Callers hold mu.
func (s *Session) userOf(att string) string {
	if p := s.people[s.byAtt[att]]; p != nil {
		return p.UserID
	}
	return ""
}

// UserOf is the Slack user behind a Chime attendee id ("" if unknown).
func (s *Session) UserOf(att string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userOf(att)
}

// Done is closed when the session ended.
func (s *Session) Done() <-chan struct{} { return s.doneCh }

// RoomID is the huddle's room id (R…).
func (s *Session) RoomID() string { return s.join.RoomID }

// Channel is the conversation the huddle is in.
func (s *Session) Channel() string { return s.join.Channel }

// SelfAttendee is your Chime attendee id.
func (s *Session) SelfAttendee() string { return s.join.Attendee.AttendeeID }

// State returns a snapshot.
func (s *Session) State() State {
	cam := s.CameraStatus()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := State{Phase: s.phase, Err: s.err, RoomID: s.join.RoomID, Channel: s.join.Channel,
		ThreadChannel: s.join.ThreadCh, ThreadTS: s.join.ThreadTS, Name: s.join.Name,
		SelfAttendee: s.join.Attendee.AttendeeID, Muted: s.muted, Deafened: s.deafened, CaptionsOn: s.captions,
		Mic: s.micStatus, MicErr: s.micErr}
	if s.join.DateStart > 0 {
		st.Started = time.Unix(s.join.DateStart, 0)
	}
	ids := append([]string(nil), s.order...)
	sort.SliceStable(ids, func(i, j int) bool { return s.people[ids[i]].Self && !s.people[ids[j]].Self })
	for _, u := range s.inviteOrd {
		if p := s.people[u]; p != nil && (p.InMedia || p.Self) {
			continue // here: their tile, not the invite's
		}
		switch v := s.invites[u]; v {
		case "pending", "joining_soon", "declined":
			st.Invites = append(st.Invites, Invite{UserID: u, Status: v})
		}
	}
	for _, id := range ids {
		p := *s.people[id]
		if !p.InMedia && !p.Self && invited(st.Invites, p.UserID) {
			continue // rung, not here yet: the invite's tile
		}
		if p.Self {
			p.Muted = s.muted
			p.Camera = cam.State == CameraOn
		}
		st.Participants = append(st.Participants, p)
	}
	for k, v := range s.sinks {
		vid := Video{Key: k, AttendeeID: strings.TrimSuffix(k, "#content"), Content: strings.HasSuffix(k, "#content")}
		vid.UserID = s.userOf(vid.AttendeeID)
		vid.W, vid.H = v.size()
		st.Videos = append(st.Videos, vid)
	}
	sort.Slice(st.Videos, func(i, j int) bool {
		if st.Videos[i].Content != st.Videos[j].Content {
			return st.Videos[i].Content
		}
		return st.Videos[i].Key < st.Videos[j].Key
	})
	st.Captions = append(st.Captions, s.lines...)
	st.Camera = cam
	return st
}

func invited(l []Invite, u string) bool {
	for _, x := range l {
		if x.UserID == u {
			return true
		}
	}
	return false
}

// setInvites replaces the invite statuses (nil: no change). Callers hold
// mu (or own s exclusively).
func (s *Session) setInvites(m map[string]string) {
	if m == nil {
		return
	}
	keep := s.inviteOrd[:0]
	for _, u := range s.inviteOrd {
		if _, ok := m[u]; ok {
			keep = append(keep, u)
		}
	}
	s.inviteOrd = keep
	have := map[string]bool{}
	for _, u := range s.inviteOrd {
		have[u] = true
	}
	var add []string
	for u := range m {
		if !have[u] && u != s.o.SelfID {
			add = append(add, u)
		}
	}
	sort.Strings(add)
	s.inviteOrd = append(s.inviteOrd, add...)
	s.invites = m
}

// Frame returns the newest picture of a video (State().Videos[i].Key).
func (s *Session) Frame(key string) VideoFrame {
	if key == SelfVideoKey {
		return s.cam.preview.latest()
	}
	s.mu.Lock()
	v := s.sinks[key]
	s.mu.Unlock()
	if v == nil {
		return VideoFrame{}
	}
	return v.latest()
}

// changed schedules OnChange (coalesced to ~30 Hz).
func (s *Session) changed() {
	if s.o.OnChange == nil {
		return
	}
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if s.changeT != nil {
		return
	}
	s.changeT = time.AfterFunc(33*time.Millisecond, func() {
		s.changeMu.Lock()
		s.changeT = nil
		s.changeMu.Unlock()
		s.o.OnChange()
	})
}

// SetMuted mutes or unmutes your microphone.
func (s *Session) SetMuted(m bool) {
	s.mu.Lock()
	s.muted = m
	s.mu.Unlock()
	s.send(encodeMute(m))
	s.changed()
}

// SetDeafened stops (or resumes) playing the huddle's audio. Deafening
// also mutes you, like Slack.
func (s *Session) SetDeafened(d bool) {
	s.mu.Lock()
	s.deafened = d
	s.mu.Unlock()
	s.play.setMuted(d)
	if d {
		s.SetMuted(true)
	}
	s.changed()
}

// SetCaptions shows or hides live captions locally (Chime delivers the
// transcript to every attendee once someone started transcription).
func (s *Session) SetCaptions(on bool) {
	s.mu.Lock()
	s.captions = on
	s.mu.Unlock()
	s.changed()
}

// SendPeer broadcasts a peer message on topic (TopicData when "").
func (s *Session) SendPeer(topic string, m *PeerMessage) error {
	if topic == "" {
		topic = TopicData
	}
	if m.FromPeerIDString == "" {
		m.FromPeerIDString = s.join.Attendee.AttendeeID
	}
	b := m.Encode()
	if len(b) > 2048 {
		return errors.New("huddle: peer message over 2048 bytes")
	}
	return s.send(encodeData(dataMessage{Topic: topic, Data: b}))
}

// React sends an emoji reaction (by emoji name, e.g. "tada") everyone
// sees on your tile.
func (s *Session) React(emoji, url string) error {
	typ := ReacjiStandard
	if url != "" {
		typ = ReacjiGif
	}
	err := s.SendPeer(TopicData, &PeerMessage{Reacji: &Reacji{Emoji: emoji, Type: typ, EmojiURL: url}})
	if err == nil {
		s.mu.Lock()
		p := s.person(s.o.SelfID)
		p.Reaction, p.ReactedAt = emoji, time.Now()
		s.mu.Unlock()
		s.changed()
	}
	return err
}

// Leave leaves the huddle (the Chime LEAVE frame) and frees everything.
func (s *Session) Leave() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.leaving = true
	s.mu.Unlock()
	ack := s.wait(ftLeaveAck)
	if s.send(encodeLeave()) == nil {
		select {
		case <-ack:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	s.finish(nil)
}

func (s *Session) finish(err error) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.phase = Ended
	s.err = err
	s.mu.Unlock()
	s.cancel()
	s.closeCamera()
	s.closeConn()
	s.mu.Lock()
	mic := s.micDev
	s.micDev = nil
	s.mu.Unlock()
	for _, d := range []Device{s.dev, mic} {
		if d != nil {
			d.Close()
		}
	}
	s.doneOnce.Do(func() { close(s.doneCh) })
	if s.o.OnEnd != nil {
		s.o.OnEnd(err)
	}
	s.changed()
}

func (s *Session) closeConn() {
	s.wmu.Lock()
	c, pc := s.conn, s.pc
	s.conn, s.pc = nil, nil
	s.wmu.Unlock()
	if c != nil {
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	if pc != nil {
		_ = pc.Close()
	}
}

func (s *Session) send(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.conn == nil {
		return errors.New("huddle: not connected")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageBinary, b)
}

// wait registers for the next frame of type t.
func (s *Session) wait(t int) <-chan *signalFrame {
	ch := make(chan *signalFrame, 1)
	s.mu.Lock()
	s.waiters[t] = append(s.waiters[t], ch)
	s.mu.Unlock()
	return ch
}

func (s *Session) await(ch <-chan *signalFrame, d time.Duration, what string) (*signalFrame, error) {
	select {
	case f := <-ch:
		if f == nil {
			return nil, fmt.Errorf("huddle: connection closed waiting for %s", what)
		}
		return f, nil
	case <-time.After(d):
		return nil, fmt.Errorf("huddle: timed out waiting for %s", what)
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// run connects, and reconnects after drops, until the session ends.
func (s *Session) run() {
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		err := s.connect()
		if s.ctx.Err() != nil {
			return
		}
		var ce websocket.CloseError
		if errors.As(err, &ce) && ce.Code == 4410 {
			s.finish(ErrEnded)
			return
		}
		if attempt >= 4 {
			s.finish(err)
			return
		}
		s.mu.Lock()
		if s.ended || s.leaving {
			s.mu.Unlock()
			return
		}
		s.phase = Reconnecting
		s.mu.Unlock()
		s.o.Logf("huddle: connection lost: %v (reconnecting)", err)
		s.changed()
		s.closeConn()
		select {
		case <-time.After(backoff):
		case <-s.ctx.Done():
			return
		}
		backoff *= 2
	}
}

func signalingURL(base string) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "X-Chime-Control-Protocol-Version=3&X-Amzn-Chime-Send-Close-On-Error=1"
}

// connect runs one signaling + media connection until it drops.
func (s *Session) connect() error {
	ji := s.join
	dctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, signalingURL(ji.Meeting.MediaPlacement.SignalingURL), &websocket.DialOptions{
		Subprotocols: []string{"_aws_wt_session", ji.Attendee.JoinToken},
		// Chime, not Slack: this socket carries no Slack cookie or token, and
		// Chime's signaling may check Origin (Slack's web client sends this
		// one). Requests to Slack itself never send Origin (slack/ua.go).
		HTTPHeader: map[string][]string{"Origin": {"https://app.slack.com"}},
	})
	cancel()
	if err != nil {
		return fmt.Errorf("huddle: signaling: %w", err)
	}
	conn.SetReadLimit(4 << 20)
	s.wmu.Lock()
	s.conn = conn
	s.wmu.Unlock()
	readErr := make(chan error, 1)
	s.connDone = make(chan struct{})
	go func() { readErr <- s.readLoop(conn) }()

	var sid [8]byte
	_, _ = rand.Read(sid[:])
	ackCh, idxCh := s.wait(ftJoinAck), s.wait(ftIndex)
	if err := s.send(encodeJoin(binary.LittleEndian.Uint64(sid[:]) >> 1)); err != nil {
		return err
	}
	ack, err := s.await(ackCh, 10*time.Second, "JOIN_ACK")
	if err != nil {
		return s.firstErr(readErr, err)
	}
	if _, err := s.await(idxCh, 10*time.Second, "INDEX"); err != nil {
		return s.firstErr(readErr, err)
	}
	if err := s.setupMedia(ack.Turn); err != nil {
		return s.firstErr(readErr, err)
	}
	s.mu.Lock()
	s.phase = Connected
	s.mu.Unlock()
	s.changed()
	s.o.Logf("huddle: media connected (attendee %s)", ji.Attendee.AttendeeID)

	go s.sendAudio(s.connDone)
	ping := time.NewTicker(10 * time.Second)
	defer ping.Stop()
	defer close(s.connDone)
	var pid uint32
	for {
		select {
		case err := <-readErr:
			return err
		case <-ping.C:
			pid++
			_ = s.send(encodePing(pingTypePing, pid))
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
}

func (s *Session) firstErr(readErr chan error, err error) error {
	select {
	case e := <-readErr:
		if e != nil {
			return e
		}
	case <-time.After(50 * time.Millisecond):
	}
	return err
}

func (s *Session) readLoop(conn *websocket.Conn) error {
	defer func() {
		s.mu.Lock()
		for t, ws := range s.waiters {
			for _, w := range ws {
				close(w)
			}
			delete(s.waiters, t)
		}
		s.mu.Unlock()
	}()
	for {
		_, b, err := conn.Read(s.ctx)
		if err != nil {
			return err
		}
		f, err := decodeFrame(b)
		if err != nil {
			s.o.Logf("huddle: bad signaling frame: %v", err)
			continue
		}
		s.handle(f)
	}
}

func (s *Session) handle(f *signalFrame) {
	s.mu.Lock()
	ws := s.waiters[f.Type]
	delete(s.waiters, f.Type)
	s.mu.Unlock()
	for _, w := range ws {
		w <- f
	}
	if f.ErrStatus != 0 || f.ErrDesc != "" {
		s.o.Logf("huddle: signaling error %d: %s (frame %d)", f.ErrStatus, f.ErrDesc, f.Type)
	}
	switch f.Type {
	case ftPingPong:
		if f.PingType == pingTypePing {
			go s.send(encodePing(pingTypePong, f.PingID))
		}
	case ftIndex:
		s.onIndex(f.Index)
	case ftAudioStreamIDInfo:
		s.onAudioInfo(f.AudioInfo)
	case ftAudioMetadata:
		s.onAudioMeta(f.AudioStates)
	case ftDataMessage:
		for _, m := range f.Data {
			s.onData(m)
		}
	case ftNotification:
		s.o.Logf("huddle: chime notice (%d): %s", f.NoticeLevel, f.Notice)
	case ftAudioStatus:
		if f.AudioStatus != 0 && f.AudioStatus != 200 {
			s.o.Logf("huddle: audio status %d", f.AudioStatus)
		}
	}
}

// ---- media ----

// testSettings lets tests adjust the ICE setup (loopback candidates).
var testSettings func(*webrtc.SettingEngine)

var h264Fmtp = []struct {
	pt   uint8
	fmtp string
}{
	{102, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f"},
	{106, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"},
	{127, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d001f"},
	{112, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=64001f"},
}

func newAPI() (*webrtc.API, error) {
	me := &webrtc.MediaEngine{}
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	fb := []webrtc.RTCPFeedback{{Type: "goog-remb"}, {Type: "transport-cc"}, {Type: "ccm", Parameter: "fir"},
		{Type: "nack"}, {Type: "nack", Parameter: "pli"}}
	for _, c := range h264Fmtp {
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: c.fmtp, RTCPFeedback: fb},
			PayloadType: webrtc.PayloadType(c.pt)}, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(me, ir); err != nil {
		return nil, err
	}
	se := webrtc.SettingEngine{}
	// Chime's audio server only speaks AES-CM: with GCM it drops you.
	se.SetSRTPProtectionProfiles(dtls.SRTP_AES128_CM_HMAC_SHA1_80)
	if testSettings != nil {
		testSettings(&se)
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(se)), nil
}

func (s *Session) setupMedia(turn *turnCreds) error {
	api, err := newAPI()
	if err != nil {
		return err
	}
	cfg := webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle, SDPSemantics: webrtc.SDPSemanticsUnifiedPlan}
	if turn != nil && len(turn.URIs) > 0 {
		cfg.ICEServers = []webrtc.ICEServer{{URLs: turn.URIs, Username: turn.Username, Credential: turn.Password}}
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return err
	}
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "clack")
	if err != nil {
		_ = pc.Close()
		return err
	}
	if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
		_ = pc.Close()
		return err
	}
	// The first video section is the local camera: recvonly until
	// SetCamera(true) gives it the camera track (sendrecv).
	camT, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		_ = pc.Close()
		return err
	}
	s.cameraAttach(api, pc, camT)
	pc.OnTrack(s.onTrack)
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		s.o.Logf("huddle: peer connection %s", st)
	})
	s.wmu.Lock()
	s.pc, s.audio = pc, track
	s.wmu.Unlock()
	s.negMu.Lock()
	s.slots = []uint32{0}
	s.ssrcOf = map[uint32]uint32{}
	s.slotSSRC = map[int]uint32{}
	s.negMu.Unlock()
	if err := s.subscribe(true); err != nil {
		return err
	}
	// Ask for videos the first INDEX already listed.
	go s.updateVideos()
	return nil
}

// subscribe (re)negotiates: offer → SUBSCRIBE → SUBSCRIBE_ACK → answer.
func (s *Session) subscribe(first bool) error {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	s.wmu.Lock()
	pc := s.pc
	s.wmu.Unlock()
	if pc == nil {
		return errors.New("huddle: no peer connection")
	}
	// one recvonly transceiver per wanted stream
	for n := countVideo(pc); n < len(s.slots); n++ {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			return err
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	select {
	case <-gathered:
	case <-time.After(10 * time.Second):
		s.o.Logf("huddle: ICE gathering slow; sending what we have")
	}
	sdp := strings.Replace(pc.LocalDescription().SDP, "o=- ", "o=mozilla-chrome ", 1)
	s.mu.Lock()
	muted := s.muted
	s.mu.Unlock()
	ackCh := s.wait(ftSubscribeAck)
	if err := s.send(encodeSubscribe(subscribeReq{SDP: sdp, AudioHost: s.join.Meeting.MediaPlacement.AudioHostURL,
		AudioMuted: muted, AttendeeID: s.join.Attendee.AttendeeID, Receive: append([]uint32(nil), s.slots...),
		Video: s.cameraDesc()})); err != nil {
		return err
	}
	ack, err := s.await(ackCh, 15*time.Second, "SUBSCRIBE_ACK")
	if err != nil {
		return err
	}
	if ack.SDPAnswer == "" {
		return errors.New("huddle: SUBSCRIBE_ACK without an SDP answer")
	}
	for _, t := range ack.Tracks {
		if t.SSRC != 0 {
			s.ssrcOf[t.SSRC] = t.StreamID
		}
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: ack.SDPAnswer}); err != nil {
		return fmt.Errorf("huddle: answer: %w", err)
	}
	return nil
}

func countVideo(pc *webrtc.PeerConnection) int {
	n := 0
	for _, t := range pc.GetTransceivers() {
		if t.Kind() == webrtc.RTPCodecTypeVideo {
			n++
		}
	}
	return n
}

// sendAudio sends a 20 ms Opus frame every 20 ms (wall clock): the
// microphone when unmuted, else silence. Chime only counts you as present
// while your audio flows.
func (s *Session) sendAudio(done <-chan struct{}) {
	enc, err := newOpusEnc()
	if err != nil {
		s.o.Logf("huddle: opus encoder: %v", err)
		return
	}
	silence := make([]int16, frameSamples)
	buf := make([]byte, 0, 1500)
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	var lvlAt time.Time
	for {
		select {
		case <-done:
			return
		case now := <-t.C:
			s.checkMic(now)
		}
		pcm := silence
		select {
		case f := <-s.mic.out:
			pcm = f
			for len(s.mic.out) > 3 { // drift: stay near real time
				pcm = <-s.mic.out
			}
		default:
		}
		s.mu.Lock()
		muted := s.muted
		s.mu.Unlock()
		if muted {
			pcm = silence
		} else if lv := level(pcm); lv > 0.02 && time.Since(lvlAt) > 100*time.Millisecond {
			lvlAt = time.Now()
			s.mu.Lock()
			s.person(s.o.SelfID).SpokeAt = lvlAt
			s.mu.Unlock()
			s.changed()
		}
		pkt, err := enc.encode(pcm, buf)
		if err != nil {
			continue
		}
		s.wmu.Lock()
		tr := s.audio
		s.wmu.Unlock()
		if tr != nil && tr.WriteSample(media.Sample{Data: pkt, Duration: 20 * time.Millisecond}) == nil {
			s.mu.Lock()
			s.stats.audioOut++
			s.mu.Unlock()
		}
	}
}

func (s *Session) onTrack(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
	s.o.Logf("huddle: remote %s track ssrc %d codec %s", track.Kind(), track.SSRC(), track.Codec().MimeType)
	done := s.connDone
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		go s.recvAudio(track, done)
		return
	}
	s.negMu.Lock()
	sid, ok := s.ssrcOf[uint32(track.SSRC())]
	if !ok { // fall back to the transceiver's slot
		s.wmu.Lock()
		pc := s.pc
		s.wmu.Unlock()
		if pc != nil {
			i := 0
			for _, t := range pc.GetTransceivers() {
				if t.Kind() != webrtc.RTPCodecTypeVideo {
					continue
				}
				if t.Receiver() == recv && i < len(s.slots) {
					sid = s.slots[i]
				}
				i++
			}
		}
	}
	s.negMu.Unlock()
	s.mu.Lock()
	src, known := s.sources[sid]
	s.mu.Unlock()
	key := src.AttendeeID
	if !known || key == "" {
		key = "ssrc:" + strconv.FormatUint(uint64(track.SSRC()), 10)
	}
	s.wmu.Lock()
	pc := s.pc
	s.wmu.Unlock()
	v := &videoSink{key: key, ssrc: uint32(track.SSRC()), pc: pc, notify: s.changed, logf: s.o.Logf}
	s.mu.Lock()
	s.sinks[key] = v
	s.mu.Unlock()
	s.changed()
	go func() {
		v.run(track, done)
		s.mu.Lock()
		if s.sinks[key] == v {
			delete(s.sinks, key)
		}
		s.mu.Unlock()
		s.changed()
	}()
}

func (s *Session) recvAudio(track *webrtc.TrackRemote, done <-chan struct{}) {
	dec, err := newOpusDec()
	if err != nil {
		s.o.Logf("huddle: opus decoder: %v", err)
		return
	}
	var last uint16
	var have bool
	var peak float64
	logAt := time.Now()
	for {
		select {
		case <-done:
			return
		default:
		}
		_ = track.SetReadDeadline(time.Now().Add(2 * time.Second))
		pkt, _, err := track.ReadRTP()
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return
		}
		if have {
			gap := pkt.SequenceNumber - last - 1
			if gap > 0 && gap < 5 {
				for i := uint16(0); i < gap; i++ {
					if pcm, err := dec.decode(nil); err == nil {
						s.play.push(pcm)
					}
				}
				s.mu.Lock()
				s.stats.audioLost += int64(gap)
				s.mu.Unlock()
			} else if gap >= 0x8000 { // late/reordered: drop
				continue
			}
		}
		last, have = pkt.SequenceNumber, true
		if len(pkt.Payload) == 0 {
			continue
		}
		pcm, err := dec.decode(pkt.Payload)
		if err != nil {
			if n := opusAborts.Load(); n == 1 || n%500 == 0 {
				s.o.Logf("huddle: %v (%d so far)", err, n)
			}
			continue
		}
		s.play.push(pcm)
		lv := level(pcm)
		if lv > peak {
			peak = lv
		}
		s.mu.Lock()
		s.stats.audioIn++
		s.stats.inLevel = lv
		s.mu.Unlock()
		if debugLog && time.Since(logAt) > 3*time.Second {
			s.o.Logf("huddle: audio in: %d packets, peak level %.3f", s.stats.audioIn, peak)
			logAt, peak = time.Now(), 0
		}
	}
}

var debugLog = os.Getenv("CLACK_HUDDLE_LOG") != ""

// Stats is a debug summary.
func (s *Session) Stats() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var vids []string
	for k, v := range s.sinks {
		vids = append(vids, fmt.Sprintf("%s: %d pkts %d frames", k, v.packets.Load(), v.decoded.Load()))
	}
	sort.Strings(vids)
	return fmt.Sprintf("audio out %d, in %d (lost %d, level %.3f); video [%s]; camera out %d", s.stats.audioOut, s.stats.audioIn,
		s.stats.audioLost, s.stats.inLevel, strings.Join(vids, "; "), s.cam.sent.Load())
}

// ---- signaling state ----

func (s *Session) onIndex(ix *indexFrame) {
	if ix == nil {
		return
	}
	s.mu.Lock()
	s.sources = map[uint32]streamDesc{}
	cams, shares := map[string]bool{}, map[string]bool{}
	for _, src := range ix.Sources {
		s.sources[src.StreamID] = src
		att := strings.TrimSuffix(src.AttendeeID, "#content")
		if u := userOfExternal(src.ExternalUserID); u != "" && att != "" {
			s.keyFor(att, u)
		}
		if strings.HasSuffix(src.AttendeeID, "#content") {
			shares[att] = true
		} else {
			cams[att] = true
		}
	}
	for _, p := range s.people {
		p.Camera, p.Sharing = cams[p.AttendeeID], shares[p.AttendeeID]
	}
	s.mu.Unlock()
	s.changed()
	go s.updateVideos()
}

// wantedStreams picks one stream per remote video source: the best layer
// of a screen share, a mid layer of a camera.
func (s *Session) wantedStreams() map[uint32]uint32 { // group -> stream
	s.mu.Lock()
	defer s.mu.Unlock()
	self := s.join.Attendee.AttendeeID
	groups := map[uint32][]streamDesc{}
	for _, src := range s.sources {
		if strings.TrimSuffix(src.AttendeeID, "#content") == self || src.MediaType == mediaAudio {
			continue
		}
		groups[src.GroupID] = append(groups[src.GroupID], src)
	}
	out := map[uint32]uint32{}
	for g, l := range groups {
		sort.Slice(l, func(i, j int) bool { return l[i].MaxBitrateKbps > l[j].MaxBitrateKbps })
		pick := l[0]
		if !strings.HasSuffix(pick.AttendeeID, "#content") {
			for _, c := range l { // cameras: the best layer at or under 900 kbps
				if c.MaxBitrateKbps <= 900 {
					pick = c
					break
				}
			}
		}
		out[g] = pick.StreamID
	}
	return out
}

// updateVideos resubscribes when the set of remote videos changed.
func (s *Session) updateVideos() {
	want := s.wantedStreams()
	s.negMu.Lock()
	if s.slots == nil {
		s.negMu.Unlock()
		return
	}
	s.mu.Lock()
	groupOf := func(sid uint32) (uint32, bool) {
		src, ok := s.sources[sid]
		return src.GroupID, ok
	}
	have := map[uint32]bool{}
	changed := false
	for i := 1; i < len(s.slots); i++ {
		sid := s.slots[i]
		if sid == 0 {
			continue
		}
		g, ok := groupOf(sid)
		if !ok || want[g] == 0 {
			s.slots[i] = 0 // source gone
			changed = true
			continue
		}
		have[g] = true
	}
	for g, sid := range want {
		if have[g] {
			continue
		}
		s.slots = append(s.slots, sid) // a fresh m-section per new source
		changed = true
	}
	s.mu.Unlock()
	s.negMu.Unlock()
	if !changed {
		return
	}
	if err := s.subscribe(false); err != nil {
		s.o.Logf("huddle: video resubscribe: %v", err)
	}
}

func (s *Session) onAudioInfo(info []audioStreamInfo) {
	s.mu.Lock()
	for _, st := range info {
		switch {
		case st.AttendeeID != "":
			s.streamAt[st.StreamID] = st.AttendeeID
			k := s.keyFor(st.AttendeeID, userOfExternal(st.ExternalUserID))
			if k == "" {
				continue
			}
			p := s.people[k]
			p.InMedia = true
			if st.HasMuted && !p.Self {
				p.Muted = st.Muted
			}
		case st.HasMuted:
			if uid := s.byAtt[s.streamAt[st.StreamID]]; uid != "" {
				if p := s.people[uid]; p != nil && !p.Self {
					p.Muted = st.Muted
				}
			}
		default: // the stream went away
			att := s.streamAt[st.StreamID]
			delete(s.streamAt, st.StreamID)
			if k := s.byAtt[att]; k != "" {
				if p := s.people[k]; p != nil {
					p.InMedia = false
					if strings.Contains(k, "/") { // a second device left: drop its tile
						delete(s.people, k)
						delete(s.byAtt, att)
						for i, o := range s.order {
							if o == k {
								s.order = append(s.order[:i], s.order[i+1:]...)
								break
							}
						}
					}
				}
			}
		}
	}
	s.mu.Unlock()
	s.changed()
}

// Chime reports volume as -dBFS; the SDK maps -42..-14 dB onto 0..1.
func normVolume(v uint32) float64 {
	db := -float64(v)
	n := 1 - (db-(-14))/(-42-(-14))
	return max(0, min(1, n))
}

func (s *Session) onAudioMeta(states []audioState) {
	now := time.Now()
	s.mu.Lock()
	dirty := false
	for _, st := range states {
		uid := s.byAtt[s.streamAt[st.StreamID]]
		p := s.people[uid]
		if p == nil {
			continue
		}
		if st.HasVolume {
			p.Volume = normVolume(st.Volume)
			if p.Volume > 0.2 {
				p.SpokeAt = now
			}
			dirty = true
		}
		if st.HasMuted && !p.Self {
			p.Muted = st.Muted
			dirty = true
		}
	}
	s.mu.Unlock()
	if dirty {
		s.changed()
	}
}

func (s *Session) onData(m dataMessage) {
	self := s.join.Attendee.AttendeeID
	switch m.Topic {
	case TopicTranscript:
		s.onTranscript(m.Data)
		return
	case TopicData, TopicAwareness, TopicCatchUp:
	default:
		return
	}
	if m.SenderAttendee == self {
		return
	}
	pm, err := DecodePeerMessage(m.Data)
	if err != nil {
		return
	}
	if s.o.OnPeer != nil {
		s.o.OnPeer(PeerEvent{Topic: m.Topic, Message: pm, Kind: pm.Kind(), SenderAttendeeID: m.SenderAttendee, SelfAttendeeID: self})
	}
	if m.Topic != TopicData {
		return
	}
	s.mu.Lock()
	key := s.byAtt[m.SenderAttendee]
	known := key != ""
	uid := s.userOf(m.SenderAttendee)
	s.mu.Unlock()
	switch {
	case pm.Reacji != nil && pm.Reacji.Emoji != "" && known:
		s.mu.Lock()
		if p := s.people[key]; p != nil {
			p.Reaction, p.ReactedAt = pm.Reacji.Emoji, time.Now()
		}
		s.mu.Unlock()
		s.changed()
	case pm.MuteRequest != nil && pm.MuteRequest.PeerID == self && known:
		s.o.Logf("huddle: %s muted you", uid)
		s.SetMuted(true)
	case pm.KickNotification != nil && pm.KickNotification.KickedUserID == s.o.SelfID && known:
		s.o.Logf("huddle: %s removed you from the huddle", uid)
		go s.Leave()
	}
}

func (s *Session) onTranscript(b []byte) {
	res, status := decodeTranscript(b)
	if status != "" {
		s.o.Logf("huddle: transcription %s", status)
	}
	if len(res) == 0 {
		return
	}
	now := time.Now()
	s.mu.Lock()
	for _, r := range res {
		if r.Text == "" {
			continue
		}
		uid := s.userOf(r.Speaker)
		if uid == "" {
			uid = userOfExternal(r.SpeakerEx)
		}
		c := Caption{ID: r.ResultID, UserID: uid, Text: r.Text, Partial: r.Partial, At: now}
		replaced := false
		for i := range s.lines {
			if s.lines[i].ID == c.ID && c.ID != "" {
				s.lines[i] = c
				replaced = true
			}
		}
		if !replaced {
			s.lines = append(s.lines, c)
		}
	}
	if len(s.lines) > 6 {
		s.lines = append(s.lines[:0], s.lines[len(s.lines)-6:]...)
	}
	s.mu.Unlock()
	s.changed()
}

// ---- Slack room events ----

// HandleRoomEvent applies a realtime sh_room_join / sh_room_leave /
// sh_room_update (core's "huddle" event: name and raw JSON).
func (s *Session) HandleRoomEvent(name string, raw json.RawMessage) {
	var ev struct {
		User   string   `json:"user"`
		Room   roomJSON `json:"room"`
		Huddle roomJSON `json:"huddle"`
		CallID string   `json:"call_id"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	r := ev.Room
	if r.ID == "" {
		r = ev.Huddle
	}
	id := firstNonEmpty(r.ID, ev.CallID)
	if id != s.join.RoomID {
		return
	}
	s.mu.Lock()
	if r.ThreadRootTS != "" && s.join.ThreadTS == "" {
		s.join.ThreadTS = r.ThreadRootTS
	}
	if r.Name != "" {
		s.join.Name = r.Name
	}
	s.setInvites(r.invites())
	if r.Participants != nil {
		in := map[string]bool{}
		for _, u := range r.Participants {
			in[u] = true
			s.person(u)
		}
		keep := s.order[:0]
		for _, u := range s.order {
			if in[u] || s.people[u].Self || s.people[u].InMedia {
				keep = append(keep, u)
			} else {
				delete(s.people, u)
			}
		}
		s.order = keep
	}
	switch name {
	case "sh_room_join":
		if ev.User != "" {
			s.person(ev.User)
			delete(s.invites, ev.User) // Slack clears a joiner's invite status
		}
	case "sh_room_leave":
		if ev.User != "" && ev.User != s.o.SelfID {
			if p := s.people[ev.User]; p != nil && !p.InMedia {
				delete(s.people, ev.User)
				for i, u := range s.order {
					if u == ev.User {
						s.order = append(s.order[:i], s.order[i+1:]...)
						break
					}
				}
			}
		}
	}
	ended := r.HasEnded || r.DateEnd > 0
	s.mu.Unlock()
	s.changed()
	if ended {
		go s.finish(ErrEnded)
	}
}
