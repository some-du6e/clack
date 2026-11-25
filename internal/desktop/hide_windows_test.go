package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// A portable Clack's update stage waits beside Clack.exe from the download
// until the quit; Windows shows dot-folders, so it's hidden.
func TestStageIsHidden(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Clack.exe")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage, err := stageNext(target)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := windows.UTF16PtrFromString(stage)
	a, err := windows.GetFileAttributes(n)
	if err != nil {
		t.Fatal(err)
	}
	if a&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Errorf("the stage %s isn't hidden (attributes %#x)", stage, a)
	}
}
