//go:build !windows

package desktop

// The jump list is the Windows taskbar button's (jumplist_windows.go).

// JumpListTask is a Windows jump list task.
type JumpListTask struct {
	Title, Description, Program, Args, IconPath string
	IconIndex                                   int
}

// JumpListCategory is a Windows jump list custom category.
type JumpListCategory struct {
	Name  string
	Items []JumpListTask
}

// SetJumpList is a no-op.
func SetJumpList(appID string, cats []JumpListCategory) error { return nil }

// JumpListRemoved is a no-op.
func JumpListRemoved(appID string) ([]JumpListTask, error) { return nil, nil }

// DeleteJumpList is a no-op.
func DeleteJumpList(appID string) error { return nil }
