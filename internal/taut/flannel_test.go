package taut

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// flannelSettled is how a query settled, as the test's catch saw it.
type flannelSettled struct {
	Found   bool           `json:"found"`
	Settled string         `json:"settled"`
	Res     map[string]any `json:"res"`
	Ctor    string         `json:"ctor"`
	Name    string         `json:"name"`
	Message string         `json:"message"`
	Own     map[string]any `json:"own"`
}

// query calls the flannel client's query export, found as OnlineCount
// finds it (named query, its source naming "Unsupported flannel method"),
// for team ("" for selfTeamIds.teamId) with {method, args} and extra
// request fields (JS source).
func (e *apiCallEnv) query(team, method string, args any, extra string) flannelSettled {
	e.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		e.t.Fatal(err)
	}
	var out flannelSettled
	e.jsInto(fmt.Sprintf(`
		const query = T.getExport((x) => typeof x === 'function' && x.name === 'query' && Function.prototype.toString.call(x).includes('Unsupported flannel method'))
		if (!query) return { found: false }
		const team = %q || T.getRawState().selfTeamIds.teamId
		try {
			return { found: true, settled: 'resolved', res: await query(team, { method: %q, args: %s, %s }) }
		} catch (err) {
			const own = {}
			for (const k of Object.keys(err)) own[k] = err[k] === undefined ? '<undefined>' : err[k]
			return { found: true, settled: 'rejected', ctor: err?.constructor?.name, name: err?.name, message: err?.message, own }
		}`, team, method, b, extra), &out)
	if !out.Found {
		e.t.Fatal("no query export named query whose source names \"Unsupported flannel method\"")
	}
	return out
}

// TestFlannelQueryLikeSlack: Slack's flannel client (client-boot EVxR,
// build a09b625d90aa) sends a method of its table to the team's edge cache
// and settles as its FlannelRequest (Ln2M) over BaseRequest (Umoo) does:
// the answer with flannelUrl, args (Clack's token "[redacted]"), method
// and clientRequestId; a not-ok answer as a FlannelRequestError (its name
// Error's, "Flannel <method> not ok: <error>", data, method, args, id,
// attempts), at once even for internal_error; permission_denied through
// BaseRequest.throwError (status PERMISSION_DENIED); an HTTP failure as
// BaseRequest's Error (data {ok: false, error: message}), a 503 tried
// three times. A method outside the table, another team or an aborted
// signal reject, as Slack's do, and nothing goes out.
func TestFlannelQueryLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 5*time.Second)
	const ch = fakeslack.ViewsChannel
	people := []string{fakeslack.SynthMemberID(92001), fakeslack.SynthMemberID(92002)}
	e.fs.SetRoster(ch, people...)
	list := func(marker string) map[string]any {
		return map[string]any{"channels": []string{ch}, "filter": "everyone AND NOT bots AND NOT apps", "count": 100, "marker": marker}
	}
	calls := func() int64 { return e.fs.CallCount("edge/users/list") }

	got := e.query("", "users/list", list(""), "")
	if got.Settled != "resolved" {
		t.Fatalf("users/list: %+v", got)
	}
	r := got.Res
	var ids []string
	if rs, ok := r["results"].([]any); ok {
		for _, x := range rs {
			if m, ok := x.(map[string]any); ok {
				ids = append(ids, fmt.Sprint(m["id"]))
			}
		}
	}
	if r["ok"] != true || !slices.Equal(ids, people) || r["next_marker"] != "" {
		t.Errorf("users/list's answer: ok %v, results %v, next_marker %v; want the channel's %v", r["ok"], ids, r["next_marker"], people)
	}
	if r["method"] != "users/list" || r["flannelUrl"] != e.app.C.EdgeBase() {
		t.Errorf("method %v, flannelUrl %v; want users/list at %s", r["method"], r["flannelUrl"], e.app.C.EdgeBase())
	}
	if id, _ := r["clientRequestId"].(string); !regexp.MustCompile(`^clack-\d+\.\d{3}$`).MatchString(id) {
		t.Errorf("clientRequestId %q", id)
	}
	if a, _ := r["args"].(map[string]any); a["token"] != "[redacted]" || a["filter"] != "everyone AND NOT bots AND NOT apps" {
		t.Errorf("the answer's args %v: want the query's, the token [redacted]", r["args"])
	}

	notOK := func(code, marker string) flannelSettled {
		t.Helper()
		e.fs.SetFault("edge/users/list", fakeslack.Fault{Error: code, Times: 3})
		defer e.fs.SetFault("edge/users/list", fakeslack.Fault{})
		return e.query("", "users/list", list(marker), "")
	}
	for _, code := range []string{"invalid_cursor", "internal_error"} {
		n := calls()
		got := notOK(code, "m-"+code)
		if got.Settled != "rejected" || got.Ctor != "FlannelRequestError" || got.Name != "Error" || got.Message != "Flannel users/list not ok: "+code {
			t.Errorf("%s: %+v, want a FlannelRequestError %q", code, got, "Flannel users/list not ok: "+code)
			continue
		}
		data, _ := got.Own["data"].(map[string]any)
		if data["error"] != code || data["method"] != "users/list" || got.Own["method"] != "users/list" || got.Own["id"] != data["clientRequestId"] {
			t.Errorf("%s: data %v, method %v, id %v", code, data, got.Own["method"], got.Own["id"])
		}
		if got.Own["attempts"] != float64(1) || calls()-n != 1 {
			t.Errorf("%s: %v attempts, %d requests; a not-ok answer is final", code, got.Own["attempts"], calls()-n)
		}
	}
	if got := notOK("permission_denied", "m-perm"); got.Settled != "rejected" || got.Ctor != "Error" || got.Message != "Flannel users/list not ok: permission_denied" ||
		got.Own["status"] != "PERMISSION_DENIED" || got.Own["method"] != "users/list" {
		t.Errorf("permission_denied: %+v, want BaseRequest's throwError with status PERMISSION_DENIED", got)
	}

	httpFail := func(status, times int, marker string) flannelSettled {
		t.Helper()
		e.fs.SetFault("edge/users/list", fakeslack.Fault{Status: status, Times: times})
		defer e.fs.SetFault("edge/users/list", fakeslack.Fault{})
		return e.query("", "users/list", list(marker), "")
	}
	if got := httpFail(404, 1, "m-404"); got.Settled != "rejected" || got.Ctor != "Error" || got.Message != "Request completed but is not OK" ||
		got.Own["status"] != float64(404) || got.Own["attempts"] != float64(1) {
		t.Errorf("HTTP 404: %+v", got)
	} else if data, _ := got.Own["data"].(map[string]any); data["ok"] != false || data["error"] != "Request completed but is not OK" {
		t.Errorf("HTTP 404's data %v: want {ok: false, error: the message}", got.Own["data"])
	}
	if got := httpFail(503, 3, "m-503"); got.Settled != "rejected" || got.Own["status"] != float64(503) || got.Own["attempts"] != float64(3) {
		t.Errorf("HTTP 503 three times: %+v, want 3 attempts", got)
	}

	n := calls()
	if got := e.query("", "users/nope", map[string]any{}, ""); got.Settled != "rejected" || got.Message != "[Taut] Clack sends no flannel socket message (users/nope)" {
		t.Errorf("a method outside the table: %+v", got)
	}
	if got := e.query("", "users/nope", map[string]any{"type": "x"}, ""); got.Settled != "rejected" || got.Message != "Arguments must not include `type` or `subtype` fields" {
		t.Errorf("a gateway query with a type: %+v", got)
	}
	if got := e.query("T0NOTMINE1", "users/list", list("m-team"), ""); got.Settled != "rejected" {
		t.Errorf("another team's query: %+v", got)
	}
	if got := e.query("", "users/list", list("m-abort"), "signal: AbortSignal.abort()"); got.Settled != "rejected" || got.Name != "AbortError" || got.Message != "Request was cancelled" {
		t.Errorf("an aborted signal: %+v", got)
	}
	if calls() != n {
		t.Errorf("%d edge users/list went out for queries Slack rejects", calls()-n)
	}
}

// TestPresenceSubscriptionGetterLikeSlack: the presence manager's
// getPresenceSubscriptionByTeamId (client-boot 4hlf) is the count of
// subscribers for each member presence_sub carries: Clack's own set, the
// viewer in it, one each, and the members plugins subscribed what they
// counted; undefined for a team the client isn't signed in to.
func TestPresenceSubscriptionGetterLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 5*time.Second)
	counts := func(team string) map[string]int {
		t.Helper()
		var out map[string]int
		e.jsInto(fmt.Sprintf(`const g = T.getExport((x) => typeof x === 'function' && x.name === 'getPresenceSubscriptionByTeamId')
			if (!g) throw new Error('no getPresenceSubscriptionByTeamId export')
			const m = g(%q || T.getRawState().selfTeamIds.teamId)
			return m === undefined ? null : m`, team), &out)
		return out
	}
	self := e.app.C.Creds.UserID
	if m := counts(""); m[self] != 1 {
		t.Errorf("the viewer's count %d in %v, want 1", m[self], m)
	}
	const member = "U0FAKE00077"
	for i := 0; i < 2; i++ {
		if got := e.probe("addMemberToPresenceSubscription", member); got.Settled != "resolved" {
			t.Fatalf("addMemberToPresenceSubscription: %+v", got)
		}
	}
	if m := counts(""); m[member] != 2 {
		t.Errorf("%s subscribed twice counts %d: %v", member, m[member], m)
	}
	if got := e.probe("removeMemberFromPresenceSubscription", member); got.Settled != "resolved" {
		t.Fatalf("removeMemberFromPresenceSubscription: %+v", got)
	}
	if m := counts(""); m[member] != 1 {
		t.Errorf("%s after one leaving counts %d", member, m[member])
	}
	if m := counts("T0NOTMINE1"); m != nil {
		t.Errorf("another team's map %v, want undefined", m)
	}
}

// TestFlannelQueueLikeSlack: flannel queries go through Slack's API queue
// (client-boot 7quX, ApiQ.q): an identical query already running is
// answered by that one (one request, the same answer); at most 5 of one
// method run at once (7 users/list: 5 go out, 2 wait); and an abort
// rejects with AbortError and stops the request, its retries included (a
// 503 is tried 3 times otherwise).
func TestFlannelQueueLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 5*time.Second)
	const ch = fakeslack.ViewsChannel
	e.fs.SetRoster(ch, fakeslack.SynthMemberID(92101))
	calls := func() int64 { return e.fs.CallCount("edge/users/list") }
	const q = `const query = T.getExport((x) => typeof x === 'function' && x.name === 'query' && Function.prototype.toString.call(x).includes('Unsupported flannel method'))
		const team = T.getRawState().selfTeamIds.teamId
		const list = (marker, signal) => query(team, { method: 'users/list', args: { channels: ['` + ch + `'], count: 100, marker }, signal })
		`
	var ok bool

	n := calls()
	var same bool
	e.jsInto(q+`const [a, b] = await Promise.all([list('dup'), list('dup')]); return a === b`, &same)
	if !same || calls()-n != 1 {
		t.Errorf("two identical queries: the same answer %v, %d requests; want one request answering both", same, calls()-n)
	}

	e.fs.SetFault("edge/users/list", fakeslack.Fault{Delay: 800 * time.Millisecond})
	n = calls()
	e.jsInto(q+`globalThis.many = Promise.all([1, 2, 3, 4, 5, 6, 7].map((i) => list('m' + i))); return true`, &ok)
	time.Sleep(300 * time.Millisecond)
	if got := calls() - n; got != 5 {
		t.Errorf("%d users/list running at once, want 5 (ApiQ's per-method limit)", got)
	}
	e.jsInto(`await many; return true`, &ok)
	if got := calls() - n; got != 7 {
		t.Errorf("%d users/list in all, want the 7", got)
	}
	e.fs.SetFault("edge/users/list", fakeslack.Fault{})

	e.fs.SetFault("edge/users/list", fakeslack.Fault{Status: 503, Times: 3})
	defer e.fs.SetFault("edge/users/list", fakeslack.Fault{})
	n = calls()
	e.jsInto(q+`globalThis.ac = new AbortController(); globalThis.aborted = list('abort', ac.signal).then(() => 'resolved', (err) => err.name); return true`, &ok)
	eventually(t, 5*time.Second, "the first try", func() bool { return calls()-n >= 1 })
	var name string
	e.jsInto(`ac.abort(); return await aborted`, &name)
	time.Sleep(3 * time.Second) // past the retry's wait (60 s in Slack's units, 0.6-2.4 s here)
	if name != "AbortError" || calls()-n != 1 {
		t.Errorf("an aborted query: %q after %d requests, want AbortError and no retry", name, calls()-n)
	}
}
