//go:build windows

package desktop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The jump list on the runner (CLACK_WINDOWS_TRAY_TESTS, ci's
// windows-shell job), for a scratch app id: SetJumpList commits Slack's
// two custom categories, the shell saves them (its
// CustomDestinations\*.customDestinations-ms file holds the categories'
// names, the tasks' titles and arguments), an empty category is left out,
// nothing counts as removed, a description over 260 characters fails
// that task, and DeleteJumpList clears it.

const jumpListTestID = "io.github.some-du6e.clack.jumplist-test"

// utf16le is s as the shell's files store strings.
func utf16le(s string) []byte {
	u := windows.StringToUTF16(s)
	b := make([]byte, 0, 2*len(u))
	for _, c := range u[:len(u)-1] {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

// customDestinations is the newest .customDestinations-ms file changed
// since since that holds marker, or nil.
func customDestinations(t *testing.T, since time.Time, marker string) []byte {
	t.Helper()
	dir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Recent", "CustomDestinations")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Logf("%s: %v", dir, err)
		return nil
	}
	m := utf16le(marker)
	var best []byte
	var bestAt time.Time
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(since) || !strings.HasSuffix(e.Name(), ".customDestinations-ms") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !bytes.Contains(b, m) {
			continue
		}
		if fi.ModTime().After(bestAt) {
			best, bestAt = b, fi.ModTime()
		}
	}
	return best
}

func TestJumpList(t *testing.T) {
	trayShellTests(t)
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, registry.QUERY_VALUE); err == nil {
		v, _, err := k.GetIntegerValue("Start_TrackDocs")
		t.Logf("Start_TrackDocs (recent items in Jump Lists): %d %v", v, err)
		k.Close()
	}
	t.Cleanup(func() { _ = DeleteJumpList(jumpListTestID) })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	team := fmt.Sprintf("T0JL%d", time.Now().UnixNano()%1e9)
	open := "slack://open?team=" + team
	cats := func(withTeam bool) []JumpListCategory {
		var items []JumpListTask
		if withTeam {
			items = append(items, JumpListTask{Title: "Fake Workspace", Description: "Fake Workspace", Program: exe, Args: open, IconPath: exe})
		}
		return []JumpListCategory{
			{Name: "Workspaces", Items: items},
			{Name: "Tasks", Items: []JumpListTask{{Title: "Move Clack to Current Display", Description: "Move the Clack window to the current display",
				Program: exe, Args: "slack://app/?action=reset-window", IconPath: exe, IconIndex: -2}}},
		}
	}
	since := time.Now().Add(-2 * time.Second)
	if err := SetJumpList(jumpListTestID, cats(true)); err != nil {
		t.Fatalf("SetJumpList: %v", err)
	}
	var file []byte
	for i := 0; i < 50 && file == nil; i++ {
		file = customDestinations(t, since, open)
		if file == nil {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if file == nil {
		t.Fatalf("no CustomDestinations file holds %q", open)
	}
	for _, s := range []string{"Workspaces", "Fake Workspace", "Tasks", "Move Clack to Current Display", "slack://app/?action=reset-window", exe} {
		if !bytes.Contains(file, utf16le(s)) {
			t.Errorf("the saved list has no %q", s)
		}
	}
	removed, err := JumpListRemoved(jumpListTestID)
	if err != nil || len(removed) != 0 {
		t.Errorf("JumpListRemoved: %v %v, want none", removed, err)
	}

	// no workspace: only Tasks
	since = time.Now().Add(-time.Second)
	if err := SetJumpList(jumpListTestID, cats(false)); err != nil {
		t.Fatalf("SetJumpList without a workspace: %v", err)
	}
	var again []byte
	for i := 0; i < 50 && again == nil; i++ {
		again = customDestinations(t, since, "slack://app/?action=reset-window")
		if again == nil {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if again == nil {
		t.Fatal("the list without a workspace wasn't saved")
	}
	if bytes.Contains(again, utf16le(open)) || bytes.Contains(again, utf16le("Workspaces")) {
		t.Error("an empty Workspaces category is in the list")
	}

	// Electron refuses a description over 260 characters; the rest commits
	long := cats(true)
	long[0].Items[0].Description = strings.Repeat("x", 261)
	err = SetJumpList(jumpListTestID, long)
	if err == nil || !strings.Contains(err.Error(), "260") {
		t.Errorf("a 261-character description: %v", err)
	}

	if err := DeleteJumpList(jumpListTestID); err != nil {
		t.Errorf("DeleteJumpList: %v", err)
	}
}
