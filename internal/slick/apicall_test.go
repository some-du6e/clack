package slick

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A Slick plugin's apiCall settles the way Slack's API request does (Slack
// web client build 35b42de8b870: apiCall client-boot 0hZc, SlackApiRequest
// QCxN, BaseRequest Umoo, SlackError 9BpC): over internal/fakeslack, the
// plugin's catch gets Slack's error, err.data the answer's JSON whole.
// internal/taut's TestAPICall* cover every way a request ends; this is the
// same thunk through Slick's api.redux.dispatchThunk.

const apiProbeSlick = `// slick-plugin {"id":"APIProbe","name":"API probe","description":"test"}
(globalThis.__slickPlugins ??= []).push((require) => {
const { SlickPlugin } = require("$slick");
class APIProbe extends SlickPlugin {
  static id = "APIProbe";
  static pluginName = "API probe";
  static description = "test";
  start() {
    const api = this.api;
    globalThis.__userAPIProbe = (method, params) => this.userAPIProbe(method, params);
    globalThis.__thunkNames = () => {
      try {
        const names = api.redux.thunkNames();
        return { names, defined: names.every((name) => typeof api.redux.getThunkCreator(name) === "function") };
      } catch (err) {
        return { error: String(err) };
      }
    };
    globalThis.__apiProbe = async (thunk, options) => {
      try {
        const res = await api.redux.dispatchThunk(thunk, options);
        return { settled: "resolved", keys: res && typeof res === "object" ? Object.keys(res) : [], res };
      } catch (err) {
        const own = {};
        for (const k of Object.keys(err)) own[k] = err[k] === undefined ? "<undefined>" : err[k];
        return { settled: "rejected", ctor: err?.constructor?.name, name: err?.name, message: err?.message, keys: Object.keys(err), own };
      }
    };
  }
  stop() {
    delete globalThis.__apiProbe;
  }
  async userAPIProbe(method, params) {
    try {
      return { settled: "resolved", res: await this.api.userAPI(method, params) };
    } catch (err) {
      return { settled: "rejected", ctor: err?.constructor?.name, message: err?.message };
    }
  }
}
return APIProbe;
});
`

type slickSettled struct {
	Settled string         `json:"settled"`
	Keys    []string       `json:"keys"`
	Res     map[string]any `json:"res"`
	Ctor    string         `json:"ctor"`
	Name    any            `json:"name"`
	Message string         `json:"message"`
	Own     map[string]any `json:"own"`
}

func TestSlickAPICallErrorsLikeSlack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	var answer atomic.Pointer[func(form url.Values) any]
	var posts atomic.Int64
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		f := answer.Load()
		if method != "chat.postMessage" || f == nil {
			return nil, false
		}
		posts.Add(1)
		return (*f)(form), true
	})
	answerWith := func(f func(form url.Values) any) { answer.Store(&f) }
	h := newHarnessOn(t, newFakeAPI(t), slack.New(fs.Creds()))
	mustImport(t, h, apiProbeSlick)
	if err := h.s.SetEnabled(h.ctx(), "APIProbe", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the probe plugin", func() bool { return h.jsBool(`return typeof globalThis.__apiProbe === 'function'`) })
	probe := func(text string) slickSettled {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"method": "chat.postMessage", "args": map[string]any{"channel": "C0FAKE00001", "text": text}})
		var out slickSettled
		h.jsInto(fmt.Sprintf("return await globalThis.__apiProbe('apiCall', %s)", b), &out)
		return out
	}
	same := func(what string, got any, want []byte) {
		t.Helper()
		var w, g any
		_ = json.Unmarshal(want, &w)
		b, _ := json.Marshal(got)
		_ = json.Unmarshal(b, &g)
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s = %s, want the server's answer %s", what, b, want)
		}
	}
	idRE := regexp.MustCompile(`^clack-\d+\.\d{3}$`)

	// chat.postMessage refusing a block: which one is in err.data
	body := []byte(`{"ok":false,"error":"invalid_blocks","errors":["downloading image failed [json-pointer:/blocks/2/accessory/image_url]"],` +
		`"response_metadata":{"messages":["[ERROR] downloading image failed [json-pointer:/blocks/2/accessory/image_url]"]}}`)
	answerWith(func(url.Values) any { return body })
	got := probe("not ok")
	if got.Settled != "rejected" || got.Ctor != "SlackError" || got.Name != "invalid_blocks" {
		t.Fatalf("apiCall %s with %s %v, want a SlackError named invalid_blocks: %+v", got.Settled, got.Ctor, got.Name, got)
	}
	same("err.data", got.Own["data"], body)
	id, _ := got.Own["id"].(string)
	if want := id + " chat.postMessage not ok: invalid_blocks"; !idRE.MatchString(id) || got.Message != want {
		t.Errorf("err.message %q, want %q", got.Message, want)
	}
	if got.Own["apiErrorCode"] != "invalid_blocks" || got.Own["status"] != 200.0 || got.Own["attempts"] != 1.0 || got.Own["method"] != "chat.postMessage" {
		t.Errorf("err %v", got.Own)
	}

	// rate limited: fails at once, status RATELIMITED, the answer kept
	limited := []byte(`{"ok":false,"error":"ratelimited"}`)
	answerWith(func(url.Values) any { return limited })
	posts.Store(0)
	got = probe("rate limited")
	if got.Settled != "rejected" || got.Ctor != "Error" || got.Name != "ratelimited" || got.Own["status"] != "RATELIMITED" || got.Own["response"] != string(limited) {
		t.Errorf("rate limited: %+v", got)
	}
	same("rate limited err.data", got.Own["data"], limited)
	if n := posts.Load(); n != 1 {
		t.Errorf("rate limited: %d requests, want 1", n)
	}

	// an HTTP error: BaseRequest's own error, the page as response, no data
	page := "<html>Bad Gateway</html>"
	answerWith(func(url.Values) any { return fakeslack.Reply{Status: http.StatusBadGateway, Body: []byte(page)} })
	got = probe("502")
	if got.Settled != "rejected" || got.Message != "Request completed but is not OK" || got.Own["status"] != 502.0 || got.Own["response"] != page {
		t.Errorf("502: %+v", got)
	}
	if want := []string{"method", "status", "response", "retryAfter", "attempts"}; !reflect.DeepEqual(got.Keys, want) {
		t.Errorf("502: err keys %q, want %q", got.Keys, want)
	}

	// this.api.userAPI is Slick's own (src/app/api/userAPI.ts): its
	// messages say [slick], with the answer's JSON whole
	answerWith(func(url.Values) any { return body })
	var u struct {
		Settled string `json:"settled"`
		Ctor    string `json:"ctor"`
		Message string `json:"message"`
	}
	h.jsInto(`return await globalThis.__userAPIProbe('chat.postMessage', {channel: 'C0FAKE00001', text: 'userAPI'})`, &u)
	if want := "[slick] userAPI chat.postMessage failed: " + string(body); u.Settled != "rejected" || u.Message != want {
		t.Errorf("userAPI: %s %q, want rejected with %q", u.Settled, u.Message, want)
	}
	answerWith(func(url.Values) any { return fakeslack.Reply{Status: http.StatusTooManyRequests} })
	h.jsInto(`return await globalThis.__userAPIProbe('chat.postMessage', {channel: 'C0FAKE00001', text: 'userAPI 429'})`, &u)
	if want := "[slick] userAPI chat.postMessage rate limited"; u.Message != want {
		t.Errorf("userAPI 429: %q, want %q", u.Message, want)
	}
}

// TestSlickReduxThunkNames: Slick's redux.thunkNames (src/app/slack/redux.ts,
// SlickMCP's find_actions) lists the thunk creators there are, each one
// getThunkCreator finds: in Clack the ones it defines.
func TestSlickReduxThunkNames(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, apiProbeSlick)
	if err := h.s.SetEnabled(h.ctx(), "APIProbe", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the probe plugin", func() bool { return h.jsBool(`return typeof globalThis.__thunkNames === 'function'`) })
	var got struct {
		Names   []string `json:"names"`
		Defined bool     `json:"defined"`
		Error   string   `json:"error"`
	}
	h.jsInto(`return globalThis.__thunkNames()`, &got)
	if got.Error != "" || !got.Defined {
		t.Fatalf("redux.thunkNames(): %+v", got)
	}
	for _, want := range []string{"apiCall", "apiCallExpedited", "markLastRead", "navigate", "handleDeepLink"} {
		if !slices.Contains(got.Names, want) {
			t.Errorf("redux.thunkNames() has no %s: %v", want, got.Names)
		}
	}
}

// TestSlickPresenceThunks: OnlineCount's and LastSeen's
// addMemberToPresenceSubscription and removeMemberFromPresenceSubscription
// are thunks Clack defines (Slack's presence manager, client-boot 4hlf),
// so Slick's dispatchThunk resolves them, with undefined as Slack's do.
func TestSlickPresenceThunks(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, apiProbeSlick)
	if err := h.s.SetEnabled(h.ctx(), "APIProbe", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the probe plugin", func() bool { return h.jsBool(`return typeof globalThis.__apiProbe === 'function'`) })
	for _, thunk := range []string{"addMemberToPresenceSubscription", "removeMemberFromPresenceSubscription"} {
		var got slickSettled
		h.jsInto(fmt.Sprintf(`return await globalThis.__apiProbe(%q, 'U0FAKE00077')`, thunk), &got)
		if got.Settled != "resolved" || got.Res != nil {
			t.Errorf("%s: %+v, want it resolved with undefined", thunk, got)
		}
	}
}
