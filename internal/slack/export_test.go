package slack

import (
	"io"
	"net/http"
)

// SetSignInTransport points the sign-in client (magicClient) at rt for the
// external tests (cookie_signin_fake_test.go: fakeslack imports slack, so
// those tests are package slack_test) and returns the undo.
func SetSignInTransport(rt http.RoundTripper) func() {
	prev := magicTransport
	magicTransport = rt
	return func() { magicTransport = prev }
}

// ReadHistoryStreamed is HistoryPage.readStreamed (history_stream.go) for
// the external tests (history_stream_test.go).
func ReadHistoryStreamed(p *HistoryPage, r io.Reader, hint int64) (bool, []byte, error) {
	return p.readStreamed(r, hint)
}

// SetStreamChunks sets the sizes a streamed body of unknown length is read
// in (streamChunks: the first, doubling to the last), so the tests put
// chunk boundaries on every byte; restore puts them back.
func SetStreamChunks(first, last int) (restore func()) {
	old := streamChunks
	streamChunks = [2]int{first, last}
	return func() { streamChunks = old }
}
