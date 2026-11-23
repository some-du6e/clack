//go:build windows

package slack

import (
	"encoding/binary"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// osMimeType is platform_mime_util_win.cc's
// GetPlatformMimeTypeFromExtension: HKEY_CLASSES_ROOT\.<ext>'s "Content
// Type" (HKCR is HKCU\Software\Classes over HKLM's; key names ignore case),
// none when it's missing or "".
func osMimeType(ext string) (string, bool) {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, "."+ext, registry.READ)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v := regKeyString(k, "Content Type")
	return v, v != ""
}

// regKeyString is base::win::RegKey::ReadValue(name, std::wstring*): a
// REG_SZ, or a REG_EXPAND_SZ with its %variables% expanded, each of at most
// 1024 UTF-16 units with its NUL (kMaxStringLength, before and after
// expansion), read up to its first NUL; "" for any other type, a longer
// value or a failed read.
func regKeyString(k registry.Key, name string) string {
	const maxLen = 1024
	buf := make([]byte, 2*maxLen)
	n, typ, err := k.GetValue(name, buf)
	if err != nil {
		return "" // ERROR_MORE_DATA past 2048 bytes, or none
	}
	raw := make([]uint16, n/2)
	for i := range raw {
		raw[i] = binary.LittleEndian.Uint16(buf[2*i:])
	}
	switch typ {
	case registry.SZ:
		return windows.UTF16ToString(raw)
	case registry.EXPAND_SZ:
		src := append(raw, 0) // ExpandEnvironmentStrings reads to the first NUL
		dst := make([]uint16, maxLen)
		m, err := windows.ExpandEnvironmentStrings(&src[0], &dst[0], maxLen)
		if err != nil || m == 0 || m > maxLen {
			return ""
		}
		return windows.UTF16ToString(dst[:m])
	}
	return ""
}
