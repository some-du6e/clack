package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nativeFetches are what a backend may never call: a native media stack
// given a URL fetches it itself, with its own TLS (Grid ends the session
// everywhere when the cookie comes over a foreign ClientHello: SECURITY.md
// C-19), around Download's guards; and message content never touches the
// disk.
var nativeFetches = []string{
	"SetSource(",                    // IMFMediaEngine's URL source
	"CreateObjectFromURL",           // IMFSourceResolver
	"MFCreateFile",                  // a file byte stream
	"MFCreateTempFile",              //
	`URLWithString:@"http`,          // an http(s) AVURLAsset
	"AVURLAssetHTTPCookiesKey",      // the cookie handed to Apple's stack
	"AVURLAssetHTTPHeaderFieldsKey", // private, the same
	"NSURLSession",                  //
	"souphttpsrc",                   // GStreamer's HTTP source
	"GST_PLAY_FLAG_DOWNLOAD",        // playbin's download buffering (a temp file)
	"os.Create",                     // also os.CreateTemp
	"os.WriteFile",                  //
	"os.OpenFile",                   //
}

// scanNative is each of nativeFetches src holds, as "pattern".
func scanNative(src string) []string {
	var hits []string
	for _, p := range nativeFetches {
		if strings.Contains(src, p) {
			hits = append(hits, p)
		}
	}
	return hits
}

// The media packages (this one and its backends: mf, avf, …) never hand a
// native media stack a URL and never write a file: TestOnlyTLSFPDials sees
// only Go code, so this reads their Go, C and Objective-C.
func TestNativeNeverFetches(t *testing.T) {
	files := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".c", ".m", ".h":
		default:
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for _, h := range scanNative(string(b)) {
			t.Errorf("%s: %s (a native media stack reads only what Go hands it, from memory)", path, h)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 5 {
		t.Fatalf("scanned only %d files", files)
	}
	for _, src := range []string{
		`hr = IMFMediaEngine_SetSource(engine, url);`,
		`[NSURL URLWithString:@"https://files.slack.com/x.mp4"]`,
		`@{AVURLAssetHTTPCookiesKey: cookies}`,
		`f, _ := os.CreateTemp("", "v")`,
		`g_object_set(src, "location", u, NULL); /* souphttpsrc */`,
	} {
		if len(scanNative(src)) == 0 {
			t.Errorf("the scan misses %q", src)
		}
	}
	if h := scanNative(`IMFMediaEngineEx_SetSourceFromByteStream(ex, stream, url);`); len(h) != 0 {
		t.Errorf("a byte stream source counted as a URL source: %v", h)
	}
}
