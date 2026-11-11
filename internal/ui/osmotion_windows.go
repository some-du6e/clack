//go:build windows

package ui

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Windows: Settings > Accessibility > Visual effects > "Animation effects"
// is SPI_GETCLIENTAREAANIMATION (FALSE: animations off). Polled off the UI
// goroutine, like the app mode (osdark_windows.go).

var procMotionSPI = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")

const spiGetClientAreaAnimation = 0x1042

func osReadReduceMotion() (reduce, ok bool) {
	uiguard.OffUI("osReadReduceMotion")
	if procMotionSPI.Find() != nil {
		return false, false
	}
	var on int32 // BOOL
	r, _, _ := procMotionSPI.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&on)), 0)
	if r == 0 {
		return false, false
	}
	return on == 0, true
}

func startOSMotionWatcher() {
	go func() {
		for {
			if reduce, ok := osReadReduceMotion(); ok {
				setOSReduceMotion(reduce)
			}
			time.Sleep(5 * time.Second)
		}
	}()
}
