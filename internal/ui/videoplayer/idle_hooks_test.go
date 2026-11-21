package videoplayer

import (
	"testing"
	"time"
)

// setIdleAfter makes the idle release come d after a pause for the test
// (registered first, it's undone after the controller's cleanup).
func setIdleAfter(t *testing.T, d time.Duration) { t.Cleanup(SetIdleAfter(d)) }

// tileStill is whether the tile shows a let-go player's frame (State.Still).
func tileStill(st State) bool { return st.Still }
