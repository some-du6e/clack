package ui

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// An update's download progress, from the shell's downloader
// (Options.DownloadUpdate's progress callback; cmd/clack maps
// desktop.Progress onto it) to the Preferences row's bar and the menus'
// update item. Slack has no such screen: this is Clack's own updater UI.

// UpdateStage is how far a download has got.
type UpdateStage int

const (
	UpdateDownloading UpdateStage = iota // SHA256SUMS, then the package's bytes
	UpdateVerifying                      // hashing the package against SHA256SUMS
	UpdateStaging                        // moving the verified package into the cache
	UpdateStaged                         // verified and kept: ready to install next
)

// UpdateProgress is a download's state. Total is 0 while the size isn't
// known: the bar is indeterminate then.
type UpdateProgress struct {
	Stage       UpdateStage
	Done, Total int64
}

// frac is how much of the bar to fill, -1 for an indeterminate bar.
func (p UpdateProgress) frac() float32 {
	switch {
	case p.Stage >= UpdateStaging, p.Total <= 0:
		return -1
	case p.Done >= p.Total:
		return 1
	case p.Done <= 0:
		return 0
	}
	return float32(float64(p.Done) / float64(p.Total))
}

// percent is p's share done, 0..100; 100 only once every byte is in.
func (p UpdateProgress) percent() int {
	if p.Total <= 0 || p.Done <= 0 {
		return 0
	}
	if p.Done >= p.Total {
		return 100
	}
	return min(int(p.Done*100/p.Total), 99)
}

// updateProgressText is the row's line for a download of tag at p:
// "Downloading Clack v0.1.0-alpha.26… 34% (12.1 of 35.6 MB)".
func updateProgressText(tag string, p UpdateProgress) string {
	name := AppName + " " + tag
	switch p.Stage {
	case UpdateVerifying:
		s := "Verifying " + name + " against SHA256SUMS…"
		if p.Total > 0 {
			s += " " + strconv.Itoa(p.percent()) + "%"
		}
		return s
	case UpdateStaging, UpdateStaged:
		return "Preparing " + name + "…"
	}
	s := "Downloading " + name + "…"
	switch {
	case p.Total > 0:
		s += " " + strconv.Itoa(p.percent()) + "% (" + updSizePair(p.Done, p.Total) + ")"
	case p.Done > 0:
		s += " " + updSize(p.Done) // size unknown: what's in so far
	}
	return s
}

// updateMenuProgress is the menus' "Downloading Update" with its share
// (whole percents, so a native menu retitles at most 100 times).
func updateMenuProgress(p UpdateProgress) string {
	if p.Stage == UpdateDownloading && p.Total > 0 {
		return updMenuDownload + "… " + strconv.Itoa(p.percent()) + "%"
	}
	return updMenuDownload
}

// updSizePair is "12.1 of 35.6 MB", or "300 of 900 KB" under a megabyte, in
// Clack's binary units (attHumanSize, sizeText).
func updSizePair(done, total int64) string {
	const k, m = 1024, 1024 * 1024
	done = min(max(done, 0), total)
	if total < m {
		return fmt.Sprintf("%d of %d KB", (done+k/2)/k, (total+k/2)/k)
	}
	return fmt.Sprintf("%.1f of %.1f MB", float64(done)/m, float64(total)/m)
}

// updSize is one size: "512 KB", "12.1 MB".
func updSize(n int64) string {
	const k, m = 1024, 1024 * 1024
	if n < m {
		return fmt.Sprintf("%d KB", (n+k/2)/k)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/m)
}

// updateProgressEvery is the most often a download's progress reaches the
// UI: at most 10 redraws a second, whatever the downloader reports.
const updateProgressEvery = 100 * time.Millisecond

// progressSink takes a download's progress from any goroutine and hands
// the latest to the UI goroutine at most once per every: one post for
// however many reports came in between, never one per chunk.
type progressSink struct {
	every time.Duration
	post  func(UpdateProgress) // runs off the UI goroutine; must not block

	mu      sync.Mutex
	latest  UpdateProgress
	last    time.Time
	pending bool // a post is scheduled
	timer   *time.Timer
	stopped bool
}

func newProgressSink(every time.Duration, post func(UpdateProgress)) *progressSink {
	return &progressSink{every: every, post: post}
}

// report records p (safe from any goroutine, cheap enough per chunk).
func (s *progressSink) report(p UpdateProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.latest = p
	if s.pending {
		return // the scheduled post carries this one
	}
	s.pending = true
	wait := s.every - time.Since(s.last)
	if wait <= 0 {
		s.flushLocked()
		return
	}
	s.timer = time.AfterFunc(wait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.stopped && s.pending {
			s.flushLocked()
		}
	})
}

func (s *progressSink) flushLocked() {
	s.pending, s.last = false, time.Now()
	s.post(s.latest)
}

// stop drops anything still scheduled: the download is over.
func (s *progressSink) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
	}
}
