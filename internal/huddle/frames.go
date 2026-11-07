package huddle

// Chime signaling frames (amazon-chime-sdk-js protocol/SignalingProtocol.proto,
// proto2). Only the fields Clack sends or reads are modelled.

import "time"

// SdkSignalFrame.Type
const (
	ftJoin              = 1
	ftJoinAck           = 2
	ftSubscribe         = 3
	ftSubscribeAck      = 4
	ftIndex             = 5
	ftPause             = 7
	ftResume            = 8
	ftLeave             = 9
	ftLeaveAck          = 10
	ftBitrates          = 13
	ftAudioControl      = 16
	ftAudioMetadata     = 17
	ftAudioStreamIDInfo = 18
	ftPingPong          = 19
	ftAudioStatus       = 20
	ftClientMetric      = 21
	ftDataMessage       = 22
	ftRemoteVideoUpdate = 24
	ftNotification      = 34
)

const (
	joinFlagHasStreamUpdate = 2

	streamRX     = 1
	mediaAudio   = 1
	mediaVideo   = 2
	pingTypePing = 1
	pingTypePong = 2

	// frameTypeRTC prefixes every websocket message.
	frameTypeRTC = 0x05
)

type turnCreds struct {
	Username, Password string
	TTL                uint32
	URIs               []string
}

type streamDesc struct {
	StreamID       uint32
	Framerate      uint32
	MaxBitrateKbps uint32
	TrackLabel     string
	GroupID        uint32
	AvgBitrateBps  uint32
	AttendeeID     string
	MediaType      uint32
	ExternalUserID string
	Width, Height  uint32
}

type trackMapping struct {
	StreamID uint32
	SSRC     uint32
	Label    string
}

type indexFrame struct {
	AtCapacity      bool
	Sources         []streamDesc
	Paused          []uint32
	NumParticipants uint32
}

type audioState struct {
	StreamID       uint32
	Volume         uint32
	Muted          bool
	HasVolume      bool
	HasMuted       bool
	SignalStrength uint32
}

type audioStreamInfo struct {
	StreamID       uint32
	AttendeeID     string
	Muted          bool
	HasMuted       bool
	ExternalUserID string
	Dropped        bool
}

type dataMessage struct {
	Topic          string
	Data           []byte
	LifetimeMS     uint32
	SenderAttendee string
	SenderExternal string
	IngestNS       int64
}

// signalFrame is a decoded SdkSignalFrame.
type signalFrame struct {
	Type        int
	TimestampMS uint64

	ErrStatus uint32
	ErrDesc   string

	Turn *turnCreds // JOIN_ACK

	SDPAnswer string // SUBSCRIBE_ACK
	Tracks    []trackMapping

	Index *indexFrame

	AudioStates []audioState
	AudioInfo   []audioStreamInfo

	PingType uint32
	PingID   uint32

	AudioStatus uint32

	Data []dataMessage

	NoticeLevel uint32
	Notice      string
}

func frameHeader(w *pbw, typ int) {
	w.uint(1, uint64(time.Now().UnixMilli()))
	w.uint(2, uint64(typ))
}

func wrapRTC(b []byte) []byte { return append([]byte{frameTypeRTC}, b...) }

// clientDetails mimic a current Chrome running the Chime JS SDK, as Slack's
// desktop app does.
func encodeJoin(audioSessionID uint64) []byte {
	var w pbw
	frameHeader(&w, ftJoin)
	w.msg(4, func(w *pbw) {
		w.uint(1, 2) // protocol_version
		w.uint(3, joinFlagHasStreamUpdate)
		w.msg(4, func(w *pbw) {
			w.str(5, "Chrome")              // platform_name
			w.str(6, "138")                 // platform_version
			w.str(7, "amazon-chime-sdk-js") // client_source
			w.str(8, "3.29.0")              // chime_sdk_version
			w.str(9, utcOffset())           // client_utc_offset
		})
		w.uint(6, audioSessionID)
		w.boolean(7, false) // wants_compressed_sdp
	})
	return wrapRTC(w.b)
}

func utcOffset() string {
	_, off := time.Now().Zone()
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	h, m := off/3600, (off%3600)/60
	return sign + two(h) + ":" + two(m)
}

func two(n int) string { return string([]byte{byte('0' + n/10%10), byte('0' + n%10)}) }

type subscribeReq struct {
	SDP        string
	AudioHost  string
	AudioMuted bool
	AttendeeID string
	Receive    []uint32 // one per video m-section, in SDP order (0 = none)
}

func encodeSubscribe(s subscribeReq) []byte {
	var w pbw
	frameHeader(&w, ftSubscribe)
	w.msg(6, func(w *pbw) {
		w.uint(1, streamRX)     // duplex
		w.msg(2, func(w *pbw) { // the audio send stream
			w.uint(1, 1)   // stream_id
			w.uint(2, 15)  // framerate
			w.uint(3, 600) // max_bitrate_kbps
			w.str(4, "AmazonChimeExpressAudio")
			w.uint(6, 1)      // group_id
			w.uint(7, 400000) // avg_bitrate_bps
			w.str(8, s.AttendeeID)
			w.uint(9, mediaAudio)
		})
		for _, id := range s.Receive {
			w.uint(3, uint64(id))
		}
		w.str(4, s.SDP)
		w.str(5, s.AudioHost)
		w.boolean(6, false) // audio_checkin
		w.boolean(7, s.AudioMuted)
	})
	return wrapRTC(w.b)
}

func encodeLeave() []byte {
	var w pbw
	frameHeader(&w, ftLeave)
	w.msg(11, func(w *pbw) {})
	return wrapRTC(w.b)
}

func encodeMute(muted bool) []byte {
	var w pbw
	frameHeader(&w, ftAudioControl)
	w.msg(17, func(w *pbw) { w.boolean(1, muted) })
	return wrapRTC(w.b)
}

func encodePing(typ, id uint32) []byte {
	var w pbw
	frameHeader(&w, ftPingPong)
	w.msg(20, func(w *pbw) {
		w.uint(1, uint64(typ))
		w.uint(2, uint64(id))
	})
	return wrapRTC(w.b)
}

func encodePause(resume bool, ids []uint32) []byte {
	var w pbw
	t := ftPause
	if resume {
		t = ftResume
	}
	frameHeader(&w, t)
	w.msg(10, func(w *pbw) {
		for _, id := range ids {
			w.uint(1, uint64(id))
		}
	})
	return wrapRTC(w.b)
}

func encodeData(msgs ...dataMessage) []byte {
	var w pbw
	frameHeader(&w, ftDataMessage)
	w.msg(23, func(w *pbw) {
		for _, m := range msgs {
			w.msg(1, func(w *pbw) {
				w.str(1, m.Topic)
				w.bytes(2, m.Data)
				w.uint(3, uint64(m.LifetimeMS))
			})
		}
	})
	return wrapRTC(w.b)
}

// decodeFrame parses one websocket message (with its 1-byte frame type).
func decodeFrame(b []byte) (*signalFrame, error) {
	if len(b) < 1 {
		return nil, errPB
	}
	b = b[1:]
	f := &signalFrame{}
	err := pbEach(b, func(fd pbField) error {
		switch fd.num {
		case 1:
			f.TimestampMS = fd.v
		case 2:
			f.Type = int(fd.v)
		case 3: // error
			return pbEach(fd.b, func(e pbField) error {
				switch e.num {
				case 1:
					f.ErrStatus = e.u32()
				case 2:
					f.ErrDesc = e.str()
				}
				return nil
			})
		case 5: // joinack
			return pbEach(fd.b, func(e pbField) error {
				if e.num == 1 {
					t := &turnCreds{}
					f.Turn = t
					return pbEach(e.b, func(c pbField) error {
						switch c.num {
						case 1:
							t.Username = c.str()
						case 2:
							t.Password = c.str()
						case 3:
							t.TTL = c.u32()
						case 4:
							t.URIs = append(t.URIs, c.str())
						}
						return nil
					})
				}
				return nil
			})
		case 7: // suback
			return pbEach(fd.b, func(e pbField) error {
				switch e.num {
				case 3:
					f.SDPAnswer = e.str()
				case 4:
					var t trackMapping
					_ = pbEach(e.b, func(c pbField) error {
						switch c.num {
						case 1:
							t.StreamID = c.u32()
						case 2:
							t.SSRC = c.u32()
						case 3:
							t.Label = c.str()
						}
						return nil
					})
					f.Tracks = append(f.Tracks, t)
				}
				return nil
			})
		case 8: // index
			ix := &indexFrame{}
			f.Index = ix
			return pbEach(fd.b, func(e pbField) error {
				switch e.num {
				case 1:
					ix.AtCapacity = e.boolean()
				case 2:
					ix.Sources = append(ix.Sources, decodeStream(e.b))
				case 3:
					ix.Paused = pbUints(ix.Paused, e)
				case 4:
					ix.NumParticipants = e.u32()
				}
				return nil
			})
		case 18: // audio_metadata
			return pbEach(fd.b, func(e pbField) error {
				if e.num != 1 {
					return nil
				}
				var s audioState
				_ = pbEach(e.b, func(c pbField) error {
					switch c.num {
					case 1:
						s.StreamID = c.u32()
					case 2:
						s.Volume, s.HasVolume = c.u32(), true
					case 3:
						s.Muted, s.HasMuted = c.boolean(), true
					case 4:
						s.SignalStrength = c.u32()
					}
					return nil
				})
				f.AudioStates = append(f.AudioStates, s)
				return nil
			})
		case 19: // audio_stream_id_info
			return pbEach(fd.b, func(e pbField) error {
				if e.num != 1 {
					return nil
				}
				var s audioStreamInfo
				_ = pbEach(e.b, func(c pbField) error {
					switch c.num {
					case 1:
						s.StreamID = c.u32()
					case 2:
						s.AttendeeID = c.str()
					case 3:
						s.Muted, s.HasMuted = c.boolean(), true
					case 4:
						s.ExternalUserID = c.str()
					case 5:
						s.Dropped = c.boolean()
					}
					return nil
				})
				f.AudioInfo = append(f.AudioInfo, s)
				return nil
			})
		case 20: // ping_pong
			return pbEach(fd.b, func(e pbField) error {
				switch e.num {
				case 1:
					f.PingType = e.u32()
				case 2:
					f.PingID = e.u32()
				}
				return nil
			})
		case 21:
			return pbEach(fd.b, func(e pbField) error {
				if e.num == 1 {
					f.AudioStatus = e.u32()
				}
				return nil
			})
		case 23: // data_message
			return pbEach(fd.b, func(e pbField) error {
				if e.num != 1 {
					return nil
				}
				var m dataMessage
				_ = pbEach(e.b, func(c pbField) error {
					switch c.num {
					case 1:
						m.Topic = c.str()
					case 2:
						m.Data = c.clone()
					case 3:
						m.LifetimeMS = c.u32()
					case 4:
						m.SenderAttendee = c.str()
					case 5:
						m.IngestNS = c.i64()
					case 6:
						m.SenderExternal = c.str()
					}
					return nil
				})
				f.Data = append(f.Data, m)
				return nil
			})
		case 35: // notification
			return pbEach(fd.b, func(e pbField) error {
				switch e.num {
				case 1:
					f.NoticeLevel = e.u32()
				case 2:
					f.Notice = e.str()
				}
				return nil
			})
		}
		return nil
	})
	return f, err
}

func decodeStream(b []byte) streamDesc {
	var s streamDesc
	_ = pbEach(b, func(c pbField) error {
		switch c.num {
		case 1:
			s.StreamID = c.u32()
		case 2:
			s.Framerate = c.u32()
		case 3:
			s.MaxBitrateKbps = c.u32()
		case 4:
			s.TrackLabel = c.str()
		case 6:
			s.GroupID = c.u32()
		case 7:
			s.AvgBitrateBps = c.u32()
		case 8:
			s.AttendeeID = c.str()
		case 9:
			s.MediaType = c.u32()
		case 10:
			s.ExternalUserID = c.str()
		case 11:
			s.Width = c.u32()
		case 12:
			s.Height = c.u32()
		}
		return nil
	})
	return s
}
