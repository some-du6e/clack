package huddle

// Slack's huddle peer messages (calls/client/peer_message.proto, proto3),
// sent as Chime data messages. JSON field names follow protobuf-es, which is
// what Slack's client (and so Taut's api.huddles.onPeerMessage) hands around.

// Data-message topics.
const (
	TopicData       = "data-channel"
	TopicAwareness  = "awareness-sync-channel"
	TopicCatchUp    = "transcript-catchup"
	TopicTranscript = "aws:chime:transcription"
)

// Reacji types.
const (
	ReacjiEffect   = 1
	ReacjiStandard = 2
	ReacjiGif      = 3
)

type Reacji struct {
	Emoji    string `json:"emoji,omitempty"`
	Type     int    `json:"type,omitempty"`
	EmojiURL string `json:"emojiUrl,omitempty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Segment struct {
	A Point `json:"a"`
	B Point `json:"b"`
	C Point `json:"c"`
	D Point `json:"d"`
}

// Draw types.
const (
	DrawNew       = 1
	DrawEdit      = 2
	DrawEnd       = 3
	DrawChangeHue = 4
	DrawPosition  = 5
)

type Draw struct {
	Type    int      `json:"type,omitempty"`
	Segment *Segment `json:"segment,omitempty"`
	Hue     int32    `json:"hue,omitempty"`
	ItemID  string   `json:"itemId,omitempty"`
	X       float64  `json:"x,omitempty"`
	Y       float64  `json:"y,omitempty"`
}

type Capabilities struct {
	IsDrawEnabled bool `json:"isDrawEnabled"`
}

type ContentShareDetails struct {
	SourceType string `json:"sourceType,omitempty"`
	ItemID     string `json:"itemId,omitempty"`
}

type MuteRequest struct {
	PeerID string `json:"peerId,omitempty"`
}

type KickNotification struct {
	KickedUserID string `json:"kickedUserId,omitempty"`
}

type JukeboxRequest struct {
	SongKey string `json:"songKey,omitempty"`
	Stop    bool   `json:"stop,omitempty"`
}

type Background struct {
	URL string `json:"url,omitempty"`
}

type RecordingStart struct {
	Type          int    `json:"type,omitempty"`
	RecordingType string `json:"recordingType,omitempty"`
}

type TranscriptLine struct {
	UserID      string `json:"userId,omitempty"`
	Content     string `json:"content,omitempty"`
	StartTimeMS uint64 `json:"startTimeMs,omitempty"`
	EndTimeMS   uint64 `json:"endTimeMs,omitempty"`
}

type TranscriptCatchUp struct {
	Type                          int              `json:"type"`
	Lines                         []TranscriptLine `json:"lines,omitempty"`
	ChunkIndex                    uint32           `json:"chunkIndex,omitempty"`
	TotalChunks                   uint32           `json:"totalChunks,omitempty"`
	RequestID                     string           `json:"requestId,omitempty"`
	EarliestTranscriptTimestampMS uint64           `json:"earliestTranscriptTimestampMs,omitempty"`
}

type AwarenessPayload struct {
	FieldName        string `json:"fieldName,omitempty"`
	FieldValue       string `json:"fieldValue,omitempty"`
	FieldTimestampMS *int64 `json:"fieldTimestampMs,omitempty"`
}

type Awareness struct {
	Channel string             `json:"channel,omitempty"`
	User    string             `json:"user,omitempty"`
	State   []AwarenessPayload `json:"awarenessState,omitempty"`
}

// PeerMessage is one decoded peer message; usually one field is set.
type PeerMessage struct {
	Reacji                     *Reacji              `json:"reacji,omitempty"`
	Draw                       *Draw                `json:"draw,omitempty"`
	FromPeerID                 uint64               `json:"fromPeerId,omitempty"`
	FromPeerIDString           string               `json:"fromPeerIdString,omitempty"`
	Capabilities               *Capabilities        `json:"capabilities,omitempty"`
	Background                 *Background          `json:"background,omitempty"`
	MuteRequest                *MuteRequest         `json:"muteRequest,omitempty"`
	AwarenessBroadcast         *Awareness           `json:"awarenessBroadcast,omitempty"`
	AwarenessQuery             *Awareness           `json:"awarenessQuery,omitempty"`
	AwarenessClose             *Awareness           `json:"awarenessClose,omitempty"`
	JukeboxRequest             *JukeboxRequest      `json:"jukeboxRequest,omitempty"`
	ContentShareDetails        *ContentShareDetails `json:"contentShareDetails,omitempty"`
	RecordingStartNotification *RecordingStart      `json:"recordingStartNotification,omitempty"`
	TranscriptCatchUp          *TranscriptCatchUp   `json:"transcriptCatchUp,omitempty"`
	KickNotification           *KickNotification    `json:"kickNotification,omitempty"`
}

// Kind is the field Slack's client acts on, in its if/else order.
func (m *PeerMessage) Kind() string {
	switch {
	case m.JukeboxRequest != nil:
		return "jukeboxRequest"
	case m.Reacji != nil:
		return "reacji"
	case m.Draw != nil:
		return "draw"
	case m.Capabilities != nil:
		return "capabilities"
	case m.MuteRequest != nil:
		return "muteRequest"
	case m.ContentShareDetails != nil:
		return "contentShareDetails"
	case m.KickNotification != nil:
		return "kickNotification"
	}
	return ""
}

// PeerEvent is a peer message as Taut's api.huddles.onPeerMessage passes it.
type PeerEvent struct {
	Topic            string       `json:"-"`
	Message          *PeerMessage `json:"message"`
	Kind             string       `json:"kind,omitempty"`
	SenderAttendeeID string       `json:"senderAttendeeId"`
	SelfAttendeeID   string       `json:"selfAttendeeId"`
}

func encPoint(w *pbw, field int, p Point) {
	w.msg(field, func(w *pbw) {
		w.double(1, p.X)
		w.double(2, p.Y)
	})
}

func encAwareness(w *pbw, field int, a *Awareness) {
	w.msg(field, func(w *pbw) {
		w.optStr(1, a.Channel)
		w.optStr(2, a.User)
		for _, p := range a.State {
			w.msg(3, func(w *pbw) {
				w.optStr(1, p.FieldName)
				w.optStr(2, p.FieldValue)
				if p.FieldTimestampMS != nil {
					w.sint(3, *p.FieldTimestampMS)
				}
			})
		}
	})
}

// Encode serializes m.
func (m *PeerMessage) Encode() []byte {
	var w pbw
	if r := m.Reacji; r != nil {
		w.msg(1, func(w *pbw) {
			w.optStr(1, r.Emoji)
			if r.Type != 0 {
				w.uint(2, uint64(r.Type))
			}
			w.optStr(3, r.EmojiURL)
		})
	}
	if d := m.Draw; d != nil {
		w.msg(2, func(w *pbw) {
			if d.Type != 0 {
				w.uint(1, uint64(d.Type))
			}
			if s := d.Segment; s != nil {
				w.msg(2, func(w *pbw) {
					encPoint(w, 1, s.A)
					encPoint(w, 2, s.B)
					encPoint(w, 3, s.C)
					encPoint(w, 4, s.D)
				})
			}
			if d.Hue != 0 {
				w.sint(3, int64(d.Hue))
			}
			w.optStr(4, d.ItemID)
			if d.X != 0 {
				w.double(5, d.X)
			}
			if d.Y != 0 {
				w.double(6, d.Y)
			}
		})
	}
	if m.FromPeerID != 0 {
		w.uint(3, m.FromPeerID)
	}
	w.optStr(4, m.FromPeerIDString)
	if c := m.Capabilities; c != nil {
		w.msg(5, func(w *pbw) {
			if c.IsDrawEnabled {
				w.boolean(1, true)
			}
		})
	}
	if b := m.Background; b != nil {
		w.msg(6, func(w *pbw) { w.optStr(1, b.URL) })
	}
	if r := m.MuteRequest; r != nil {
		w.msg(10, func(w *pbw) { w.optStr(1, r.PeerID) })
	}
	if a := m.AwarenessBroadcast; a != nil {
		encAwareness(&w, 11, a)
	}
	if a := m.AwarenessQuery; a != nil {
		encAwareness(&w, 12, a)
	}
	if a := m.AwarenessClose; a != nil {
		encAwareness(&w, 13, a)
	}
	if j := m.JukeboxRequest; j != nil {
		w.msg(14, func(w *pbw) {
			w.optStr(1, j.SongKey)
			if j.Stop {
				w.boolean(2, true)
			}
		})
	}
	if c := m.ContentShareDetails; c != nil {
		w.msg(15, func(w *pbw) {
			w.optStr(1, c.SourceType)
			w.optStr(2, c.ItemID)
		})
	}
	if r := m.RecordingStartNotification; r != nil {
		w.msg(16, func(w *pbw) {
			if r.Type != 0 {
				w.uint(1, uint64(r.Type))
			}
			w.optStr(2, r.RecordingType)
		})
	}
	if t := m.TranscriptCatchUp; t != nil {
		w.msg(17, func(w *pbw) {
			if t.Type != 0 {
				w.uint(1, uint64(t.Type))
			}
			for _, l := range t.Lines {
				w.msg(2, func(w *pbw) {
					w.optStr(1, l.UserID)
					w.optStr(2, l.Content)
					if l.StartTimeMS != 0 {
						w.uint(3, l.StartTimeMS)
					}
					if l.EndTimeMS != 0 {
						w.uint(4, l.EndTimeMS)
					}
				})
			}
			if t.ChunkIndex != 0 {
				w.uint(3, uint64(t.ChunkIndex))
			}
			if t.TotalChunks != 0 {
				w.uint(4, uint64(t.TotalChunks))
			}
			w.optStr(5, t.RequestID)
			if t.EarliestTranscriptTimestampMS != 0 {
				w.uint(6, t.EarliestTranscriptTimestampMS)
			}
		})
	}
	if k := m.KickNotification; k != nil {
		w.msg(18, func(w *pbw) { w.optStr(1, k.KickedUserID) })
	}
	return w.b
}

func decPoint(b []byte) Point {
	var p Point
	_ = pbEach(b, func(f pbField) error {
		switch f.num {
		case 1:
			p.X = f.double()
		case 2:
			p.Y = f.double()
		}
		return nil
	})
	return p
}

func decAwareness(b []byte) *Awareness {
	a := &Awareness{}
	_ = pbEach(b, func(f pbField) error {
		switch f.num {
		case 1:
			a.Channel = f.str()
		case 2:
			a.User = f.str()
		case 3:
			var p AwarenessPayload
			_ = pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					p.FieldName = g.str()
				case 2:
					p.FieldValue = g.str()
				case 3:
					v := g.i64()
					p.FieldTimestampMS = &v
				}
				return nil
			})
			a.State = append(a.State, p)
		}
		return nil
	})
	return a
}

// DecodePeerMessage parses a peer message.
func DecodePeerMessage(b []byte) (*PeerMessage, error) {
	m := &PeerMessage{}
	err := pbEach(b, func(f pbField) error {
		if !f.isBytes() {
			if f.num == 3 {
				m.FromPeerID = f.v
			}
			return nil
		}
		switch f.num {
		case 1:
			r := &Reacji{}
			m.Reacji = r
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					r.Emoji = g.str()
				case 2:
					r.Type = int(g.v)
				case 3:
					r.EmojiURL = g.str()
				}
				return nil
			})
		case 2:
			d := &Draw{}
			m.Draw = d
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					d.Type = int(g.v)
				case 2:
					s := &Segment{}
					d.Segment = s
					return pbEach(g.b, func(h pbField) error {
						switch h.num {
						case 1:
							s.A = decPoint(h.b)
						case 2:
							s.B = decPoint(h.b)
						case 3:
							s.C = decPoint(h.b)
						case 4:
							s.D = decPoint(h.b)
						}
						return nil
					})
				case 3:
					d.Hue = int32(g.v)
				case 4:
					d.ItemID = g.str()
				case 5:
					d.X = g.double()
				case 6:
					d.Y = g.double()
				}
				return nil
			})
		case 4:
			m.FromPeerIDString = f.str()
		case 5:
			c := &Capabilities{}
			m.Capabilities = c
			return pbEach(f.b, func(g pbField) error {
				if g.num == 1 {
					c.IsDrawEnabled = g.boolean()
				}
				return nil
			})
		case 6:
			b := &Background{}
			m.Background = b
			return pbEach(f.b, func(g pbField) error {
				if g.num == 1 {
					b.URL = g.str()
				}
				return nil
			})
		case 10:
			r := &MuteRequest{}
			m.MuteRequest = r
			return pbEach(f.b, func(g pbField) error {
				if g.num == 1 {
					r.PeerID = g.str()
				}
				return nil
			})
		case 11:
			m.AwarenessBroadcast = decAwareness(f.b)
		case 12:
			m.AwarenessQuery = decAwareness(f.b)
		case 13:
			m.AwarenessClose = decAwareness(f.b)
		case 14:
			j := &JukeboxRequest{}
			m.JukeboxRequest = j
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					j.SongKey = g.str()
				case 2:
					j.Stop = g.boolean()
				}
				return nil
			})
		case 15:
			c := &ContentShareDetails{}
			m.ContentShareDetails = c
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					c.SourceType = g.str()
				case 2:
					c.ItemID = g.str()
				}
				return nil
			})
		case 16:
			r := &RecordingStart{}
			m.RecordingStartNotification = r
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					r.Type = int(g.v)
				case 2:
					r.RecordingType = g.str()
				}
				return nil
			})
		case 17:
			t := &TranscriptCatchUp{}
			m.TranscriptCatchUp = t
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					t.Type = int(g.v)
				case 2:
					var l TranscriptLine
					_ = pbEach(g.b, func(h pbField) error {
						switch h.num {
						case 1:
							l.UserID = h.str()
						case 2:
							l.Content = h.str()
						case 3:
							l.StartTimeMS = h.v
						case 4:
							l.EndTimeMS = h.v
						}
						return nil
					})
					t.Lines = append(t.Lines, l)
				case 3:
					t.ChunkIndex = g.u32()
				case 4:
					t.TotalChunks = g.u32()
				case 5:
					t.RequestID = g.str()
				case 6:
					t.EarliestTranscriptTimestampMS = g.v
				}
				return nil
			})
		case 18:
			k := &KickNotification{}
			m.KickNotification = k
			return pbEach(f.b, func(g pbField) error {
				if g.num == 1 {
					k.KickedUserID = g.str()
				}
				return nil
			})
		}
		return nil
	})
	return m, err
}

// transcriptResult is one Chime live-transcription result.
type transcriptResult struct {
	ResultID  string
	Partial   bool
	Text      string
	Speaker   string // attendee id of the first item
	SpeakerEx string
	StartMS   int64
}

// decodeTranscript parses an SdkTranscriptFrame (topic aws:chime:transcription).
func decodeTranscript(b []byte) (results []transcriptResult, status string) {
	_ = pbEach(b, func(f pbField) error { // frame.events
		if f.num != 1 {
			return nil
		}
		return pbEach(f.b, func(ev pbField) error {
			switch ev.num {
			case 1: // status
				_ = pbEach(ev.b, func(s pbField) error {
					if s.num == 1 {
						status = [...]string{"", "started", "interrupted", "resumed", "stopped", "failed"}[min(int(s.v), 5)]
					}
					return nil
				})
			case 2: // transcript.results
				_ = pbEach(ev.b, func(r pbField) error {
					if r.num != 1 {
						return nil
					}
					var res transcriptResult
					first := true
					_ = pbEach(r.b, func(x pbField) error {
						switch x.num {
						case 1: // alternatives (first one wins)
							if !first {
								return nil
							}
							first = false
							_ = pbEach(x.b, func(a pbField) error {
								switch a.num {
								case 1: // items
									if res.Speaker != "" {
										return nil
									}
									_ = pbEach(a.b, func(it pbField) error {
										switch it.num {
										case 3:
											res.Speaker = it.str()
										case 4:
											res.SpeakerEx = it.str()
										}
										return nil
									})
								case 2:
									res.Text = a.str()
								}
								return nil
							})
						case 4:
							res.Partial = x.boolean()
						case 5:
							res.ResultID = x.str()
						case 6:
							res.StartMS = x.i64()
						}
						return nil
					})
					results = append(results, res)
					return nil
				})
			}
			return nil
		})
	})
	return results, status
}
