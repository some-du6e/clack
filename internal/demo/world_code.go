package demo

import (
	"encoding/json"
	"net/url"
	"time"
)

// Inkwell's code channel (#inkwell-order-portal): the Code tab's diff,
// the PR view, the summary view and the context bar's actions, in the
// shapes fakeslack's codechannels.go answers. Content is the demo's own
// story: PR #217 fixes the order portal's bulk discount rounding.

const (
	DemoRepo    = "quillfeather/order-portal"
	DemoBranch  = "fix-bulk-discount-rounding"
	DemoPRURL   = "https://github.com/quillfeather/order-portal/pull/217"
	DemoPRTitle = "Fix the bulk discount rounding on 10+ reams"
)

func demoDiff() map[string]any {
	files := []map[string]any{
		{"filename": "src/pricing/discount.ts", "status": "modified", "additions": 9, "deletions": 4, "patch": "@@ -1,18 +1,23 @@\n import { Order } from '../orders/types'\n \n-// bulk orders (10+ reams) get the tier discount\n-export function applyBulkDiscount(order: Order): number {\n-  const subtotal = order.reams * order.pricePerReam\n-  const discount = subtotal * tierRate(order.reams)\n-  return Math.round((subtotal - discount) * 100) / 100\n+// bulk orders (10+ reams) get the tier discount. Money is integer\n+// cents all the way down: 107.40 * 0.075 is 8.054999… in floating\n+// point, which rounded the discount the wrong way.\n+export function applyBulkDiscount(order: Order): number {\n+  const subtotalCents = order.reams * toCents(order.pricePerReam)\n+  const discountCents = roundHalfUp(subtotalCents * tierRate(order.reams))\n+  return (subtotalCents - discountCents) / 100\n }\n \n+const toCents = (dollars: number) => Math.round(dollars * 100)\n+const roundHalfUp = (cents: number) => Math.floor(cents + 0.5)\n+\n export function tierRate(reams: number): number {\n   if (reams >= 50) return 0.12\n   if (reams >= 10) return 0.075\n   return 0\n }"},
		{"filename": "src/pricing/discount.test.ts", "status": "modified", "additions": 14, "deletions": 0, "patch": "@@ -22,3 +22,17 @@ describe('applyBulkDiscount', () => {\n   it('leaves small orders alone', () => {\n     expect(applyBulkDiscount(order(4, 8.95))).toBe(35.8)\n   })\n+\n+  it('rounds a half-cent discount up on 12 reams', () => {\n+    // Ultra Bright 24lb: $107.40 less 7.5% is $99.34, not $99.35\n+    expect(applyBulkDiscount(order(12, 8.95))).toBe(99.34)\n+  })\n+\n+  it.each([10, 11, 25, 49, 50, 120])('never charges an extra cent on %i reams', (n) => {\n+    const cents = Math.round(n * 895)\n+    const want = cents - Math.floor(cents * tierRate(n) + 0.5)\n+    expect(applyBulkDiscount(order(n, 8.95))).toBe(want / 100)\n+  })\n })"},
		{"filename": "CHANGELOG.md", "status": "modified", "additions": 4, "deletions": 0, "patch": "@@ -1,5 +1,9 @@\n # Changelog\n \n+## 3.8.1\n+\n+- Bulk orders of 10+ reams no longer round the discount down by a cent (#217).\n+\n ## 3.8.0\n \n - Reorder from your last invoice in one click."},
	}
	return map[string]any{"files": files, "truncated": false, "total_commits": 4, "base_branch": "main", "head_branch": DemoBranch}
}

func (w *World) demoPR() map[string]any {
	now := w.now().Unix()
	comment := func(id, kind string, at int64, login, body string, anchor map[string]any) map[string]any {
		c := map[string]any{"id": id, "comment_kind": kind, "review_state": "COMMENTED", "timestamp": at,
			"comment": map[string]any{"value": body}, "sender": map[string]any{"login": login}}
		if anchor != nil {
			c["review_anchor"] = anchor
		}
		return c
	}
	return map[string]any{
		"pull_request": map[string]any{"title": DemoPRTitle, "state": "open", "review_decision": "review_required", "has_conflicts": false,
			"body": "Orders of 10+ reams rounded a half-cent discount the wrong way (12 reams of Ultra Bright 24lb came to $99.35 instead of $99.34). " +
				"`applyBulkDiscount` now works in integer cents and rounds half up.\n\nCloses #212 (\"the portal charged me a penny extra\", reported by Priya)."},
		"diff":     demoDiff(),
		"revision": "c0ffee2",
		"comments": []map[string]any{
			comment("c1", "issue_comment", now-5400, "theo-lindqvist", "Thanks Inkwell. Can we get a test for exactly 10 reams? That's where the tier starts.", nil),
			comment("c2", "review_comment", now-4800, "theo-lindqvist", "Nit: `roundHalfUp` could live in src/money.ts, we'll want it for tax too.",
				map[string]any{"path": "src/pricing/discount.ts", "line": 14, "side": "RIGHT"}),
			comment("c3", "issue_comment", now-3000, "priya-raman", "if this gets me my penny back I'm framing it", nil),
			comment("c4", "review_summary", now-2400, "theo-lindqvist", "Looks good once the 10-ream case is in. Prinny approves too (it printed the diff without jamming).", nil),
		},
	}
}

// codeCall answers the code channel methods for the demo's code
// channel; anything else falls through to the fake.
func (w *World) codeCall(method string, f url.Values) (any, bool) {
	ch := orStr(f.Get("channel_id"), f.Get("channel"))
	switch method {
	case "client.codeChannels.getDiff":
		if ch != CodeID {
			return nil, false
		}
		return okMap("diff", demoDiff()), true
	case "client.codeChannels.getPullRequest":
		if f.Get("pr_url") != DemoPRURL {
			return nil, false
		}
		pr := w.demoPR()
		return okMap("pull_request", pr["pull_request"], "diff", pr["diff"], "comments", pr["comments"], "revision", pr["revision"]), true
	case "client.codeChannels.getBlockKitView":
		if ch != "" && ch != CodeID {
			return nil, false
		}
		ci := w.codeProp("ci_state")
		blocks := []any{
			map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": "PR #217 summary"}},
			map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "*3 files changed*, 6 new test cases. Orders of 10+ reams now round to the cent."}},
			map[string]any{"type": "divider"},
			map[string]any{"type": "section", "fields": []any{
				map[string]any{"type": "mrkdwn", "text": "*Branch*\n" + DemoBranch},
				map[string]any{"type": "mrkdwn", "text": "*CI*\n" + orStr(ci, "pending")},
				map[string]any{"type": "mrkdwn", "text": "*Reviewer*\ntheo-lindqvist"},
				map[string]any{"type": "mrkdwn", "text": "*Pennies saved*\nall of them"}}},
			map[string]any{"type": "actions", "elements": []any{map[string]any{"type": "button", "action_id": "inkwell_rerun",
				"text": map[string]any{"type": "plain_text", "text": "Re-run checks"}}}},
		}
		return okMap("blocks", blocks, "service_id", InkwellBotID, "app_id", InkwellAppID), true
	case "client.codeChannels.getView":
		if ch != "" && ch != CodeID {
			return nil, false
		}
		return okMap("html_content", "<!doctype html><h1>Discount check</h1><table><tr><th>Reams</th><th>Before</th><th>After</th></tr>"+
			"<tr><td>12</td><td>$99.35</td><td>$99.34</td></tr><tr><td>50</td><td>$393.81</td><td>$393.80</td></tr></table>",
			"content_version", 1, "csp", map[string]any{"resource_domains": []any{}}), true
	case "client.codeChannels.action":
		if ch != "" && ch != CodeID {
			return nil, false
		}
		w.goBusy(func() { w.bots.runCI("success") })
		return okMap(), true
	case "agents.sessions.stop":
		if ch != "" && ch != CodeID {
			return nil, false
		}
		w.setCodeProps(map[string]any{"agent_session": map[string]any{"status": "active"}})
		w.goBusy(func() {
			if w.sleep(1500 * time.Millisecond) {
				w.Post(InkwellID, CodeID, "", "Stopped. Everything so far is pushed to `"+DemoBranch+"`; ask me to pick it back up any time.")
			}
		})
		return okMap(), true
	}
	return nil, false
}

// codeProp reads one of the code channel's code_channel properties.
func (w *World) codeProp(key string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.convLocked(CodeID)
	if !ok || len(c.Properties) == 0 {
		return ""
	}
	var p struct {
		CC map[string]any `json:"code_channel"`
	}
	_ = json.Unmarshal(c.Properties, &p)
	s, _ := p.CC[key].(string)
	return s
}

// setCodeProps merges updates into the code channel's properties and
// pushes channel_updated, as Slack does when the agent's session or CI
// state changes.
func (w *World) setCodeProps(updates map[string]any) {
	w.mu.Lock()
	c, ok := w.convLocked(CodeID)
	if !ok {
		w.mu.Unlock()
		return
	}
	props := map[string]any{}
	if len(c.Properties) > 0 {
		_ = json.Unmarshal(c.Properties, &props)
	}
	mergeMap(props, updates)
	c.Properties, _ = json.Marshal(props)
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "channel_updated", "channel": CodeID, "updates": map[string]any{"properties": updates},
		"event_ts": microTS(w.now().UnixMicro())})
}

func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeMap(dm, sm)
				continue
			}
			cp := map[string]any{}
			mergeMap(cp, sm)
			dst[k] = cp
			continue
		}
		dst[k] = v
	}
}
