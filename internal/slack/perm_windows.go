//go:build windows

package slack

import (
	"golang.org/x/sys/windows"
)

// ownerOnly replaces path's ACL with a protected one (no inherited
// entries) granting full control to the current user only. On Windows Go's
// 0600 mode is ignored, so without this creds.json inherits the profile
// directory's ACL (usually also SYSTEM and Administrators; on some setups
// more).
func ownerOnly(path string, dir bool) error {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	ace := "(A;;FA;;;" + tu.User.Sid.String() + ")"
	if dir {
		ace = "(A;OICI;FA;;;" + tu.User.Sid.String() + ")" // inherited by new files
	}
	sd, err := windows.SecurityDescriptorFromString("D:P" + ace)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// RestrictFile gives path an owner-only ACL.
func RestrictFile(path string) error { return ownerOnly(path, false) }

// RestrictDir gives dir (the config directory) an owner-only, inheritable ACL.
func RestrictDir(dir string) error { return ownerOnly(dir, true) }
