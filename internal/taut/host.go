package taut

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// installHost exposes the __clack.taut_* functions the TautAPI calls.
// Sync ones run on the JS thread and only touch memory; anything that may
// wait on the network is async (a Promise in JS, a goroutine here).
func (t *Taut) installHost(r *runtime) {
	rt := r.rt
	sync := func(name string, fn jsrt.JSONFunc) { rt.ExposeJSON("taut_"+name, fn) }
	async := func(name string, fn jsrt.AsyncFunc) { rt.ExposeAsync("taut_"+name, fn) }

	// ---- the mirror ----
	sync("slices", func(a []json.RawMessage) (any, error) { return t.mirror.slices(), nil })
	sync("get", func(a []json.RawMessage) (any, error) { return t.mirror.get(strArg(a, 0), strArg(a, 1)), nil })
	sync("keys", func(a []json.RawMessage) (any, error) { return t.mirror.keys(strArg(a, 0)), nil })
	sync("action", func(a []json.RawMessage) (any, error) { return nil, nil })

	// ---- what plugins patched (Go keeps cheap flags for the UI) ----
	sync("statePatched", func(a []json.RawMessage) (any, error) {
		var s statePatchInfo
		jsonArg(a, 0, &s)
		t.ui.setStatePatched(s)
		return nil, nil
	})
	sync("patchedThunks", func(a []json.RawMessage) (any, error) {
		var names []string
		jsonArg(a, 0, &names)
		t.thunks.setPatched(names)
		return nil, nil
	})
	sync("patchedComponents", func(a []json.RawMessage) (any, error) {
		var names []string
		jsonArg(a, 0, &names)
		t.ui.setPatchedComponents(names)
		return nil, nil
	})
	sync("composerHooks", func(a []json.RawMessage) (any, error) {
		var h composerHooks
		jsonArg(a, 0, &h)
		t.ui.setComposerHooks(h)
		return nil, nil
	})
	sync("actionablePatched", func(a []json.RawMessage) (any, error) {
		var on bool
		jsonArg(a, 0, &on)
		t.ui.actionable.Store(on)
		return nil, nil
	})
	sync("injecting", func(a []json.RawMessage) (any, error) { return nil, nil })
	sync("experiments", func(a []json.RawMessage) (any, error) { return nil, nil })
	sync("rtmTypes", func(a []json.RawMessage) (any, error) {
		var types []string
		jsonArg(a, 0, &types)
		t.ui.setRTMTypes(types)
		return nil, nil
	})

	// ---- UI ----
	sync("overlays", func(a []json.RawMessage) (any, error) {
		var o []Overlay
		jsonArg(a, 0, &o)
		t.ui.setOverlays(o)
		return nil, nil
	})
	sync("preferencesTabs", func(a []json.RawMessage) (any, error) {
		var tabs []PreferencesTab
		jsonArg(a, 0, &tabs)
		t.ui.setTabs(tabs)
		return nil, nil
	})
	sync("style", func(a []json.RawMessage) (any, error) {
		key := strArg(a, 0)
		if len(a) < 2 || string(a[1]) == "null" {
			if f := t.opts.UI.RemoveStyle; f != nil {
				f(key)
			}
			return nil, nil
		}
		if f := t.opts.UI.SetStyle; f != nil {
			f(key, strArg(a, 1))
		}
		return nil, nil
	})
	sync("callGo", func(a []json.RawMessage) (any, error) {
		var id int64
		jsonArg(a, 0, &id)
		var args []json.RawMessage
		jsonArg(a, 1, &args)
		if IsTautFn(id) {
			return t.fns.call(id, args)
		}
		if f := t.opts.UI.CallGo; f != nil {
			b, _ := json.Marshal(args)
			res, err := f(id, b)
			if err != nil || len(res) == 0 {
				return nil, err
			}
			return json.RawMessage(res), nil
		}
		return nil, fmt.Errorf("taut: no Go function %d", id)
	})
	sync("threadPane", func(a []json.RawMessage) (any, error) {
		f := t.opts.UI.ThreadPane
		if f == nil {
			return nil, nil
		}
		return f(strArg(a, 0), strArg(a, 1), strArg(a, 2), strArg(a, 3)), nil
	})
	sync("input", func(a []json.RawMessage) (any, error) {
		f := t.opts.UI.Input
		if f == nil {
			return nil, nil
		}
		var where InputWhere
		jsonArg(a, 1, &where)
		var arg json.RawMessage
		if len(a) > 2 {
			arg = a[2]
		}
		return f(strArg(a, 0), where, arg), nil
	})
	sync("localDraft", func(a []json.RawMessage) (any, error) {
		if f := t.opts.UI.SetLocalDraft; f != nil && len(a) > 0 {
			f(a[0])
		}
		return nil, nil
	})
	sync("huddleEventLog", func(a []json.RawMessage) (any, error) { return nil, nil })
	sync("huddleReaction", func(a []json.RawMessage) (any, error) { return false, nil })

	// ---- data ----
	sync("kv", func(a []json.RawMessage) (any, error) {
		op, ns, key := strArg(a, 0), strArg(a, 1), strArg(a, 2)
		switch op {
		case "list":
			return t.data.list(ns), nil
		case "read":
			if v, ok := t.data.read(ns, key); ok {
				return v, nil
			}
			return nil, nil
		case "write":
			t.data.write(ns, key, strArg(a, 3))
			return true, nil
		case "delete":
			return t.data.delete(ns, key), nil
		case "clear":
			t.data.clear(ns)
			return true, nil
		}
		return nil, fmt.Errorf("taut: kv %s", op)
	})
	sync("cookie", func(a []json.RawMessage) (any, error) { return t.cookie(strArg(a, 0), a[1:]), nil })
	sync("accountsSupported", func(a []json.RawMessage) (any, error) { return t.opts.UI.SwitchAccount != nil, nil })
	async("accounts", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return t.accounts(ctx, strArg(a, 0), strArg(a, 1))
	})

	// ---- thunks ----
	sync("thunkNames", func(a []json.RawMessage) (any, error) { return t.thunks.names(), nil })
	async("thunk", func(ctx context.Context, a []json.RawMessage) (any, error) {
		var args []json.RawMessage
		jsonArg(a, 1, &args)
		return t.thunks.runThunk(ctx, strArg(a, 0), args)
	})
	async("callNative", func(ctx context.Context, a []json.RawMessage) (any, error) {
		var id int64
		jsonArg(a, 0, &id)
		var args []json.RawMessage
		jsonArg(a, 1, &args)
		return t.thunks.callNative(ctx, id, args)
	})

	// ---- network ----
	async("api", func(ctx context.Context, a []json.RawMessage) (any, error) {
		var fields map[string]string
		jsonArg(a, 1, &fields)
		var files map[string]apiFile
		jsonArg(a, 2, &files)
		return t.userAPI(ctx, strArg(a, 0), fields, files)
	})
	async("messageMetadata", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return t.messageMetadata(ctx, strArg(a, 0), strArg(a, 1))
	})

	// ---- time ----
	sync("scheduleEnd", func(a []json.RawMessage) (any, error) {
		var windows []ScheduleWindow
		jsonArg(a, 0, &windows)
		var now float64
		jsonArg(a, 2, &now)
		at := time.UnixMilli(int64(now))
		if now == 0 {
			at = time.Now()
		}
		end, ok := ScheduleEnd(windows, strArg(a, 1), at)
		if !ok {
			return nil, nil
		}
		return end.UnixMilli(), nil
	})
}

type apiFile struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data []byte `json:"data"`
}

// userAPI calls a Slack Web API method as the signed-in user (Taut's
// userAPI: the active team's token, the session cookie). It reports the
// HTTP status so JS can retry 429s like Taut does.
func (t *Taut) userAPI(ctx context.Context, method string, fields map[string]string, files map[string]apiFile) (any, error) {
	if len(files) == 0 {
		vals := url.Values{}
		for k, v := range fields {
			vals.Set(k, v)
		}
		raw, err := t.app.C.Raw(ctx, method, vals)
		if err != nil {
			var se *slack.Error
			if errors.As(err, &se) {
				return map[string]any{"status": 200, "json": map[string]any{"ok": false, "error": se.Code}}, nil
			}
			if errors.Is(err, slack.RateLimited) {
				return map[string]any{"status": 429, "retryAfter": 2}, nil
			}
			return nil, err
		}
		return map[string]any{"status": 200, "json": json.RawMessage(raw)}, nil
	}
	// multipart: one file field (Slack's upload-style methods take one)
	var name string
	var f apiFile
	for k, v := range files {
		name, f = k, v
		break
	}
	var out json.RawMessage
	vals := url.Values{}
	for k, v := range fields {
		vals.Set(k, v)
	}
	if err := t.app.C.CallMultipart(ctx, method, vals, name, f.Name, f.Data, &out); err != nil {
		return nil, err
	}
	return map[string]any{"status": 200, "json": out}, nil
}

// messageMetadata is a message's app metadata (event_type + event_payload),
// which history only returns with include_all_metadata.
func (t *Taut) messageMetadata(ctx context.Context, ch, ts string) (any, error) {
	raw, err := t.app.C.Raw(ctx, "conversations.replies", url.Values{
		"channel": {ch}, "ts": {ts}, "limit": {"1"}, "inclusive": {"true"}, "include_all_metadata": {"true"},
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Messages []struct {
			TS       string          `json:"ts"`
			Metadata json.RawMessage `json:"metadata"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	for _, m := range res.Messages {
		if m.TS == ts && len(m.Metadata) > 0 {
			return m.Metadata, nil
		}
	}
	return nil, nil
}

// cookie is api.cookies over Clack's session: only Slack's `d` cookie
// exists, and setting it switches the session's cookie for this run.
func (t *Taut) cookie(op string, a []json.RawMessage) any {
	var q struct {
		URL   string `json:"url"`
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	jsonArg(a, 0, &q)
	d := t.app.C.Creds.Cookie
	if dec, err := url.QueryUnescape(d); err == nil {
		d = dec
	}
	cookie := map[string]any{"name": "d", "value": d, "domain": ".slack.com", "path": "/", "secure": true, "httpOnly": true, "sameSite": "lax"}
	switch op {
	case "get":
		if q.Name == "d" && d != "" {
			return cookie
		}
		return nil
	case "getAll":
		if d == "" {
			return []any{}
		}
		return []any{cookie}
	case "set":
		if q.Name != "d" || q.Value == "" {
			return false
		}
		t.app.C.Creds.Cookie = url.QueryEscape(q.Value)
		return true
	case "remove":
		return q.Name != "d"
	}
	return nil
}
