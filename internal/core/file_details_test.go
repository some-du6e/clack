package core

import (
	"context"
	"strings"
	"testing"
)

// FileDetails' files.info is fetchFileById's {file, page 1, count 500,
// truncate true, public_shared false, skip_shares true}; the answer's alt
// text, username and canvas creator come along; Slack's refusals become
// its file stubs (file_deleted a tombstone, file_not_found not_found),
// and any other error stays one.
func TestFileDetailsFetch(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	f.reply["files.info"] = `{"ok":true,"file":{"id":"F0DETAILS01","name":"cat.png","title":"A cat","mimetype":"image/png","filetype":"png",` +
		`"user":"U1","created":1762600000,"timestamp":1762600000,"thumb_360":"https://files.example/cat_360.png","thumb_360_w":360,"thumb_360_h":240,` +
		`"original_w":1200,"original_h":800,"alt_txt":"a cat on a mat","is_modified_by_ai":true,"username":"cat-uploader"},"comments":[]}`
	info, err := a.FileDetails(ctx, "F0DETAILS01")
	if err != nil {
		t.Fatalf("FileDetails: %v", err)
	}
	v := f.last("files.info")
	want := map[string]string{"file": "F0DETAILS01", "page": "1", "count": "500", "truncate": "true", "public_shared": "false", "skip_shares": "true"}
	for k, w := range want {
		if v.Get(k) != w {
			t.Errorf("files.info %s=%q, want %q (fetchFileById's args); sent %v", k, v.Get(k), w, v)
		}
	}
	for _, k := range []string{"include_transcription", "reason"} {
		if v.Has(k) {
			t.Errorf("files.info sent %s=%q, which FileDetails' fetch leaves out", k, v.Get(k))
		}
	}
	if info.File == nil || info.File.ID != "F0DETAILS01" || info.Mode != "" {
		t.Fatalf("info %+v, want the file", info)
	}
	if info.AltText != "a cat on a mat" || !info.AltByAI || info.Username != "cat-uploader" {
		t.Errorf("alt %q (AI %v), username %q; want the answer's alt_txt, is_modified_by_ai and username", info.AltText, info.AltByAI, info.Username)
	}
	if rf := info.RichFile("F0DETAILS01"); rf == nil || !rf.IsImage || rf.Title != "A cat" || rf.User != "U1" || rf.Created != 1762600000 {
		t.Errorf("RichFile %+v, want the message's conversion of the image", rf)
	}

	for code, mode := range map[string]string{"file_deleted": "tombstone", "file_not_found": "not_found", "access_denied": "access_denied",
		"hidden_by_limit": "hidden_by_limit", "slack_connect_canvas_sharing_blocked": "canvas_slack_connect_blocked"} {
		// a file each: the client holds back a request identical to one
		// that just failed (slack-client-identical-failure backoff)
		id := "F0STUB" + strings.ToUpper(strings.ReplaceAll(code, "_", ""))[:6]
		f.reply["files.info"] = `{"ok":false,"error":"` + code + `"}`
		info, err := a.FileDetails(ctx, id)
		if err != nil || info.File != nil || info.Mode != mode {
			t.Errorf("%s: info %+v, err %v; want the %s stub", code, info, err, mode)
		}
		if rf := info.RichFile(id); rf.ID != id || rf.Mode != mode {
			t.Errorf("%s: RichFile %+v, want the id and mode %s", code, rf, mode)
		}
	}
	f.reply["files.info"] = `{"ok":false,"error":"some_other_error"}`
	if _, err := a.FileDetails(ctx, "F0DETAILS03"); err == nil {
		t.Error("files.info refused with another code reported no error")
	}
}
