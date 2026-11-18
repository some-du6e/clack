package media

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// nativeFetches are what a backend may never call: a native media stack
// given a URL fetches it itself, with its own TLS (Grid ends the session
// everywhere when the cookie comes over a foreign ClientHello: SECURITY.md
// C-19), around Download's guards; and message content never touches the
// disk.
var nativeFetches = []string{
	// Media Foundation's URL sources and URL readers
	"SetSource(", "_SetSource(", "SetSourceElements(", "AddElement(", "CreateObjectFromURL",
	"MFCreateSourceReaderFromURL", "MFCreateSinkWriterFromURL", "MFCreateFile", "MFCreateTempFile",
	// Apple's fetches, and the cookie handed to them
	`URLWithString:@"http`, "AVURLAssetHTTPCookiesKey", "AVURLAssetHTTPHeaderFieldsKey", "NSURLSession",
	"AVAssetDownloadURLSession", "NSURLConnection", "dataWithContentsOfURL:", "playerWithURL:", "playerItemWithURL:",
	// GStreamer's
	"souphttpsrc", "GST_PLAY_FLAG_DOWNLOAD",
	// Windows' own HTTP
	"WinHttpOpen", "InternetOpenUrl", "URLDownloadToFile",
	// files: message content never touches the disk
	"os.Create", "os.WriteFile", "os.OpenFile", "os.MkdirTemp", "ioutil.TempFile", "ioutil.WriteFile",
	"fopen(", "CreateFileW", "CreateFile2", "SHCreateStreamOnFile", "writeToFile:", "writeToURL:",
	"NSFileManager", "NSTemporaryDirectory",
}

// guarded are calls allowed only in a file that checks, at run time, that
// the URL it hands the native stack is a made-up clack-media one.
var guarded = map[string]string{
	"URLAssetWithURL:": `isEqualToString:@"clack-media"`,
}

// vtableAllow is every COM method the media packages' C may call through
// a vtable: a URL source under another name (a renamed slot) fails.
var vtableAllow = map[string]bool{
	"QueryInterface": true, "AddRef": true, "Release": true,
	"Play": true, "Pause": true, "SetCurrentTime": true, "SetVolume": true, "SetMuted": true, "SetLoop": true,
	"SetDefaultPlaybackRate": true, "SetPlaybackRate": true, "CurTime": true, "GetDuration": true, "IsPaused": true,
	"IsEnded": true, "IsSeeking": true, "GetReadyState": true, "GetError": true, "GetErrorCode": true,
	"GetExtendedErrorCode": true, "GetNativeVideoSize": true, "HasVideo": true, "HasAudio": true,
	"GetNumberOfStreams": true, "GetStreamAttribute": true, "Shutdown": true, "TransferVideoFrame": true,
	"OnVideoStreamTick": true, "SetSourceFromByteStream": true, "CreateInstance": true, "SetMultithreadProtected": true,
}

var vtableCall = regexp.MustCompile(`lpVtbl->([A-Za-z0-9_]+)\(`)

// scanNative is what src calls that it may not: each of nativeFetches it
// holds, a guarded call without its check, a vtable method off the
// allowlist.
func scanNative(src string) []string {
	var hits []string
	for _, p := range nativeFetches {
		if strings.Contains(src, p) {
			hits = append(hits, p)
		}
	}
	for call, check := range guarded {
		if strings.Contains(src, call) && !strings.Contains(src, check) {
			hits = append(hits, call+" without the clack-media check")
		}
	}
	for _, m := range vtableCall.FindAllStringSubmatch(src, -1) {
		if !vtableAllow[m[1]] {
			hits = append(hits, "lpVtbl->"+m[1])
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
		case ".go", ".c", ".m", ".h", ".cc", ".cpp", ".cxx", ".mm", ".hpp", ".swift":
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
		`hr = e->lpVtbl->SetSrcURL(e, url);`,
		`IMFMediaEngine_SetSourceElements(engine, elems);`,
		`_asset = [AVURLAsset URLAssetWithURL:[NSURL URLWithString:s] options:nil];`,
		`FILE *f = fopen(path, "wb");`,
		`[data writeToFile:path atomically:YES];`,
	} {
		if len(scanNative(src)) == 0 {
			t.Errorf("the scan misses %q", src)
		}
	}
	if h := scanNative(`IMFMediaEngineEx_SetSourceFromByteStream(ex, stream, url);`); len(h) != 0 {
		t.Errorf("a byte stream source counted as a URL source: %v", h)
	}
}
