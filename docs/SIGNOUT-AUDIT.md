# Sign-out audit (v0.1.0-alpha.2)

## Result (2026-11-08): the browser disguise was the cause

**Hack Club's Enterprise Grid ends a session everywhere (browser, desktop, every client) when a request that claims to be a browser plainly isn't one.** Clack's TLS and HTTP/2 come from Go's `net/http`, and no User-Agent string changes that.

Live tests on the project's own test account, with the user's consent:

| run | identity sent | steps | session afterwards |
|---|---|---|---|
| alpha.1 | hardcoded Linux Chrome User-Agent | pasted session, Sign in | **signed out everywhere** when Sign in was clicked |
| alpha.2 | the user's own browser User-Agent plus Chrome `sec-ch-ua*` client hints, `Origin: https://app.slack.com` | pasted session, Sign in | **signed out everywhere** when Sign in was clicked |
| desktop sign-in, rounds 2 and 3 (Go) | Slack desktop's `Mozilla/5.0 (…) … Electron/… Slack_SSB/…` User-Agent | `auth.loginMagicBulk?…&ssb=1`, `app.slack.com/auth`, `auth.test` | **signed out everywhere** within 15 s. Round 3 ran 3092cb9, which never sends the org token to `auth.test` |
| **control: Python script** | its default `Python-urllib/3.x`; no client hints, no `Origin`, a plain cookie jar | the same three requests: `auth.loginMagicBulk?…&ssb=1`, then `GET https://app.slack.com/auth?app=client&return_to=%2Fclient%2F<T>&teams=&iframe=1`, then `auth.test` with the workspace token | **both the browser session and the new session alive** 100+ s later |
| the same script style, all day | honest default User-Agent, a borrowed browser session | ordinary Web API calls | never signed out |

The control did the exact steps that failed, from a non-browser stack, and said so. So the trigger is the disguise, not the `/auth` token page and not the `d-s` cookie.

**The fix (honest User-Agent).** Every request Clack makes to Slack carries one User-Agent, `Clack/<version> (+https://github.com/some-du6e/yoloslackgoclient)`, for pasted and desktop sessions alike: Web API, edgeapi, file downloads and uploads, the sign-in exchange and token page, the realtime handshake, and credential-free CDN and image fetches. No request sends `sec-ch-ua*` client hints, `Sec-Fetch-*`, `Origin` or `Referer`. The socket handshake also drops the browser's `Pragma`/`Cache-Control`. That is `slack.HonestHeaders`, enforced again in the transport (`honestTransport`) so redirect hops and anything a caller adds go out the same way. Plugin fetches to Slack hosts and Taut's saved-account check get the same headers. `TestNoBrowserDisguise` walks every request kind through both session kinds and fails on any browser token or browser-only header, and `TestNoBrowserUserAgentInSource` keeps `Slack_SSB` and `Chrome/` out of the code outside ua.go's deny list.

**The earlier hypotheses were not the cause.** Suspect #2 (`d-s`) and suspect #3 (the org token's `auth.test` on the workspace host) are ruled out as the trigger. Round 3 made no org-token `auth.test` and was still signed out. Its jar sent the vended `d-s` the way the control's jar did, and the control survived. Both changes stay because they are cheaper and closer to what Slack's own client does: pasted sessions send `d` only, and nothing sends the org token to `auth.test`. Suspect #4 below, ranked last at ~10%, was right.

The rest of this file is the original code-only audit and its follow-ups, kept as written.

Why Clack ends Hack Club Enterprise Grid sessions everywhere (browser, desktop app, Clack). This is a code-only audit of tag `v0.1.0-alpha.2`. No requests were made to Slack. Every `file:line` below refers to that tag (`git show v0.1.0-alpha.2:<path>`).

**What we know.** With alpha.2, the session ended "right when I clicked" Sign in. So the cause is in the first few seconds: `slack.SignIn` and the boot burst that `connect()` starts right after it. It is not the 20 s poll or the realtime socket, which is off by default and opens only after Boot.

**Contrast case.** A Python script has used another account's browser session for a day from a different machine and UA. It makes ordinary team-host calls (`conversations.history`, `chat.postMessage`, `reactions.add`, `auth.test`), and the session survived. By our own notes (memory `slack-web-api-restricted`), it sends `Cookie: d=<raw>` and nothing else. So a second client, a second IP, or a different UA is not enough on its own. Each candidate below is something Clack does at sign-in that this script does not.

## What happens when Sign in is clicked

All requests are `POST` with `Content-Type: application/x-www-form-urlencoded`, the token in the form body, and these headers:

- `Cookie: d=<d>; d-s=<d-s>` (client.go:190-195, 386)
- `Origin: https://app.slack.com` (client.go:387)
- the pasted UA plus derived `sec-ch-ua`, `sec-ch-ua-mobile` and `sec-ch-ua-platform` (ua.go:68-113)

There is no `Referer`, no `sec-fetch-*`, no `Accept-Language`, and none of the web client's `_x_*` query params. TLS and HTTP/2 come from Go's `net/http` (client.go:84-102). Requests run up to 12 at a time (client.go:60).

| t | Request | Host | Token | Where |
|---|---|---|---|---|
| 0 | `auth.test` | `hackclub.slack.com` | workspace T token | login.go:72 |
| +~150 ms | `auth.test` | **`hackclub.slack.com`** (`oc := c` keeps TeamURL; line 90 sets it again) | **org E token** | login.go:87-92 |
| +~300 ms, in parallel (Boot, app.go:410-469) | `users.conversations` (paged, limit 1000) | team | T | methods.go:48 |
| | `client.counts` | **enterprise** | **E** | methods.go:74, client.go:47 |
| | `users.channelSections.list` | **enterprise** | **E** | more.go:28 |
| | `users.prefs.get` (first run: no cache) | **enterprise** | **E** | more.go:326 |
| | `team.info` | team | T | app.go:443 |
| | edge `users/info` (self) | `edgeapi.slack.com/cache/E…/users/info?_x_app_name=client` | **E as both `token` and `enterprise_token`** | client.go:307-324 |
| | `emoji.list` (first run: about 7 MB) | team | T | app.go:465 |
| after the wait | edge `users/info`, one batch per DM partner | edgeapi | E | app.go:491 |
| then | prefetch: `conversations.history` for up to 24 unread conversations, 3 at a time; `loadExtras` (starred/saved); resolve loop; the default channel's history, tabs and members; Taut/Block Kit start | mixed | mixed | app.go:505-509, ui/backend.go:104-118 |
| +5 s | `usergroups.list` | team | T | app.go:469 |

There is no write at startup: no `users.prefs.set`, `users.setPresence`, `conversations.mark` or `client.shouldReload`. All of those are only reached through user actions (ui/backend.go:642, 683; popups; Taut thunks). Nothing loads a Slack web page: `Download` and `checkRedirect` refuse non-file slack.com GETs (client.go:145-163, 441-469).

## Ranked suspects

### 1. A rotated `d`/`d-s` is ignored and the old one is replayed within milliseconds. Confidence: medium (~40%)

**Evidence.**
- `slack.New` builds an `http.Client` with no `Jar` (client.go:102). No code anywhere reads `Set-Cookie`: `rg 'Jar|Set-Cookie|SetCookie'` over internal/ and cmd/ finds nothing.
- Every request sends the static pasted value (client.go:190-195, 386, 455, 529).
- Sign-in sends two `auth.test` calls back to back (login.go:72, 92). About 8 more requests follow in parallel within a second (app.go:410-469).
- If Slack answers the first request from a new client fingerprint with a fresh `d` or `d-s` (rotation or re-binding), then the second request, and every one after it, presents the superseded cookie. So does the browser. A superseded session cookie arriving again is the textbook refresh-token-reuse signal, and the usual response to it is to revoke the whole session family. That fits "at the moment I clicked", and it fits every client dying together.
- The 2026-11-07 meclod death came from the `/ssb/redirect` page load, and page loads are where Slack definitely issues cookies. That points the same way.
- The Python script doesn't contradict this. Either Slack doesn't rotate for it, or it uses a `requests.Session` cookie jar and adopts the new cookie. Check which.

**What's unverified.** I couldn't confirm from public docs that API responses carry `Set-Cookie: d=…` or `d-s=…`. A HAR from Karim's own browser would settle it. Ask them to export one around a page reload and a few API calls, then look at the response `Set-Cookie` headers. That is the browser talking to Slack, not us.

**Minimal change.**
1. Diagnose first. In `doPost`, `Download` and `UploadBytes`, log the *names* of any `Set-Cookie` cookies on Slack responses (never the values; redact.go already exists).
2. Then follow rotation. Wrap the transport with a `cookiejar.Jar`, seeded with `d`/`d-s` for `.slack.com`, and stop setting the `Cookie` header by hand. Persist changed `d`/`d-s` back to creds.json through `WriteCreds`.
3. Rotation that Clack follows still leaves the browser holding the old cookie. If rotation turns out to be real, the safe fix is to send nothing beyond what the Python script sends (see #2 and #3), not to race the browser.

### 2. Clack sends `d-s`; the surviving client doesn't. Confidence: medium-low (~25%). Not the cause (see Result); the change stays

**Evidence.**
- `cookieHeader` appends `; d-s=<value>` whenever one was pasted (client.go:190-195). The README *tells* users to include it (README "Sign in", line 98), and alpha.2's warning made that stronger.
- `d-s` is a short numeric value set alongside `d`, which is consistent with a timestamp for when the session cookie was set or refreshed. Slack can compare it with what it last issued for that `d`. A second client presenting an old `d-s` next to a valid `d` looks exactly like a copied cookie that the real browser has since refreshed. Without `d-s`, the server has nothing to compare.
- The only client known to be safe sends `d` alone.
- `cleanCookie` leaves digits untouched, so the value isn't mangled (login.go:107-120). The problem would be that it is sent at all.

**Minimal change.** Drop `d-s` from the header: `cookieHeader` returns `"d=" + c.Creds.Cookie`. Make the sign-in field optional and ignored, or remove it. Remove the README instruction to paste it. One line in client.go:191-193, plus README.

### 3. The org (E) token goes to hosts and methods the safe client never touches. Confidence: low-medium (~15%). Not the cause (see Result); the change stays

**Evidence.**
- `auth.test` with the org token on the **workspace** host (login.go:87-92; line 90 is a no-op because `oc := c` already copied TeamURL). The web client sends the org token to `hackclub.enterprise.slack.com`, not to the workspace host. Alpha.1 did the same thing, so this is common to all four sign-outs.
- At boot, three enterprise-host methods run with the E token at the same moment (`client.counts`, `users.channelSections.list`, `users.prefs.get`; client.go:46-57). So does edgeapi with the E token in both `token` and `enterprise_token` (client.go:316-317). Slack's own client presumably pairs the workspace token with the org token there, so check that in a HAR.
- In under a second, one `d` cookie is used with two different tokens across three hosts, and the org-level session is exercised from a new fingerprint. Grid's session controls live at the org level. The Python script uses one token on one host.
- If a policy is going to treat org-level use from an unrecognised client as a hijack, this is where it would trigger.

**Minimal change.**
- Test without the org token: sign in with only the T token. `useEnt` then returns false (client.go:199-201) and every call goes to the team host with the T token, which is the Python script's footprint. The cost: client.counts and sections answer `team_is_restricted` and the sidebar degrades.
- If the org token stays, don't validate it with a second `auth.test` on the team host. Take `EnterpriseID` from the first `auth.test` (`a.EnterpriseID`, login.go:79), and if a probe is needed, send it to `https://<org>.enterprise.slack.com/api/`.
- On edgeapi, send `token=<T>` and `enterprise_token=<E>`, matching a HAR of the real client.

### 4. Half-impersonating a browser. Confidence: low (~10%). **Confirmed 2026-11-08 (see Result)**

**Evidence.**
- Clack claims Chrome: the pasted UA, `sec-ch-ua*` built from it (ua.go:68-113), and `Origin: https://app.slack.com` (client.go:387, 530). But its TLS ClientHello and HTTP/2 SETTINGS are Go's (client.go:84-92). It also sends no `Referer`, no `sec-fetch-*`, none of the web client's query params (`_x_id`, `_x_version_ts`, `_x_gantry`, `slack_route`, `_x_reason`, `_x_mode`, `_x_sonic`, `fp`), and it opens a burst of about 8 parallel calls within a second of a cold start, where a real tab starts with `client.userBoot`/`client.boot`.
- A Chrome UA on a non-Chrome TLS fingerprint is a stronger anomaly signal than an honest non-browser UA. This would explain why alpha.2's *closer* browser imitation didn't help.
- What argues against it: alpha.1's mismatched UA and alpha.2's matching UA both died, and the Python script (whose UA we should check) survived.

**Minimal change.** Try the honest option: drop the client hints and the `Origin` header, and send a plain `User-Agent: Clack/<version> (+github…)`, the way the Python script presumably does. Which direction is right can only be decided by experiment. Recommend testing on a throwaway account, never on Karim's.

### 5. `expandPastedTeams` can pick the wrong workspace or org. Confidence: low (~5%) as a cause; real bug anyway

**Evidence.**
- `localConfig_v2.teams` lists every workspace and org signed in to in that browser. The code takes the **first E entry by ID** as the org (login.go:185-188), whatever the workspace is. It takes the first T entry (map sorted by ID, login.go:166-169) unless one's URL contains what was typed in Workspace (login.go:189-193).
- `strings.Contains(host, want)` matches loosely: "hack" matches several hosts.
- For a user on two Grids, or with the workspace field left empty, Clack can validate and boot with another org's token, using the same `d`. That is one more unusual cross-org pattern.

**Minimal change.**
- Pick the org whose ID equals the chosen team's enterprise ID: the first `auth.test` returns `enterprise_id`, so match against it after that call.
- Require an exact host match on Workspace, and error when several T entries are present and none was chosen.

### 6. The forbidden-method guard can be bypassed, but not at sign-in. Confidence: ~0% for these incidents; fix anyway

**Evidence.**
- `forbiddenMethods` matches exactly on the method name (client.go:230-239, 271-273). The URL is then built as `base+method` (client.go:250, 296), with no validation of the name.
- A plugin-supplied name such as `auth.signout?x=1`, `auth.signout/` or `auth.signout#` passes the map check, and the URL that results still requests `/api/auth.signout`. Plugin calls pass names straight through: Taut `userAPI` (taut/host.go:247, 272) and `apiCall` (taut/thunks.go:521).
- Nothing at sign-in uses it. Taut and Block Kit start only after Boot (ui/backend.go:109). I searched the bundled non-Go assets for `signout`/`auth.revoke` and found nothing, though the assets pack may be compressed.
- jsrt `fetch` and Slick downloads use `http.DefaultClient` with no session cookie (jsrt/boot.go:532-556, slick/manager.go:1082-1084). They can't reach the session unless a plugin is handed the token and cookie.

**Minimal change.** In `callOn` and `multipartOn`:
- reject any method name that doesn't match `^[a-zA-Z][a-zA-Z0-9]*(\.[a-zA-Z][a-zA-Z0-9]*)+$`;
- compare against `forbiddenMethods` case-insensitively;
- add anything whose name contains `signout` or `revoke`.

## Already ruled out at sign-in

- **Realtime socket.** Off by default and set before Boot (ui/backend.go:101); it starts only after Boot finishes (app.go:501-504).
- **20 s poll.** The first tick is 20 s after Boot (app.go:183-190).
- **Writes, mark/read, presence, prefs.** None happen without a user action (see above).
- **Web page loads.** Refused (client.go:159-161, 452-454).
- **Explicit sign-out methods.** Refused by name (client.go:238, 271). The only bypass (#6) needs a plugin.

## Suggested next steps (no Slack traffic from us)

1. Ask Karim for a HAR of their own browser: one reload of app.slack.com and a minute of use. It answers four questions: does `Set-Cookie` rotate `d`/`d-s` on API responses (#1/#2), which token goes to edgeapi (#3), which `_x_*`/`slack_route` params are sent (#4), and which host the org token is sent to.
2. Find out what the surviving Python script sends: UA, whether it uses a cookie jar, `d` only.
3. Ship the cheap, low-risk changes together:
   - drop `d-s` (#2);
   - remove the org-token `auth.test` on the team host (#3);
   - log `Set-Cookie` names (#1);
   - harden method-name checks (#6).

   Then test only on a disposable Grid account.

## Follow-up: d-s is a last-seen timestamp (2026-11-08)

Checked from the orchestrator with one `auth.test` on a session that has survived a day of plain Web API calls (only cookie names and hashes were printed, never values):

- Slack answers every API call with `Set-Cookie: d-s=…` (session cookie, `.slack.com`, HttpOnly) and `uc=…` (10-year expiry). `d` was not re-set.
- `d-s` is a 10-digit Unix timestamp. Two calls 3 s apart returned values exactly 3 apart.

So a pasted `d-s` is stale the moment it's copied, while the browser keeps sending the newest one. Clack (alpha.1 and alpha.2) sent the pasted one on every request, starting with the Sign in `auth.test`. Slack then saw the same session presenting a last-seen stamp older than the one it had just issued, which is what a cloned cookie looks like. The session that survived never sends `d-s`. This promotes suspect #2 (with #1's rotation mechanism) to the top. (Superseded by the Result at the top: `d-s` was not the trigger.)

Change: Clack never sends or stores `d-s` (`Client.cookieHeader`, Taut's `checkAuth`), and the sign-in card, CLI and README no longer ask for it. Still unconfirmed: we can't test it on the affected account without risking another sign-out.

## Live result, 2026-11-08: the org-token auth.test

With the user's consent we ran desktop sign-in on the project's own test account (Hack Club Grid). Details are in docs/DESKTOP-SIGNIN.md.

| run | what Clack (or the browser) did | session afterwards |
|---|---|---|
| Clack round 1 | exchange; `/ssb/first` (no token, so it stopped) | browser still signed in |
| clean Chrome, same vended session | exchange; full web client incl. `app.slack.com/auth` | browser still signed in |
| Python helper, all day | `auth.test` and Web API calls with the **workspace** token | never signed out |
| Clack round 2 | exchange; `app.slack.com/auth`; `auth.test` with the workspace token **and with the org (E) token on the workspace host** | **signed out everywhere**: the browser and Clack's own new session, within seconds |

Alpha.1 and alpha.2 also called `auth.test` with the org token on the workspace host the moment Sign in was clicked (suspect #3 above). Every sign-out followed that call, and nothing that skipped it was signed out. Slack's own client never makes it. Suspect #3 is now the leading cause, ahead of `d-s`. (Superseded: round 3 skipped that call and was signed out anyway, and the honest-User-Agent control survived. See Result at the top.)

Change: neither sign-in path sends the org token to `auth.test` any more. The workspace token's `auth.test` names the org (`enterprise_id`), and the org's token and address come from the token page (desktop) or the pasted JSON (Advanced). A typed org token without its address is refused, so it can never be used on the workspace host.
