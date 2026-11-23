package openurl

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// checkFile is p when it is an absolute path to an existing regular
// file, else an error: a downloaded file is all OpenPath and Reveal hand
// to the system.
func checkFile(p string) (string, error) {
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
		return "", ErrRefused
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", ErrRefused
	}
	return p, nil
}

// OpenPath opens a downloaded file with the system's application for it
// (Electron's shell.openPath, which the desktop app's openDownloadEpic
// calls after its own checks: restricted types are refused before here).
// It returns once the opener has started; call it off the UI goroutine.
func OpenPath(p string) error {
	p, err := checkFile(p)
	if err != nil {
		return err
	}
	uiguard.OffUI("exec (open a file)")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", p) // an absolute path: never read as a flag
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// Reveal shows a downloaded file selected in its folder (Electron's
// shell.showItemInFolder): Finder, Explorer, or the folder in the file
// manager on Linux. Call it off the UI goroutine.
func Reveal(p string) error {
	p, err := checkFile(p)
	if err != nil {
		return err
	}
	uiguard.OffUI("exec (show a file)")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", p)
	case "windows":
		cmd = exec.Command("explorer", "/select,"+p)
		cmd.SysProcAttr = revealAttr(p)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(p))
	}
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
