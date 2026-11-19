//go:build !js

// Package rtc is the part of pion/webrtc that package huddle uses, which
// imports it under pion's name, webrtc. Everywhere but the browser it is
// pion itself: type aliases (so every method comes along) and functions
// that call pion's. Under GOOS=js pion's build has no media tracks, so
// rtc_js.go has stand-ins whose connections never open: the browser demo
// (cmd/clack-web) leaves huddles out, and Join fails with ErrNoHuddles
// before anything is sent.
package rtc

import (
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

type (
	API                     = webrtc.API
	BundlePolicy            = webrtc.BundlePolicy
	Configuration           = webrtc.Configuration
	ICEServer               = webrtc.ICEServer
	ICETransportPolicy      = webrtc.ICETransportPolicy
	MediaEngine             = webrtc.MediaEngine
	NetworkType             = webrtc.NetworkType
	PayloadType             = webrtc.PayloadType
	PeerConnection          = webrtc.PeerConnection
	PeerConnectionState     = webrtc.PeerConnectionState
	RTCPFeedback            = webrtc.RTCPFeedback
	RTPCodecCapability      = webrtc.RTPCodecCapability
	RTPCodecParameters      = webrtc.RTPCodecParameters
	RTPCodecType            = webrtc.RTPCodecType
	RTPReceiver             = webrtc.RTPReceiver
	RTPSender               = webrtc.RTPSender
	RTPTransceiver          = webrtc.RTPTransceiver
	RTPTransceiverDirection = webrtc.RTPTransceiverDirection
	RTPTransceiverInit      = webrtc.RTPTransceiverInit
	SDPSemantics            = webrtc.SDPSemantics
	SDPType                 = webrtc.SDPType
	SessionDescription      = webrtc.SessionDescription
	SettingEngine           = webrtc.SettingEngine
	TrackLocalStaticSample  = webrtc.TrackLocalStaticSample
	TrackRemote             = webrtc.TrackRemote
)

const (
	BundlePolicyMaxBundle           = webrtc.BundlePolicyMaxBundle
	ICETransportPolicyRelay         = webrtc.ICETransportPolicyRelay
	MimeTypeH264                    = webrtc.MimeTypeH264
	MimeTypeOpus                    = webrtc.MimeTypeOpus
	NetworkTypeUDP4                 = webrtc.NetworkTypeUDP4
	RTPCodecTypeAudio               = webrtc.RTPCodecTypeAudio
	RTPCodecTypeVideo               = webrtc.RTPCodecTypeVideo
	RTPTransceiverDirectionRecvonly = webrtc.RTPTransceiverDirectionRecvonly
	RTPTransceiverDirectionSendrecv = webrtc.RTPTransceiverDirectionSendrecv
	SDPSemanticsUnifiedPlan         = webrtc.SDPSemanticsUnifiedPlan
	SDPTypeAnswer                   = webrtc.SDPTypeAnswer
	SDPTypeOffer                    = webrtc.SDPTypeOffer
)

func ConfigureNack(m *MediaEngine, r *interceptor.Registry) error { return webrtc.ConfigureNack(m, r) }

func GatheringCompletePromise(pc *PeerConnection) <-chan struct{} {
	return webrtc.GatheringCompletePromise(pc)
}

func NewAPI(options ...func(*API)) *API { return webrtc.NewAPI(options...) }

func NewTrackLocalStaticSample(c RTPCodecCapability, id, streamID string, options ...func(*webrtc.TrackLocalStaticRTP)) (*TrackLocalStaticSample, error) {
	return webrtc.NewTrackLocalStaticSample(c, id, streamID, options...)
}

func RegisterDefaultInterceptors(m *MediaEngine, r *interceptor.Registry) error {
	return webrtc.RegisterDefaultInterceptors(m, r)
}

func WithInterceptorRegistry(r *interceptor.Registry) func(*API) {
	return webrtc.WithInterceptorRegistry(r)
}

func WithMediaEngine(m *MediaEngine) func(*API) { return webrtc.WithMediaEngine(m) }

func WithSettingEngine(s SettingEngine) func(*API) { return webrtc.WithSettingEngine(s) }
