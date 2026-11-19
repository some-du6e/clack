//go:build js

package rtc

// Stand-ins for the browser (rtc.go): the same names, fields and methods
// package huddle uses, none of which can open a connection. NewAPI's
// NewPeerConnection fails with ErrNoHuddles, so a huddle stops at its
// first media step, and nothing here ever touches the browser's WebRTC
// or the network.

import (
	"errors"
	"strings"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media"
)

// ErrNoHuddles is what opening a connection gets: the browser demo leaves
// huddles out.
var ErrNoHuddles = errors.New("huddles aren't part of the browser demo")

type (
	BundlePolicy            int
	ICETransportPolicy      int
	NetworkType             int
	PayloadType             uint8
	SSRC                    uint32
	RTPCodecType            int
	RTPTransceiverDirection int
	SDPSemantics            int
	SDPType                 int
	PeerConnectionState     int
)

const (
	BundlePolicyMaxBundle           BundlePolicy            = 3
	ICETransportPolicyRelay         ICETransportPolicy      = 1
	MimeTypeH264                                            = "video/H264"
	MimeTypeOpus                                            = "audio/opus"
	NetworkTypeUDP4                 NetworkType             = 1
	RTPCodecTypeAudio               RTPCodecType            = 1
	RTPCodecTypeVideo               RTPCodecType            = 2
	RTPTransceiverDirectionSendrecv RTPTransceiverDirection = 2
	RTPTransceiverDirectionRecvonly RTPTransceiverDirection = 4
	SDPSemanticsUnifiedPlan         SDPSemantics            = 0
	SDPTypeOffer                    SDPType                 = 1
	SDPTypeAnswer                   SDPType                 = 3
)

func (t RTPCodecType) String() string {
	switch t {
	case RTPCodecTypeAudio:
		return "audio"
	case RTPCodecTypeVideo:
		return "video"
	}
	return "unknown"
}

func (PeerConnectionState) String() string { return "closed" }

type RTCPFeedback struct{ Type, Parameter string }

type RTPCodecCapability struct {
	MimeType     string
	ClockRate    uint32
	Channels     uint16
	SDPFmtpLine  string
	RTCPFeedback []RTCPFeedback
}

type RTPCodecParameters struct {
	RTPCodecCapability
	PayloadType PayloadType
}

type RTPTransceiverInit struct{ Direction RTPTransceiverDirection }

type SessionDescription struct {
	Type SDPType
	SDP  string
}

type ICEServer struct {
	URLs       []string
	Username   string
	Credential any
}

type Configuration struct {
	ICEServers         []ICEServer
	ICETransportPolicy ICETransportPolicy
	BundlePolicy       BundlePolicy
	SDPSemantics       SDPSemantics
}

type RTPEncodingParameters struct{ SSRC SSRC }

type RTPSendParameters struct{ Encodings []RTPEncodingParameters }

type OfferOptions struct{}

type AnswerOptions struct{}

type SettingEngine struct{}

func (*SettingEngine) SetIncludeLoopbackCandidate(bool)                        {}
func (*SettingEngine) SetNetworkTypes([]NetworkType)                           {}
func (*SettingEngine) SetSRTPProtectionProfiles(...dtls.SRTPProtectionProfile) {}

type MediaEngine struct{}

func (*MediaEngine) RegisterCodec(RTPCodecParameters, RTPCodecType) error { return nil }
func (*MediaEngine) RegisterDefaultCodecs() error                         { return nil }

func ConfigureNack(*MediaEngine, *interceptor.Registry) error               { return nil }
func RegisterDefaultInterceptors(*MediaEngine, *interceptor.Registry) error { return nil }

type API struct{}

func NewAPI(...func(*API)) *API                                { return &API{} }
func WithInterceptorRegistry(*interceptor.Registry) func(*API) { return func(*API) {} }
func WithMediaEngine(*MediaEngine) func(*API)                  { return func(*API) {} }
func WithSettingEngine(SettingEngine) func(*API)               { return func(*API) {} }

func (*API) NewPeerConnection(Configuration) (*PeerConnection, error) { return nil, ErrNoHuddles }
func (*API) NewRTPSender(TrackLocal, *DTLSTransport) (*RTPSender, error) {
	return nil, ErrNoHuddles
}

// TrackLocal is what a sender carries.
type TrackLocal interface{ Kind() RTPCodecType }

type TrackLocalStaticSample struct{ codec RTPCodecCapability }

func NewTrackLocalStaticSample(c RTPCodecCapability, id, streamID string) (*TrackLocalStaticSample, error) {
	return &TrackLocalStaticSample{codec: c}, nil
}

func (t *TrackLocalStaticSample) Codec() RTPCodecCapability { return t.codec }

func (t *TrackLocalStaticSample) Kind() RTPCodecType {
	if strings.HasPrefix(t.codec.MimeType, "audio/") {
		return RTPCodecTypeAudio
	}
	return RTPCodecTypeVideo
}

func (*TrackLocalStaticSample) WriteSample(media.Sample) error { return ErrNoHuddles }

type TrackRemote struct{}

func (*TrackRemote) Codec() RTPCodecParameters       { return RTPCodecParameters{} }
func (*TrackRemote) Kind() RTPCodecType              { return 0 }
func (*TrackRemote) PayloadType() PayloadType        { return 0 }
func (*TrackRemote) SSRC() SSRC                      { return 0 }
func (*TrackRemote) SetReadDeadline(time.Time) error { return nil }
func (*TrackRemote) ReadRTP() (*rtp.Packet, interceptor.Attributes, error) {
	return nil, nil, ErrNoHuddles
}

type DTLSTransport struct{}

type SCTPTransport struct{}

func (*SCTPTransport) Transport() *DTLSTransport { return nil }

type RTPSender struct{}

func (*RTPSender) Track() TrackLocal                { return nil }
func (*RTPSender) GetParameters() RTPSendParameters { return RTPSendParameters{} }
func (*RTPSender) ReadRTCP() ([]rtcp.Packet, interceptor.Attributes, error) {
	return nil, nil, ErrNoHuddles
}

type RTPReceiver struct{}

type RTPTransceiver struct{}

func (*RTPTransceiver) Kind() RTPCodecType                     { return 0 }
func (*RTPTransceiver) Receiver() *RTPReceiver                 { return nil }
func (*RTPTransceiver) Sender() *RTPSender                     { return nil }
func (*RTPTransceiver) SetSender(*RTPSender, TrackLocal) error { return ErrNoHuddles }

type PeerConnection struct{}

func (*PeerConnection) AddTrack(TrackLocal) (*RTPSender, error) { return nil, ErrNoHuddles }
func (*PeerConnection) AddTransceiverFromKind(RTPCodecType, ...RTPTransceiverInit) (*RTPTransceiver, error) {
	return nil, ErrNoHuddles
}
func (*PeerConnection) AddTransceiverFromTrack(TrackLocal, ...RTPTransceiverInit) (*RTPTransceiver, error) {
	return nil, ErrNoHuddles
}
func (*PeerConnection) Close() error { return nil }
func (*PeerConnection) CreateAnswer(*AnswerOptions) (SessionDescription, error) {
	return SessionDescription{}, ErrNoHuddles
}
func (*PeerConnection) CreateOffer(*OfferOptions) (SessionDescription, error) {
	return SessionDescription{}, ErrNoHuddles
}
func (*PeerConnection) GetSenders() []*RTPSender                          { return nil }
func (*PeerConnection) GetTransceivers() []*RTPTransceiver                { return nil }
func (*PeerConnection) LocalDescription() *SessionDescription             { return &SessionDescription{} }
func (*PeerConnection) OnConnectionStateChange(func(PeerConnectionState)) {}
func (*PeerConnection) OnTrack(func(*TrackRemote, *RTPReceiver))          {}
func (*PeerConnection) RemoveTrack(*RTPSender) error                      { return ErrNoHuddles }
func (*PeerConnection) SCTP() *SCTPTransport                              { return &SCTPTransport{} }
func (*PeerConnection) SetLocalDescription(SessionDescription) error      { return ErrNoHuddles }
func (*PeerConnection) SetRemoteDescription(SessionDescription) error     { return ErrNoHuddles }
func (*PeerConnection) WriteRTCP([]rtcp.Packet) error                     { return ErrNoHuddles }

// GatheringCompletePromise is closed at once: nothing gathers.
func GatheringCompletePromise(*PeerConnection) <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}
