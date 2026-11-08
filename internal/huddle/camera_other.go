//go:build !(darwin && cgo)

package huddle

// No camera backend in this build: openDefaultCamera stays nil, so
// CameraSupported is false and SetCamera(true) reports
// ErrCameraUnsupported (the huddle window greys its camera button).
//
// A backend plugs in by setting openDefaultCamera from an init in its own
// file, as camera_darwin.go does: V4L2 (linux, an mmap'd /dev/videoN
// delivering YUYV or NV12, converted to CameraFrame) or Media Foundation
// (windows, IMFSourceReader with MFVideoFormat_NV12). Frames go to the
// encoder chosen by newVideoEncoder, which on those systems would be
// OpenH264 or the OS's H.264 MFT; the pure-Go I_PCM encoder (h264pcm.go)
// is only small enough for the test pattern.
const cameraBackend = "none"
