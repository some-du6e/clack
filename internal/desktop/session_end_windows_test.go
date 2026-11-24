package desktop

import (
	"strings"
	"testing"
)

// Every WM_ENDSESSION reason carries SessionEndMark, which keeps the quit
// it starts from installing an update (cmd/clack quitInstallBlocked).
func TestEndSessionReasonCarriesTheMark(t *testing.T) {
	for _, lp := range []uintptr{0, 0x1, 0x80000000, 0x40000000, 0x80000000 | 0x40000000} {
		if why := endSessionReason(lp); !strings.HasSuffix(why, " "+SessionEndMark) {
			t.Errorf("endSessionReason(%#x) = %q", lp, why)
		}
	}
}
