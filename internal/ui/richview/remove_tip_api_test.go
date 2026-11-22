package richview

import (
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The remove tooltip test's reads (remove_tip_test.go).

func hideTipsT() { theme.HideTooltips() }

// removeTipUpT reports whether the remove button's tooltip is up at now.
func removeTipUpT(v *View, now time.Time) bool {
	return v.remHot == 1 && v.copyGen == theme.TipGen() && !now.Before(v.copyTipAt.Add(removeTipDelay))
}
