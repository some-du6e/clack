package popups

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/some-du6e/clack/internal/core"
)

// SaveDownload fetches src (a Slack file URL, with the session) into the
// Downloads folder as name, like DownloadFile, but fresh every time (an
// export changes as its list does; core.Fetch would hand back its cached
// copy) and leaving the toast to the caller (a list's download says what
// Slack says: "CSV downloaded successfully"). done runs on the UI
// goroutine with the saved file's path or the error.
func SaveDownload(h Host, src, name string, done func(path string, err error)) {
	run(h, func(ctx context.Context) (string, error) {
		c := h.Core()
		if c == nil || !c.C.CredentialsOK(src) {
			return "", errors.New("not a Slack file")
		}
		resp, err := c.C.Download(ctx, src)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("http %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, core.MaxFetch+1))
		if err != nil {
			return "", err
		}
		if len(data) > core.MaxFetch {
			return "", errors.New("file too large")
		}
		return lbSave(name, src, data)
	}, done)
}
