package bgblur

// cadence is Slack's MaskCadenceController (the background worker's
// video-effects module 4Cqi): the segmenter runs on every decimation-th
// frame and the mask in between is the last one. decimation climbs a rung
// of [1, 2, 3, 4] after two 2-second windows more than half busy, and goes
// back down after three windows under a quarter busy, at most one change
// per 3 s. The huddle worker creates it without maxMaskRung (so 3) and it
// starts on rung 1: every second frame.
type cadence struct {
	maxRung, rung        int
	framesSinceMask      int
	windowStartedAtMs    float64
	windowBusyMs         float64
	hotWindows           int
	coolWindows          int
	lastRungChangeAtMs   float64
	lastUtilization      float64
	sourcePeriodMs       float64
	lastFrameTimestampUs float64
}

var cadenceRungs = [...]int{1, 2, 3, 4}

func newCadence(maxRung int) *cadence {
	maxRung = max(0, min(maxRung, len(cadenceRungs)-1))
	return &cadence{maxRung: maxRung, rung: min(1, maxRung)}
}

func (c *cadence) decimation() int { return cadenceRungs[c.rung] }

// shouldSegment counts a frame and says whether to run the segmenter on it.
func (c *cadence) shouldSegment(haveMask bool) bool {
	c.framesSinceMask++
	if !haveMask || c.framesSinceMask >= c.decimation() {
		c.framesSinceMask = 0
		return true
	}
	return false
}

// accumulateUtilization adds one frame's busy time at time nowMs.
func (c *cadence) accumulateUtilization(busyMs, nowMs float64) {
	if c.windowStartedAtMs == 0 {
		c.windowStartedAtMs = nowMs
	}
	c.windowBusyMs += busyMs
	r := nowMs - c.windowStartedAtMs
	if r < 2000 {
		return
	}
	c.lastUtilization = c.windowBusyMs / r
	c.windowStartedAtMs = nowMs
	c.windowBusyMs = 0
	if c.lastUtilization > 0.5 {
		c.hotWindows++
	} else {
		c.hotWindows = 0
	}
	if c.lastUtilization < 0.25 {
		c.coolWindows++
	} else {
		c.coolWindows = 0
	}
	if nowMs-c.lastRungChangeAtMs < 3000 {
		return
	}
	if c.hotWindows >= 2 && c.rung < c.maxRung {
		c.rung++
	} else {
		if c.coolWindows < 3 || c.rung <= 0 {
			return
		}
		c.rung--
	}
	c.hotWindows, c.coolWindows = 0, 0
	c.lastRungChangeAtMs = nowMs
}

// trackSourcePeriod keeps the camera's frame period (an EWMA of gaps
// between 8 and 100 ms), for the pressure reports.
func (c *cadence) trackSourcePeriod(tsUs float64) {
	t := c.lastFrameTimestampUs
	c.lastFrameTimestampUs = tsUs
	if t == 0 {
		return
	}
	r := (tsUs - t) / 1000
	if r < 8 || r > 100 {
		return
	}
	if c.sourcePeriodMs == 0 {
		c.sourcePeriodMs = r
	} else {
		c.sourcePeriodMs += 0.1 * (r - c.sourcePeriodMs)
	}
}
