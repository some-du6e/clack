package slack

import "net/http"

// SetSignInTransport points the sign-in client (magicClient) at rt for the
// external tests (cookie_signin_fake_test.go: fakeslack imports slack, so
// those tests are package slack_test) and returns the undo.
func SetSignInTransport(rt http.RoundTripper) func() {
	prev := magicTransport
	magicTransport = rt
	return func() { magicTransport = prev }
}
