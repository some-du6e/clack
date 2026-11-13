//go:build windows

package richclip

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"
)

// readFormat reads clipboard format f's bytes (nil when absent).
func readFormat(t *testing.T, f uintptr) []byte {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rcCloseClipboard.Call()
	h, _, _ := rcGetClipboardData.Call(f)
	if h == 0 {
		return nil
	}
	n, _, _ := rcGlobalSize.Call(h)
	p, _, _ := rcGlobalLock.Call(h)
	if p == 0 || n == 0 {
		return nil
	}
	defer rcGlobalUnlock.Call(h)
	b := make([]byte, n)
	rcRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
	return b
}

// The real clipboard (CI's windows-applog job): one copy puts the text
// as CF_UNICODETEXT, the HTML as "HTML Format" and Slack's custom types
// in "Chromium Web Custom MIME Data Format", where Slack's desktop app
// (Chromium) reads them; ReadCustom takes the custom types back.
func TestWinRichCopy(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard; set CLACK_WINDOWS_CLIPBOARD_TESTS=1 (CI's windows-applog job)")
	}
	c := Copy{Text: "ping @alice ✓", HTML: `<a href="/team/U1">@alice</a>`,
		Custom: map[string]string{"slack/html": `<meta charset="utf-8"><a data-stringify-type="mention" data-stringify-id="U1" data-stringify-label="@alice">@alice</a>`,
			"slack/texty": `{"ops":[{"insert":"@alice"}]}`}}
	if err := Write(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	tb := readFormat(t, cfUnicodeText)
	u := make([]uint16, len(tb)/2)
	for i := range u {
		u[i] = uint16(tb[2*i]) | uint16(tb[2*i+1])<<8
	}
	if got := strings.TrimRight(string(utf16.Decode(u)), "\x00"); got != c.Text {
		t.Errorf("CF_UNICODETEXT %q", got)
	}
	if h := string(readFormat(t, register(fmtHTML))); !strings.Contains(h, "<!--StartFragment-->"+c.HTML+"<!--EndFragment-->") || !strings.HasPrefix(h, "Version:0.9") {
		t.Errorf("HTML Format %q", h)
	}
	got, err := ReadCustom(context.Background())
	if err != nil || !reflect.DeepEqual(got, c.Custom) {
		t.Errorf("custom data %v %q", err, got)
	}
}
