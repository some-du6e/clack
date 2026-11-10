# Desktop-style sign-in for Clack

Research written 2026-11-07. The plan at the end is now built (branch
`desktop-signin-impl`); "As built" below lists where Clack differs from it
and what is still unverified.

## Why this exists

Clack currently authenticates by reusing the browser's `xoxc` token and
`d`/`d-s` cookies (see `internal/slack/login.go`). On Hack Club's Enterprise
Grid this has repeatedly ended Karim's session on every device at once. We
want Clack to obtain a session that belongs to Clack, so that if Slack ever
invalidates it, only Clack is affected and the browser keeps working.

The official Slack desktop app does exactly this when a user clicks
"open the Slack app" from a browser sign-in page. This document records how
that handoff works, with evidence from the shipped app, and sketches how
Clack could follow the same pattern.

## Evidence base

- Artifact: `slack-desktop-4.53.276-amd64.deb`, an unauthenticated public
  download from `downloads.slack-edge.com/desktop-releases/linux/x64/4.53.276/`.
  - deb sha256 `8974c6a9b3ffd4ef09ced4403acaa444d743d113a185504d5a1dc405be46e746`
  - `resources/app.asar` sha256 `a1ee6a847cee0cb250ebb18c80aad94d2f9096092ce5fa729d0693eef1183953`
  - `dist/main.bundle.cjs` sha256 `78ab646c3a12cbba4169271a6c155ee2ba339d36cad5565be17206313f41e28a`
  - Electron 45.1.0, Chrome 156.0.8078.49.
- Extracted with `@electron/asar`, inventoried with REA 6.8.0
  (Evidence `ev_99f7de510468683fce4861a609e300306991734d43737dceea3462c077e0e4d7`),
  and re-printed un-minified with `esbuild` for reading. No source maps ship.
- Citations: `main:N` is a line in the re-printed `main.bundle.cjs`; `@N` is a
  byte offset of the quoted string in the original minified file, locatable
  with `rg -F -b -o '<string>' dist/main.bundle.cjs`.
- Nothing was run. No request was made to Slack with any credential. This is
  static reading of shipped code. It shows what the client sends and how it
  reads the reply; it cannot show what the server does, so server behaviour is
  called out as inference.

## The flow, end to end

1. The user starts sign-in on a Slack web page (`/ssb/signin`, `/ssb/add`, or
   `/ssb/signin_redirect`). The desktop app opens these in the system browser
   rather than in-app (`main:27772`, the "Sign in to Another Workspace" menu
   item opens `${base}/ssb/add?&s=1&v=4.53.276` via `shell.openExternal`;
   `@1033551`).
2. After the user authenticates in the browser, Slack's page navigates to a
   `slack://` deep link that carries a short-lived "magic" token. The OS hands
   that link to whichever app registered as the `slack://` handler — the
   desktop app.
3. The desktop app parses the deep link into one or more magic tokens, then
   makes an HTTPS call to `auth.loginMagicBulk` from its own Electron session.
   Slack's response sets the app's own `d` cookie (and friends) in that
   session and returns per-team results.
4. The app persists those cookies to its own cookie store and boots the client
   UI, which then has its own session independent of the browser.

The key property: the magic token in the deep link is the only secret that
crosses from browser to app. The actual session cookie is minted fresh by
Slack for the app's HTTP client. The browser's cookie is never copied.

## 1. Deep-link formats

Two shapes are parsed (`main:33300`–`main:33309`, module `29274`):

**Classic single-team magic login** (`@1390631`):

```
slack://<TEAMID>/magic-login/<token>[?host=<basedomain>]
```

Parsed by a regex `(" + KK + "/magic-login/[A-z0-9-]+/?)` where `KK` is the
team-id pattern `(E|T)[A-Z0-9]{8,50}` (module `66008`, `@1391... `). The app
splits each match into `{teamId, token}` and rebuilds each as a bulk token
string `z-app-<TEAMID>-<token>`. The optional `?host=` query names the base
domain to send the exchange to (validated against Slack's allowed domains by
`m()`, `main:33330`). This is the form seen in the wild, e.g.
`slack://T08PSQ7BQ/magic-login/<magic-id>` (public KDE writeups, below).

**Multi-token / Enterprise Grid bulk form** (`login-v2` host, `main:33289`):

```
slack://login-v2/?<i>.host=<basedomain>&<i>.tokens=<tok1>_<tok2>...&<i>.dpop=1
```

For `i = 0, 1, 2, …` the app reads `<i>.host`, `<i>.tokens` (underscore-joined
list), and `<i>.dpop`. Each index is one base domain with one or more tokens,
which is how multiple workspaces / an org are signed in at once. `p(e)` tests
`slack:` scheme + host `login-v2` (`main:33326`).

Both forms converge on the same internal shape: a list of
`{tokens[], unverifiedBaseDomain, requiresDpop}`.

There is also a toolbelt OAuth callback `slack://toolbelt-callback` (`@...`,
`Gl` in module `62131`) used for the separate "tsauth"/toolbelt token flow —
not the session login, ignore for Clack.

## 2. The exchange request and response

Built in module `main.bundle.cjs` at `main:25975`–`main:26090` (function `uI`
→ inner window loader). Confidence: high (verbatim from shipped code).

**Request** (`@952921`):

```
GET https://<basedomain>/api/auth.loginMagicBulk?magic_tokens=<tok1>,<tok2>&ssb=1[&vend_sameparty_cookies=1]
```

- `<basedomain>` is the validated host from the deep link (`slack.com` for
  commercial; gov/mil variants exist). The `/api/` base is built by
  `PL`/`E()` (`main:21... `, `${base}/api/`).
- `magic_tokens` is the comma-joined token list (the `z-app-…` strings for the
  classic form, or the raw tokens for `login-v2`).
- `ssb=1` marks it as a standalone-app (SSB = "Slack SSB"/desktop) request.
- `vend_sameparty_cookies=1` is added only when `requestSamePartyCookies` is
  set.
- **How it is sent:** not via `fetch`. The app creates a hidden
  `BrowserWindow` with a dedicated preload
  (`magic-login-preload-entry-point.bundle.js`) and calls `loadURL(m10, g10)`
  on the magic-login API URL (`main:26085`). The response body (JSON) is read
  back out of the page by the preload, which on an IPC ping posts
  `document.body.innerText` to the main process over the
  `SLACK_MAGIC_LOGIN_RESPONSE` channel (`@904190`; preload source in
  `dist/magic-login-preload-entry-point.bundle.js`). The main process checks
  the response came from the expected frame/window and parses it as
  `{ok, ...}` (`main:26010`). Because it is a real navigation in an Electron
  session, any `Set-Cookie` on the response is written straight into that
  session's cookie jar by Chromium — this is how the app ends up holding its
  own `d` cookie without ever copying one.

**User-Agent.** The hidden window's UA is set to the app UA
(`a10.userAgent = o3(i11)`, `main:25982`). `o3` (`main:23171`) returns the
Electron UA with a trailing ` Sonic Slack_SSB/4.53.276` marker (and
`Snap`/`Flatpak`/`GovEnvironment` suffixes where relevant). So the desktop
session is explicitly tagged as a desktop/SSB client in the UA — the opposite
of Clack's current approach of impersonating the browser's UA. (Clack sends
neither in the end: its own `Clack/<version>` UA, see "As built" below.)

**Optional DPoP binding** (`main:26030`–`main:26072`, and `main:23435`–`main:23468`).
When the token set has `requiresDpop`, the app:
- generates a P-256 EC keypair per base domain, stores the PEM encrypted with
  Electron `safeStorage` under `userData/dpop-keys/<domain>` (`main:23100`,
  `main:26046`);
- signs a `dpop+jwt` proof (`{typ:"dpop+jwt",alg:"ES256",jwk}` header;
  `{jti,htm,htu,iat}` claims) and sends it as an extra request header
  `Dpop: <proof>` (`aL = "Dpop"`, `@820392`). The login request itself is
  explicitly exempted from the per-request DPoP header
  (`if (t12.pathname.endsWith("/api/auth.loginMagicBulk")) return;`,
  `main:a7`), but the proof is passed to `loadURL` as `extraHeaders` for the
  binding (`main:26070`). After binding, every subsequent `*.slack.com` API/WS
  request carries a fresh `Dpop` proof (`main:sr`/`a7`, `@...`).
- If the server later returns a response header `dpop-unbind: true`, the app
  deletes the stored key for that domain (`@828343`, `main:23658`).

**Response handling** (`main:26076`–`main:26090`). The JSON is
`{ok, token_results: { <id>: {team?, auth_redir?, redir?, reason?, user?} }}`:
- If any team result has `auth_redir`, that workspace needs the browser to
  finish (e.g. SAML/SSO); the app opens that https URL externally
  (`MAGIC_LOGIN_REDIRECT`) rather than completing in-app (`main:26081`).
- Otherwise the first result with a `team.url` is used; depending on the
  team's environment the app may switch between NORMAL / RESTRICTED (gov) /
  MILITARY client modes (`main:26086`).
- Before closing the hidden window the app flushes cookies to disk and logs
  which cookies landed: it specifically reads back the `d` and `lc` cookies
  (`d10.cookies.get({name:"d"})`, `{name:"lc"}`, then `flushStore()`;
  `main:26015`). So the session cookies vended by this call are `d` (the auth
  cookie) and `lc` (a "last cookie"/session marker), both on the base domain.
- **No `auth.test`/`client.boot` is made inside this function.** After the
  epic resolves, the normal main window is loaded at the Sonic boot URL
  (`/client` or `/ssb/…redir=/ssb/client`; `EL()`, `main:29770`), and that
  webapp performs its own `client.boot` as usual. For the standalone
  "slackbot" window it re-loads the boot URL directly (`main:30005`).

### Where the magic token comes from

Besides the deep link, the app has three other intake paths for the same
magic token, all converging on the same `uI` exchange:
- **Clipboard on focus** (`magicLoginOnFocusEpic`, `main:26096`): when the
  window gains focus the app reads the clipboard; if it holds a `slack://…
  magic-login` URL it runs the exchange. (`c7()`/`c4()` read
  `clipboard.readText()`, `@935957`.)
- **`open-url` / `second-instance`**: macOS `open-url` and the Linux/Windows
  single-instance `second-instance` events deliver the `slack://` arg
  (`main:33048`, `main:25710`). The second-instance path can also carry a
  `tsauthCookies` data object, separate from magic login.
- **Local loopback POST** (dev only): `EC()` spins up an `http` server on
  `127.0.0.1:0` that accepts `POST {magic_login_uri}` (`@1130516`,
  `main:29746`); only used when `devEnv` is set.
- **SSO native path** (`START_NATIVE_AUTH`, `main:29990`): requests a login
  link programmatically and feeds the returned link into `uI`.

## 3. Is the desktop session really separate from the browser's?

Inference, medium-high confidence (static evidence is strong, server behaviour
unverified):

- The desktop app's cookies live in an Electron `session` partition
  (`PROD_DEFAULT_SESSION` → `session.defaultSession`, or
  `persist:<mode>`; module `10370`). The browser has its own cookie store.
  They are different clients to Slack.
- The `d` cookie the app holds is the one Slack *minted for this login call*,
  returned as `Set-Cookie` on the `auth.loginMagicBulk` response. It is not the
  browser's `d` value copied across. So they are two distinct sessions from the
  start.
- Slack can see this is a desktop client: `ssb=1` on the request and the
  `Slack_SSB/<ver> Sonic` UA marker both identify it. That is the basis for
  expecting Slack to treat it as its own "desktop" session row.
- The magic token is one-time and short-lived (it is a login-handoff token,
  not a session). Using it does not touch the browser's session.

What this does *not* prove: that signing out one leaves the other alive. That
is a server-side policy we cannot confirm from the client. But structurally the
sessions are independent, which is the property Clack needs — the failure that
keeps biting Karim is "Clack's cookie reuse makes Slack treat Clack and the
browser as the same session and kill both", and a freshly-vended cookie for a
`ssb=1` client is a different session row.

## 4. Registering as the `slack://` handler

The desktop app registers at startup (`main:33179`):
`app.setAsDefaultProtocolClient("slack")` on normal installs (dev/webdriver
builds pass extra args). The Linux `.desktop` file declares
`MimeType=x-scheme-handler/slack;` and `Exec=/usr/bin/slack %U`
(`/usr/share/applications/slack.desktop`). The deb depends on `xdg-utils`.

Per-OS, and what Clack already has (checked against `origin/ui`):

- **Linux.** Clack ships `packaging/linux/clack.desktop` with
  `MimeType=x-scheme-handler/slack;` and `Exec=clack %u`, and
  `internal/desktop/urlhandler_other.go` runs
  `xdg-mime default clack.desktop x-scheme-handler/slack`. This is the same
  mechanism Slack uses. **Conflict:** if real Slack desktop is installed it
  also claims `x-scheme-handler/slack`; whoever registered last (or the user's
  explicit default) wins. We should not silently steal it — see plan.
- **macOS.** Clack's `packaging/macos/Info.plist` already declares
  `CFBundleURLSchemes = [slack]`. LaunchServices registers it when the bundle
  is first seen; `urlhandler_other.go` says as much. Real Slack.app also claims
  it; macOS uses the most-recently-registered unless the user chose one.
- **Windows.** `packaging/windows/clack.iss` and
  `internal/desktop/urlhandler_windows.go` write
  `HKCU\Software\Classes\slack\shell\open\command = "clack.exe" "%1"`. This is
  per-user and overwrites any existing `slack` handler for that user, which is
  how a second app takes the scheme.

A Go app forwards the link the same way the current code already does for
in-app deep links: `cmd/clack/main.go` reads `desktop.URLArg(flag.Args())`,
forwards to a running instance via `desktop.Forward`, and the running instance
gets it in `desktop.Listen`'s callback → `App.OpenURL`. The magic-login case
would branch *before* `App.OpenURL`'s navigation logic, because a
`…/magic-login/…` link is an auth action, not a navigation.

Known Linux gotcha (public writeups): KDE's `kde-open` lowercases the host of
`slack://` links, turning `slack://T08PSQ7BQ/magic-login/…` into
`slack://t08psq7bq/…`, which breaks the team-id match. Slack users work around
it with an `xdg-open` wrapper. Clack's parser should upper-case the team id
(it already does: `t4[0].toUpperCase()` in Slack's parser; Clack must copy
that) to be robust to this.

## 5. Cookie rotation / persistence

Evidence, high confidence for the mechanism:

- Slack does set cookies on API responses, and the app persists them. The
  login window flushes `d`/`lc` to disk (`main:26015`). A `CookieMonitor`
  watches the session's cookie store for `changed` events on `d` and `lc` and
  logs them with cause and expiry (`main:32788`). There is also
  `SLACK_WAIT_AUTH_COOKIE_CHANGE` IPC that the webapp uses to block until the
  auth cookie actually changes and is flushed (`main:waitAuthCookieChange`,
  preload `@...`). So the app treats the `d` cookie as something Slack rotates
  and the app must re-persist.
- A SameSite transform runs on `Set-Cookie` for Slack domains
  (`onHeadersReceived`, `main:32... `): cookies that arrive `SameSite=None`
  are stored re-encrypted (prefix `__enc_ssb_cookie:` + `safeStorage` hex) and
  flipped to `SameSite=Lax` in the store, then transformed back to `None` with
  the real value when a request needs them (`a$`, `main:23505`; `se`/`st`,
  `main:32... `). This is desktop-specific cookie hygiene; Clack does not need
  to replicate it, but it confirms the app owns and rewrites its cookie jar.
- On MAS (Mac App Store) builds only, there is an extra encrypted
  `auth-cookie-backup` sidecar (`cD`/`cx`, `main:25507`/`main:25545`) that
  backs up and restores `d`/`lc` with `safeStorage` + an instance key. Not
  relevant to Clack's distribution, but shows how seriously the app guards the
  vended cookie.

For Clack's parallel cookie-rotation investigation: Slack rotates `d` via
`Set-Cookie` on ordinary API responses, and the desktop app persists the new
value immediately (flush on change). Clack's `internal/slack/client.go`
currently sends a fixed `Cookie: d=…` header from `Creds` and ignores response
`Set-Cookie` entirely (no cookie jar). If Clack ever vends its own `d`, it must
read `Set-Cookie` on every response and persist the rotated value, or it will
drift and get signed out.

## 6. Risks and policy

- **Detection.** (Overtaken on 2026-11-08: from Go, the `Slack_SSB` UA was
  itself what got the session signed out, and Clack now sends its own UA. See
  "As built" and "Rounds 2 and 3".) The honest version of this flow identifies itself (`ssb=1`,
  `Slack_SSB` UA). If Clack sends `auth.loginMagicBulk?...&ssb=1` but then uses
  a *browser* UA for subsequent calls, that mismatch is exactly the kind of
  anomaly that has been signing Karim out. If Clack adopts desktop sign-in it
  should be internally consistent: present as an SSB/desktop client throughout,
  with a stable UA, and send DPoP proofs if the token set requires them
  (`requiresDpop` / `dpop=1`). Sending the login request without the DPoP key
  the server expects, or dropping the `Dpop` header on later calls after
  binding, could also look anomalous.
- **DPoP is load-bearing on Grid.** Hack Club is Enterprise Grid, and the
  `login-v2`/bulk form carries `dpop=1` per token set. If Grid requires DPoP,
  Clack must implement the P-256 keypair + `dpop+jwt` proof (`ES256`,
  `{jti,htm,htu,iat}`), persist the key, and honour `dpop-unbind`. This is the
  most complex part and the biggest unknown (we have the client side but not
  whether the server rejects an unbound SSB login on this org).
- **Terms.** This is still an unofficial client using Slack's internal
  endpoints. `auth.loginMagicBulk` is undocumented
  (ReDocumented lists `auth.loginMagic` and `auth.magicLogins.create` as
  existence-only, `methods.txt`). Using it is no more sanctioned than the
  current token reuse, but it is *less* likely to collaterally damage the
  browser session, which is the actual harm we are trying to stop.
- **Triggering a real login email/SSO.** The magic token in a deep link is
  minted by the browser sign-in the *user* performs; Clack must never try to
  mint one itself or drive the browser sign-in. Clack's role is only to receive
  the `slack://…magic-login…` link the user's browser hands to the OS and
  exchange it.

## Implementation plan for Clack

Not to be built yet. This is the shape.

### internal/slack/login.go

Add a desktop sign-in path alongside the existing paste flow:

1. `ParseMagicLink(uri string) ([]MagicTokenSet, error)` — parse both
   `slack://<TEAM>/magic-login/<tok>?host=` and `slack://login-v2/?<i>.host=…&
   <i>.tokens=a_b&<i>.dpop=1`. Upper-case the team id (KDE lowercasing).
   Validate `host` is a slack.com domain with the existing `CookieURL`/host
   checks. Each set = `{Tokens []string, BaseDomain string, RequiresDPoP bool}`.
2. `LoginMagic(ctx, sets) (Creds, error)` — for each set, do
   `GET https://<base>/api/auth.loginMagicBulk?magic_tokens=<joined>&ssb=1`
   with a dedicated `http.Client` that has a `cookiejar.Jar`, sending the SSB
   UA (new constant, not the browser UA). Read `Set-Cookie` for `d` (and `lc`,
   `d-s`) out of the jar, parse `token_results`, handle `auth_redir` (tell the
   user that workspace needs the browser), pick `team.url`. Return `Creds` with
   the *vended* cookie, not any pasted one.
3. DPoP (guarded, only if `RequiresDPoP`): generate/persist a P-256 key per
   base domain (encrypted at rest — on Linux there is no Electron `safeStorage`;
   use the OS keyring or an app-scoped key, decide separately), build the
   `dpop+jwt` proof, send `Dpop:` on the bound calls, honour `dpop-unbind`.
   This is a separate milestone; start without DPoP and see whether Grid
   rejects the SSB login unbound (it may require a browser `host=` with
   `dpop=1`).
4. Extend `Creds` so a desktop session is marked as such (so the UA layer in
   `ua.go` sends the SSB UA consistently instead of a browser UA), and so the
   client reads and persists rotated `d`/`d-s`/`lc` from `Set-Cookie` on every
   response (needs a `cookiejar.Jar` in `client.go`, which today sends a fixed
   `Cookie` header and ignores `Set-Cookie`).

### cmd/clack/main.go

- Detect a magic-login link in `desktop.URLArg(flag.Args())` *before* the
  existing `App.OpenURL` navigation path: if the link matches
  `…/magic-login/…` or `login-v2`, route it to `slack.LoginMagic` and write
  creds, instead of treating it as an in-app navigation deep link.
- When already running, the forwarded link (`desktop.Forward` →
  `desktop.Listen` callback) must likewise branch: a magic link means
  "sign in", not "navigate".
- Add a user affordance in the login screen (`internal/ui/login.go`): a
  "Sign in with Slack (desktop)" button whose instruction is "click
  open-the-app on Slack's sign-in page", i.e. it waits for the OS to hand
  Clack the `slack://` link. Keep the paste flow as the fallback.

### Packaging / URL handler

- Linux/macOS/Windows registration already exists
  (`internal/desktop/urlhandler_*.go`, `packaging/*`). The gap is coexistence
  with real Slack desktop:
  - Do **not** auto-steal `x-scheme-handler/slack` on install. Register only on
    explicit user action (`--register-url-handler` already exists) or prompt.
  - Consider a Clack-specific scheme (e.g. `clack://`) so Clack can be the
    handler without fighting Slack, and only claim `slack://` when the user
    asks. The browser hands out `slack://`, though, so to actually receive the
    magic link Clack must be the `slack://` default at the moment the user
    clicks — document this tradeoff for the user to decide.
  - Keep the team-id upper-casing workaround for KDE's `kde-open`.

## Open questions / unverified

- Whether Slack actually keeps the desktop session alive when the browser
  signs out (and vice versa). Structurally independent, but server policy
  unverified — needs a careful live test with a throwaway account, not Karim's.
- Whether Enterprise Grid rejects an `auth.loginMagicBulk` from an SSB client
  that does not present a DPoP binding. The client sets `dpop=1` in the
  `login-v2` deep link for Grid; we have not confirmed the server requirement.
- Exact response schema of `auth.loginMagicBulk` (we have the fields the client
  reads: `ok`, `token_results[].team.url`, `.auth_redir`, `.redir`, `.reason`,
  `.user`; there may be more).
- Whether `lc`/`d-s` are required for subsequent calls or only `d`.
- The precise `host=` value Slack puts in production magic links for Grid
  (commercial `slack.com` is clear; the org-specific base domain for Hack Club
  is not verified from static analysis alone).

## As built (2026-11-07)

Code: `internal/slack/magic.go` (link parsing, the exchange), `desktop.go`
(the cookie jar), `dpop.go`, `ua.go` (`UserAgent`, `HonestHeaders`),
`internal/ui/login.go`, `cmd/clack/main.go`. Tests use httptest only. No
request in this work went to Slack.

Differences from the plan:

- **Token.** `auth.loginMagicBulk` vends cookies, not a token. Clack reads
  the xoxc token from the page Slack desktop opens next,
  `https://app.slack.com/ssb/first?redir=/ssb/client`, by matching
  `"api_token":"xoxc-…"`. If that page has none, sign-in fails and says so.
  Clack never tries another page. In particular it never loads
  `<team>/ssb/redirect`, and refuses a redirect there.
- **No auto-registration.** Clack never makes itself the `slack://`
  handler. `clack --register-url-handler` does that, on request. The Windows
  installer's "Open slack:// links with Clack" task is now unchecked by
  default. Copying the link into the sign-in screen works without the
  handler.
- **External links while signed in.** A magic link the OS delivers while
  Clack is signed in doesn't replace the session (any web page can make the
  browser open a `slack://` link). It shows a toast. Links sign in from the
  sign-in screen only.
- **DPoP key storage.** `<config>/dpop-keys/<domain>.pem`, PKCS#8, owner-only
  like creds.json. It isn't encrypted: Clack has no `safeStorage` equivalent
  yet (see SECURITY.md C-14).
- **Cookie jar.** Flat, one value per name, sent to every https slack.com
  host. Slack desktop's Chromium jar honours host and path scoping.
- **User-Agent (changed 2026-11-08).** Clack does not present as Slack
  desktop. Every request of a desktop session (the exchange, the token page,
  `auth.test`, the Web API, edgeapi, files, uploads, the realtime handshake
  and credential-free CDN fetches) sends
  `Clack/<version> (+https://github.com/some-du6e/clack)`, the
  same User-Agent a pasted session sends, with no `sec-ch-ua*`,
  `Sec-Fetch-*`, `Origin` or `Referer` and no browser `Accept`. The exchange
  still says `ssb=1`. Sending Slack desktop's Electron User-Agent from Go's
  TLS stack got the test account signed out everywhere (rounds 2 and 3,
  below). The plan in §6 assumed a consistent desktop identity was safe, and
  that was wrong.

Still unverified (needs a live test, with consent, on a throwaway account):

- whether `/ssb/first` really carries `api_token` for a fresh SSB session;
- whether the desktop session survives the browser signing out, and the
  reverse;
- everything about DPoP on the server side: whether Grid requires it,
  whether `htu` keeps the query (Clack drops it, per RFC 9449), which
  requests need a proof (Clack: the exchange, `/api/…`, edgeapi, the
  websocket and `/ssb/first`; not file downloads), whether Slack sends
  `DPoP-Nonce`, and what a rejection looks like (Clack stops on a 401 with
  a DPoP `WWW-Authenticate` or an error code containing `dpop`);
- the sign-in page: Clack's "Open sign-in page" opens
  `https://slack.com/ssb/add?s=1&v=4.53.276`, the URL Slack desktop's
  "Sign in to Another Workspace" opens. Whether a first sign-in from there
  ends in a copyable `slack://…/magic-login/…` link is unverified.

## Live test, 2026-11-08 (meclod, Hack Club Grid)

With the user's consent, on the project's own test account:

- **Getting a link.** Slack's `https://slack.com/ssb/add?s=1&v=4.53.276` lists workspaces; "Open in Slack" goes to `hackclub.enterprise.slack.com/?is_ssb_browser_signin=1`, whose "Launch Hack Club in Slack" opens `https://hackclub.slack.com/ssb/signin_redirect`. That page navigates to `slack://T0266FRGM/magic-login/<token>?id=1`: the classic form, no `host=` and **no `dpop=1`** on this org.
- **The exchange works.** Both Clack and a clean browser got `ok: true` from `auth.loginMagicBulk?magic_tokens=z-app-T0266FRGM-<token>&ssb=1`. Slack set `d`, `d-s`, `lc`, `b`, `x` and `utm`. `token_results` holds `ok`, `user`, `redir` (`https://hackclub.slack.com/messages`) and `team` (id, name, url, domain, enterprise ids, icons). **No token.**
- **`/ssb/first` has no token.** It's a marketing landing page (`ssb-landing-core`). In a browser it stays put; in Slack desktop it hands over to the web client.
- **Where the token comes from.** In a clean browser holding only the vended cookies, loading `redir` goes to `app.slack.com/client/T0266FRGM`. Its web client then loads the hidden page `https://app.slack.com/auth?app=client&return_to=%2Fclient%2FT0266FRGM&teams=&iframe=1` (beacons `auth_stale`, `auth_fetch`, `lc_ingest`). That page sets `sessionStorage.incomingConfig = JSON.stringify({"teams": {…}})`, with one entry per team (`id`, `name`, `url`, `domain`, `token`) for both the org (E…) and the workspace (T…), plus `enterprise_api_token`. Clack now reads its tokens there (`bootTokens`, `parseIncomingConfig`).
- The browser session the link came from stayed signed in throughout.

### Rounds 2 and 3, and the control

- **Round 2** (Go, Slack desktop's `Mozilla/5.0 (…) Slack/4.53.276 Chrome/… Electron/45.1.0 Safari/537.36 Sonic Slack_SSB/4.53.276` User-Agent): exchange, `app.slack.com/auth`, `auth.test` with the workspace token and with the org token on the workspace host. **Signed out everywhere** within seconds, the browser included.
- **Round 3** (Go, same User-Agent, no org-token `auth.test`): exchange, `app.slack.com/auth`, `auth.test` with the workspace token. **Signed out everywhere** within 15 s.
- **Control** (Python, default `Python-urllib/3.x`, no client hints, no `Origin`, a plain cookie jar): the same three requests, `auth.loginMagicBulk?magic_tokens=…&ssb=1`, then `GET https://app.slack.com/auth?app=client&return_to=%2Fclient%2F<T>&teams=&iframe=1`, then `auth.test` with the workspace token. **Both sessions alive** 100+ s later.

So neither the token page nor the vended cookies were the trigger. A Go client that claims to be Slack desktop (or Chrome) was. Clack now sends its own User-Agent everywhere (As built, "User-Agent"; docs/SIGNOUT-AUDIT.md "Result").
