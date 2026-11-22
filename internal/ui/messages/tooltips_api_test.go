package messages

import (
	"image"
	"time"

	"github.com/some-du6e/clack/internal/ui/richview"
)

// The tooltip tests' reads of the row and list (tooltips_patched_test.go).

func rowStatusURL(r *row) string { return r.statusURL }
func rowStatusHov(r *row) bool   { return r.statusHov }
func rowStatusDrawn(r *row) bool { return r.statusDrawn }
func rowTimeHov(r *row) bool     { return r.tsTip.hov }
func rowCompactHov(r *row) bool  { return r.compactTip.hov }

// rowCompactTipShowing reports whether the gutter time's tooltip is up.
func rowCompactTipShowing(r *row, now time.Time) bool { return r.compactTip.tip.Showing(now) }

// statusTipState is the status tooltip the list holds.
func statusTipState(s *listState) (on bool, text, sub string, rect image.Rectangle) {
	return s.stOn, s.stTip.text, s.stTip.sub, s.stRect
}

func fullTimestampFor(ts string, now time.Time) string { return richview.FullTimestampSeconds(ts, now) }

const (
	statusTipDelayT      = statusTipDelay
	timestampTipDelayT   = timestampTipDelay
	reactionAddTipT      = reactionAddTip
	reactionAddTipDelayT = reactionAddTipDelay
)
