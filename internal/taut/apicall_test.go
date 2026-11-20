package taut

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A plugin's apiCall settles the way Slack's API request does (Slack web
// client build 35b42de8b870: the apiCall thunk client-boot 0hZc, its
// SlackApiRequest QCxN, BaseRequest Umoo, the API queue 7quX, SlackError
// 9BpC). Clack's client talks to internal/fakeslack, whose hook answers
// chat.postMessage the way Slack can; a Taut plugin dispatches the thunk
// and reports what its catch sees.

// apiProbePlugin dispatches a thunk for the test and reports how it
// settled: the response, or the error's class, name, message, own keys
// (undefined values as "<undefined>") and whether it has data. It also
// records every apiCall its patch sees.
const apiProbePlugin = `class APIProbe extends TautPlugin {
  static id = 'APIProbe'
  static pluginName = 'API probe'
  static description = 'calls Slack the way plugins do and reports what its catch sees'
  static authors = ['clack']
  static defaultConfig = { enabled: false }
  start() {
    const api = this.api
    globalThis.__apiSeen = []
    api.redux.patchThunk('apiCall', (original) => (options, ...rest) => {
      globalThis.__apiSeen.push({ method: options?.method, expedited: options?.EXPEDITED === true })
      return original(options, ...rest)
    })
    globalThis.__apiProbe = async (thunk, options) => {
      try {
        const res = await api.redux.dispatchThunk(thunk, options)
        return { settled: 'resolved', keys: Object.keys(res), res }
      } catch (err) {
        const own = {}
        for (const k of Object.keys(err)) own[k] = err[k] === undefined ? '<undefined>' : err[k]
        return { settled: 'rejected', isError: err instanceof Error, ctor: err?.constructor?.name, name: err?.name,
          message: err?.message, keys: Object.keys(err), hasData: 'data' in err, own }
      }
    }
  }
  stop() {
    delete globalThis.__apiProbe
  }
}
return APIProbe`

type apiSettled struct {
	Settled string         `json:"settled"`
	Keys    []string       `json:"keys"`
	Res     map[string]any `json:"res"`
	IsError bool           `json:"isError"`
	Ctor    string         `json:"ctor"`
	Name    any            `json:"name"`
	Message string         `json:"message"`
	HasData bool           `json:"hasData"`
	Own     map[string]any `json:"own"`
}

type apiCallEnv struct {
	*harness
	fs *fakeslack.Server
	// answer, when set, answers chat.postMessage (fakeslack's own handler
	// otherwise); posts counts the requests that reached the fake
	answer atomic.Pointer[func(form url.Values) any]
	posts  atomic.Int64
}

// newAPICallEnv is the harness over fakeslack with the probe plugin
// running; Slack's retry schedule runs in hundredths (10 ms a second) and
// a request times out after timeout.
func newAPICallEnv(t *testing.T, timeout time.Duration) *apiCallEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	old := slack.RetryUnit
	slack.RetryUnit = 10 * time.Millisecond
	t.Cleanup(func() { slack.RetryUnit = old })
	c := slack.New(fs.Creds())
	c.HTTP.Timeout = timeout
	e := &apiCallEnv{fs: fs}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "chat.postMessage" {
			return nil, false
		}
		e.posts.Add(1)
		if f := e.answer.Load(); f != nil {
			return (*f)(form), true
		}
		return nil, false
	})
	e.harness = newHarnessOn(t, newFakeAPI(t), c)
	e.install(pluginJS(apiProbePlugin), true)
	eventually(t, renderWait(), "the probe plugin", func() bool {
		var up bool
		e.jsInto(`return typeof globalThis.__apiProbe === 'function'`, &up)
		return up
	})
	return e
}

func (e *apiCallEnv) answerWith(f func(form url.Values) any) { e.answer.Store(&f) }

// probe dispatches thunk with options from the plugin.
func (e *apiCallEnv) probe(thunk string, options map[string]any) apiSettled {
	e.t.Helper()
	b, err := json.Marshal(options)
	if err != nil {
		e.t.Fatal(err)
	}
	var out apiSettled
	e.jsInto(fmt.Sprintf("return await globalThis.__apiProbe(%q, %s)", thunk, b), &out)
	return out
}

func postOptions(text string) map[string]any {
	return map[string]any{"method": "chat.postMessage", "args": map[string]any{"channel": "C0FAKE00001", "text": text}}
}

// requestID is Slack's generateId (client-boot EVbb) over Clack's
// bootData.version_uid ("clack"): the request's _x_id.
var requestID = regexp.MustCompile(`^clack-\d+\.\d{3}$`)

func sameAnswer(t *testing.T, what string, got any, want []byte) {
	t.Helper()
	var w any
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	g, _ := json.Marshal(got)
	var gv any
	_ = json.Unmarshal(g, &gv)
	if !reflect.DeepEqual(gv, w) {
		t.Errorf("%s = %s, want the server's answer %s", what, g, want)
	}
}

func eqKeys(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s keys %q, want %q", what, got, want)
	}
}

// Slack's SlackError (9BpC) after _onNotOk: its class fields in order,
// then name and status.
var slackErrorKeys = []string{"args", "data", "apiErrorCode", "id", "method", "subType", "type", "attempts", "doNotFetchAuth", "name", "status"}

// TestAPICallNotOkLikeSlack: an {"ok":false} answer rejects with Slack's
// SlackError: err.data is the answer's JSON whole (errors, warning,
// response_metadata.messages: which block chat.postMessage refused), its
// message `<request id> <method> not ok: <code>`, its name the code.
func TestAPICallNotOkLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 10*time.Second)
	body := []byte(`{"ok":false,"error":"invalid_blocks","errors":["downloading image failed [json-pointer:/blocks/2/accessory/image_url]"],` +
		`"warning":"missing_charset","response_metadata":{"messages":["[ERROR] downloading image failed [json-pointer:/blocks/2/accessory/image_url]"],"warnings":["missing_charset"]}}`)
	e.answerWith(func(url.Values) any { return body })
	got := e.probe("apiCall", postOptions("not ok"))
	if got.Settled != "rejected" {
		t.Fatalf("apiCall %s, want rejected: %+v", got.Settled, got)
	}
	sameAnswer(t, "err.data", got.Own["data"], body)
	id, _ := got.Own["id"].(string)
	if !requestID.MatchString(id) {
		t.Errorf("err.id %q, want Slack's generateId(version_uid)", id)
	}
	if want := id + " chat.postMessage not ok: invalid_blocks"; got.Message != want {
		t.Errorf("err.message %q, want %q", got.Message, want)
	}
	if !got.IsError || got.Ctor != "SlackError" || got.Name != "invalid_blocks" {
		t.Errorf("err is %s (Error: %v) named %v, want a SlackError named invalid_blocks", got.Ctor, got.IsError, got.Name)
	}
	eqKeys(t, "err", got.Keys, slackErrorKeys)
	for k, want := range map[string]any{"apiErrorCode": "invalid_blocks", "method": "chat.postMessage", "status": 200.0, "attempts": 1.0,
		"subType": "<undefined>", "type": "<undefined>", "doNotFetchAuth": "<undefined>"} {
		if got.Own[k] != want {
			t.Errorf("err.%s = %v, want %v", k, got.Own[k], want)
		}
	}
	sameAnswer(t, "err.args", got.Own["args"], []byte(`{"token":"[redacted]","channel":"C0FAKE00001","text":"not ok"}`))
	if n := e.posts.Load(); n != 1 {
		t.Errorf("%d requests, want 1: Slack's queue doesn't try a refused message again", n)
	}

	// the backoff guard answers the same request again at once, with the
	// same answer
	again := e.probe("apiCall", postOptions("not ok"))
	sameAnswer(t, "a repeat's err.data", again.Own["data"], body)
	if again.Name != "invalid_blocks" || again.Ctor != "SlackError" {
		t.Errorf("a repeat rejected with %s %v", again.Ctor, again.Name)
	}

	// apiCallExpedited is apiCall with EXPEDITED (0hZc T): apiCall's
	// patches see it, and it rejects the same way
	exp := e.probe("apiCallExpedited", postOptions("expedited"))
	sameAnswer(t, "apiCallExpedited's err.data", exp.Own["data"], body)
	if exp.Name != "invalid_blocks" {
		t.Errorf("apiCallExpedited rejected with %v", exp.Name)
	}
	var seen []struct {
		Method    string `json:"method"`
		Expedited bool   `json:"expedited"`
	}
	e.jsInto(`return globalThis.__apiSeen`, &seen)
	if len(seen) != 3 || !seen[2].Expedited || seen[2].Method != "chat.postMessage" || seen[0].Expedited {
		t.Errorf("apiCall's patch saw %+v, want the two calls and then the expedited one", seen)
	}
}

// TestAPICallResolvesLikeSlack: the response is SlackApiRequest's: the
// request ids, args and method ahead of the answer's own fields.
func TestAPICallResolvesLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 10*time.Second)
	got := e.probe("apiCall", postOptions("resolves"))
	if got.Settled != "resolved" {
		t.Fatalf("apiCall %s: %+v", got.Settled, got)
	}
	eqKeys(t, "the response", got.Keys[:min(4, len(got.Keys))], []string{"request_id", "client_request_id", "args", "method"})
	if got.Res["ok"] != true || got.Res["ts"] == nil || got.Res["request_id"] != nil {
		t.Errorf("response %v", got.Res)
	}
	if id, _ := got.Res["client_request_id"].(string); !requestID.MatchString(id) {
		t.Errorf("client_request_id %q", id)
	}
	sameAnswer(t, "response.args", got.Res["args"], []byte(`{"token":"[redacted]","channel":"C0FAKE00001","text":"resolves"}`))
}

// TestAPICallRateLimitedLikeSlack: {"ok":false} with a rate-limit code
// fails at once (the queue's dummy request): Slack's error, as
// BaseRequest.throwError builds it over the SlackError, with status
// RATELIMITED, the answer as response and retryAfter 60 s and its jitter.
func TestAPICallRateLimitedLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 10*time.Second)
	body := []byte(`{"ok":false,"error":"ratelimited"}`)
	e.answerWith(func(url.Values) any { return body })
	got := e.probe("apiCall", postOptions("rate limited"))
	if got.Settled != "rejected" || got.Ctor != "Error" || got.Name != "ratelimited" {
		t.Fatalf("apiCall %s %s %v, want rejected with an Error named ratelimited: %+v", got.Settled, got.Ctor, got.Name, got)
	}
	sameAnswer(t, "err.data", got.Own["data"], body)
	id, _ := got.Own["id"].(string)
	if want := id + " chat.postMessage not ok: ratelimited"; !requestID.MatchString(id) || got.Message != want {
		t.Errorf("err.message %q, want %q", got.Message, want)
	}
	eqKeys(t, "err", got.Keys, append(append([]string(nil), slackErrorKeys...), "response", "retryAfter"))
	if got.Own["status"] != "RATELIMITED" || got.Own["response"] != string(body) || got.Own["attempts"] != 1.0 {
		t.Errorf("err status %v response %v attempts %v", got.Own["status"], got.Own["response"], got.Own["attempts"])
	}
	// _getRetryAfterMs: 60 s plus getBackoffJittered({attempts: 1}): 0…8
	if ra, _ := got.Own["retryAfter"].(float64); ra < 60000 || ra > 68000 || ra != float64(int(ra)/1000*1000) {
		t.Errorf("err.retryAfter %v, want 60000…68000 in whole seconds", got.Own["retryAfter"])
	}
	if n := e.posts.Load(); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

// TestAPICallRetriesLikeSlack: what Slack's queue tries again (an HTTP
// 429, internal_error, no answer at all, a timeout) goes out 3 times; what
// it doesn't (a 500, a page that isn't JSON) once. Each rejects with the
// error BaseRequest builds for its last try.
func TestAPICallRetriesLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 500*time.Millisecond)
	transportKeys := []string{"method", "status", "response", "retryAfter", "attempts"}
	check := func(name string, got apiSettled, message string, status any, response string, attempts float64, raLo, raHi float64) {
		t.Helper()
		if got.Settled != "rejected" || got.Ctor != "Error" || got.Message != message {
			t.Errorf("%s: %s %s %q, want rejected with Error %q", name, got.Settled, got.Ctor, got.Message, message)
		}
		if got.Own["status"] != status || got.Own["response"] != response || got.Own["attempts"] != attempts || got.Own["method"] != "chat.postMessage" {
			t.Errorf("%s: status %v response %q attempts %v method %v, want %v %q %v", name, got.Own["status"], got.Own["response"], got.Own["attempts"], got.Own["method"], status, response, attempts)
		}
		if ra, _ := got.Own["retryAfter"].(float64); ra < raLo || ra > raHi {
			t.Errorf("%s: retryAfter %v, want %v…%v", name, got.Own["retryAfter"], raLo, raHi)
		}
	}

	// an HTTP 429 with Retry-After: tried 3 times, a second apart
	e.posts.Store(0)
	limited := `{"ok":false,"error":"ratelimited"}`
	e.answerWith(func(url.Values) any {
		return fakeslack.Reply{Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"1"}}, Body: []byte(limited)}
	})
	got := e.probe("apiCall", postOptions("429"))
	check("429", got, "Request completed but is not OK", 429.0, limited, 3, 1000, 1000)
	eqKeys(t, "429 err", got.Keys, transportKeys)
	if got.HasData {
		t.Errorf("429: err.data %v: Slack's HTTP errors have none", got.Own["data"])
	}
	if n := e.posts.Load(); n != 3 {
		t.Errorf("429: %d requests, want 3", n)
	}

	// a 500 page: final at once, retryAfter 30 s and its jitter
	e.posts.Store(0)
	page := "<html><body>Internal Server Error</body></html>"
	e.answerWith(func(url.Values) any {
		return fakeslack.Reply{Status: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"text/html"}}, Body: []byte(page)}
	})
	got = e.probe("apiCall", postOptions("500"))
	check("500", got, "Request completed but is not OK", 500.0, page, 1, 30000, 38000)
	if n := e.posts.Load(); n != 1 {
		t.Errorf("500: %d requests, want 1", n)
	}

	// internal_error: tried 3 times; Slack's error over the SlackError,
	// status 0 (throwError's CLIENT_UNKNOWN)
	e.posts.Store(0)
	internal := `{"ok":false,"error":"internal_error","warning":"retry"}`
	e.answerWith(func(url.Values) any { return []byte(internal) })
	got = e.probe("apiCall", postOptions("internal"))
	id, _ := got.Own["id"].(string)
	if got.Settled != "rejected" || got.Name != "internal_error" || got.Message != id+" chat.postMessage not ok: internal_error" {
		t.Errorf("internal_error: %s %v %q", got.Settled, got.Name, got.Message)
	}
	sameAnswer(t, "internal_error's err.data", got.Own["data"], []byte(internal))
	if got.Own["status"] != 0.0 || got.Own["attempts"] != 3.0 || got.Own["response"] != internal {
		t.Errorf("internal_error: status %v attempts %v response %v", got.Own["status"], got.Own["attempts"], got.Own["response"])
	}
	if n := e.posts.Load(); n != 3 {
		t.Errorf("internal_error: %d requests, want 3", n)
	}

	// a 200 that isn't JSON: SlackApiRequest's _parse_error, final
	e.posts.Store(0)
	notJSON := "<html>Slack is down</html>"
	e.answerWith(func(url.Values) any { return []byte(notJSON) })
	got = e.probe("apiCall", postOptions("not json"))
	id, _ = got.Own["id"].(string)
	if got.Ctor != "SlackError" || got.Name != "_parse_error" || got.Message != id+" chat.postMessage not ok: _parse_error" {
		t.Errorf("not JSON: %s %v %q", got.Ctor, got.Name, got.Message)
	}
	data, _ := got.Own["data"].(map[string]any)
	if msg, _ := data["parse_error_message"].(string); data["ok"] != false || data["error"] != "_parse_error" || msg == "" || data["payload_size"] != float64(len(notJSON)) {
		t.Errorf("not JSON: err.data %v", data)
	}
	if n := e.posts.Load(); n != 1 {
		t.Errorf("not JSON: %d requests, want 1", n)
	}

	// no answer (the connection dropped): tried 3 times
	e.answer.Store(nil)
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{HangUp: true})
	got = e.probe("apiCall", postOptions("hang up"))
	check("no answer", got, "Request did not complete.", 0.0, "", 3, 1000, 39000)
	eqKeys(t, "no answer err", got.Keys, transportKeys)
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{})

	// past the request timeout: tried 3 times
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{Delay: 2 * time.Second})
	got = e.probe("apiCall", postOptions("timeout"))
	check("timeout", got, "Request timed out after 60 seconds.", "TIMEOUT", "", 3, 1000, 39000)
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{})
}
