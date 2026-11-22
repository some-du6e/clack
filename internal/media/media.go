// Package media plays a message's video the way Slack's <video> does,
// with the operating system's decoder: Media Foundation on Windows
// (internal/media/mf), AVFoundation on macOS (internal/media/avf). The
// decoder's frames are GPU textures that Gio draws inside the window's own
// frame (Gio patch 51, external images), under Slack's own controls, so a
// frame's pixels never enter Go memory.
//
// The backends never get a Slack URL: they read the file through a
// Source, whose bytes come from Go (RangeSource over
// slack.Client.DownloadRange, which keeps the session's TLS fingerprint
// and Download's guards), and nothing is written to disk.
// TestNativeNeverFetches holds the backends to that.
package media

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"strings"
	"sync"
	"time"

	"gioui.org/op/paint"
)

// Source is a file a player plays.
type Source struct {
	// FileID is the Slack file's id (logs, and the URL hint the demuxer
	// is given: clack-media://<id>.<ext>, never the file's own URL).
	FileID string
	// Mime is the file's mimetype, Ext the extension picking the demuxer:
	// mp4, mov, m4v.
	Mime, Ext string
	// Open is a streaming read of the file from off: the body from off on
	// a seekable answer (a 206), the whole file from byte 0 on one that
	// isn't (a 200). RangeSource calls it, at most one body open at once.
	Open func(ctx context.Context, off int64) (io.ReadCloser, Info, error)
}

// IsAudio reports whether a source is an audio file (a message's audio
// player's): an audio/* type, or an audio extension. A backend plays it
// with no picture: no frames, no device of Gio's.
func IsAudio(src Source) bool {
	if strings.HasPrefix(strings.ToLower(src.Mime), "audio/") {
		return true
	}
	switch strings.ToLower(strings.TrimPrefix(src.Ext, ".")) {
	case "m4a", "mp3", "wav", "ogg", "oga", "opus", "aac":
		return true
	}
	return false
}

// Info is what the file's server answered.
type Info struct {
	// Status is the HTTP status (206, 200); 0 for a source that isn't one.
	Status int
	// Total is the file's size; -1 unknown.
	Total int64
	// Seekable is whether another offset can be asked for: the server
	// answered the range (206). Not seekable, the body starts at byte 0.
	Seekable bool
	// ETag and LastModified are the answer's validators: a later answer
	// with others is another version of the file (ErrChanged).
	ETag, LastModified string
}

// HTML5 readyState values (State.Ready).
const (
	HaveNothing = iota
	HaveMetadata
	HaveCurrentData
	HaveFutureData
	HaveEnoughData
)

// HTML5 MediaError codes (State.Err).
const (
	ErrAborted         = 1
	ErrNetwork         = 2
	ErrDecode          = 3
	ErrSrcNotSupported = 4 // MEDIA_ERR_SRC_NOT_SUPPORTED: also what Chromium gives a src whose fetch failed before the metadata
)

// FrameCopier is optional on a Player: CopyFrame is the frame it shows
// now, read back at the size it draws it (sRGB bytes, opaque), so a tile
// that lets an idle player go keeps showing it (Chromium's idle suspend
// keeps the last frame on screen).
type FrameCopier interface {
	CopyFrame() (*image.RGBA, bool)
}

// State is a player's state now, as an HTMLMediaElement's attributes.
type State struct {
	Pos, Duration time.Duration
	// Size is the video's size, 0 until its metadata.
	Size image.Point
	// Paused, Ended and Waiting are paused, ended and whether playback
	// waits for data (the waiting event until playing).
	Paused, Ended, Waiting bool
	// Ready is the readyState (HaveNothing…HaveEnoughData).
	Ready int
	// Err is the MediaError code, 0 for none.
	Err int
	// Volume (0…1), Muted and Loop as last set.
	Volume float64
	Muted  bool
	Loop   bool
}

// EventKind is an HTMLMediaElement event's.
type EventKind uint8

const (
	LoadedMetadata EventKind = iota + 1
	LoadedData
	Play
	Playing
	Pause
	Waiting
	Seeking
	Seeked
	TimeUpdate
	Ended
	DurationChange
	Error
	// FirstFrame is the first frame ready to be drawn (Media Engine's
	// MF_MEDIA_ENGINE_EVENT_FIRSTFRAMEREADY, Slack's time to first frame).
	FirstFrame
)

var kindNames = [...]string{LoadedMetadata: "loadedmetadata", LoadedData: "loadeddata", Play: "play", Playing: "playing",
	Pause: "pause", Waiting: "waiting", Seeking: "seeking", Seeked: "seeked", TimeUpdate: "timeupdate", Ended: "ended",
	DurationChange: "durationchange", Error: "error", FirstFrame: "firstframe"}

func (k EventKind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return fmt.Sprintf("event(%d)", k)
}

// Event is an event and the player's state as it fired.
type Event struct {
	Kind  EventKind
	State State
}

// Stats are a player's running counts, for the cost budgets' tests.
type Stats struct {
	// Frames are the new video frames published (frameReady calls),
	// Transfers the frames copied into a texture the GPU draws (Windows:
	// TransferVideoFrame; macOS: pixel buffers wrapped as textures; the
	// fake: frames made), Ticks the media thread's polls for a frame.
	Frames, Transfers, Ticks int64
	// Asks are the GPU's asks for the frame to draw.
	Asks int64
	// Requests are the GETs the source made, BytesRead the bytes they
	// brought.
	Requests, BytesRead int64
	// Dropped are events dropped because nothing read them.
	Dropped int64
	// Skipped are new frames the media thread had no free texture for,
	// Timeouts the asks that couldn't take the newest frame (Windows: the
	// shared ring's keyed mutexes, both sides waiting for neither).
	Skipped, Timeouts int64
}

// Player is one <video>: a source, decoding on the OS's media stack.
// Every method returns at once; the controls are posted to the player's
// own thread or queue, and State is a snapshot.
type Player interface {
	Play()
	Pause()
	// Seek is an exact seek, as currentTime = t.
	Seek(t time.Duration)
	SetVolume(v float64)
	SetMuted(m bool)
	SetLoop(l bool)
	// SetRate is playbackRate, keeping pitch.
	SetRate(r float64)
	// SetDrawSize is the size the player is drawn at, in pixels: frames
	// come at this size or the video's, whichever is smaller.
	SetDrawSize(px image.Point)
	// Op is the op drawing the player's frame over size pixels: an
	// external image (Gio patch 51) on the real backends, a live one on
	// the fake; still (the poster) while there's no frame.
	Op(still paint.ImageOp, size image.Point) paint.ImageOp
	State() State
	// Events are the player's events, in order; a full channel drops
	// (Stats.Dropped).
	Events() <-chan Event
	Stats() Stats
	// Close stops the player and frees what it holds. Events closes.
	Close()
}

// Backend makes players.
type Backend interface {
	Name() string
	// Available is nil when the backend can play here (Media Foundation
	// present, …).
	Available() error
	// New makes a player of src, paused, loading it at once. frameReady is
	// called (on any goroutine) when a new frame is ready to be drawn: the
	// App's repaint, which shows the last frame's ops again with no layout
	// (internal/ui quiet.go).
	New(src Source, frameReady func()) (Player, error)
}

// ErrNoBackend is Available's answer where nothing can play video: Linux,
// Windows arm64 (no cgo), Windows N without the Media Feature Pack.
var ErrNoBackend = errors.New("media: no video backend on this system")

var reg struct {
	sync.Mutex
	backends []Backend
}

// Register adds a backend; mf and avf register themselves in init, and
// cmd/clack imports them.
func Register(b Backend) {
	reg.Lock()
	defer reg.Unlock()
	reg.backends = append(reg.backends, b)
}

// Default is the first registered backend that is available here.
func Default() (Backend, error) {
	reg.Lock()
	defer reg.Unlock()
	var first error
	for _, b := range reg.backends {
		err := b.Available()
		if err == nil {
			return b, nil
		}
		if first == nil {
			first = fmt.Errorf("media: %s: %w", b.Name(), err)
		}
	}
	if first != nil {
		return nil, first
	}
	return nil, ErrNoBackend
}

// Available is nil when a registered backend can play video here.
func Available() error {
	_, err := Default()
	return err
}
