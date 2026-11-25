package desktop

import "golang.org/x/sys/windows"

// hidePath sets FILE_ATTRIBUTE_HIDDEN on p (an update's stage, a swapped
// out Clack.exe.clack-old), as Explorer hides it; failures don't matter.
func hidePath(p string) {
	n, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return
	}
	a, err := windows.GetFileAttributes(n)
	if err != nil {
		return
	}
	_ = windows.SetFileAttributes(n, a|windows.FILE_ATTRIBUTE_HIDDEN)
}
