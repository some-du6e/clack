# Security and policy review (2026-11-07)

This review covers the whole codebase on branch `security` (based on `ui` at 4222be3, then merged with `ui` at e54f91a). It looks at credentials, deep links, the network, the plugin sandbox (Taut and Slick), the Hack Club scraping policy, and the supply chain. Every High and Medium finding is fixed on this branch. Each finding below lists its fix and the test that covers it.

Severity scale:
- **High:** a remote party (a message, a link or a plugin update) gets credentials, local files or code execution outside the sandbox.
- **Medium:** the same, but it needs an unusual setup or a malicious plugin the user installed. Also covers a policy breach.
- **Low:** defence in depth.
- **Info:** a design decision, written down.

## Trust model

- **Slack content is hostile.** That covers messages, unfurls, files, links and other people's profiles. It must never reach credentials, local files, internal network addresses or disk.
- **Plugins are trusted code, granted with informed consent.** This matches Taut and Slick. A plugin gets `fetch` (no CORS), `cookies`, `localStorage`, and `localStorage['localConfig_v2']`, which holds the session token as it does on Slack's own page. That was a deliberate decision. So a plugin can act as the user on Slack. The sandbox makes sure it can't do more than that: no reading or writing files outside its own storage, no host process, no breakout from wasm, no freezing the UI. Install-time consent tells the user what a plugin can do.
- **Same-user local processes are trusted.** That includes the single-instance socket and the `slack://` handoff from the OS.

## 1. Credentials

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| C-1 | High | The RTM gateway URL carries `token=xoxc-…`. A failed websocket dial returned an error that quoted the URL, and `rtm: disconnected (%v)` wrote it to the log. | `slack.scrubErr` redacts every HTTP and websocket error. `cmd/clack` also wraps the log output in `slack.RedactWriter`, as a last line of defence for `xox?-` tokens and `d`/`d-s` cookie values. | `TestDialErrorRedacted`, `TestRedact` (internal/slack/redact_test.go) |
| C-4 | Medium | `Client.Download` attached the `d`/`d-s` cookies to any URL, including `http://` and non-Slack hosts. An https→http redirect kept the cookie. | `credsOK`: cookies and the token go only to `https` slack.com / `*.slack.com` hosts or the configured API origin; anything else is refused (`ErrUntrustedHost`). Redirects: see U-2 in §5. | `TestCredentialsStayOnSlack`, `TestCheckRedirectStripsCredentials` |
| C-5 | Medium | Gateway URLs from the server (primary, fallback, `reconnect_url`) had the token appended without a host check. Sign-in accepted any workspace with a dot in it (`evil.com`) and trusted the URL `auth.test` returned. | Only `wss://` Slack hosts get the token. Sign-in needs a slack.com host. `ReadCreds` and account switching refuse non-https or non-Slack team/enterprise URLs. | `TestWebSocketURLRejectsForeignHosts`, `TestRTMRefusesForeignSocket`, `TestSignInOnlySlackHosts`, `TestReadCredsRefusesForeignHosts` |
| C-6 | Medium | creds.json: rewriting an existing 0644 file left it at 0644 (`os.WriteFile` doesn't chmod). On Windows, `0600` means nothing. | `slack.WriteCreds`/`WriteSecretFile`: write a temp file, then rename; the file is 0600 in a 0700 directory; on Windows an owner-only protected DACL (`x/sys/windows`, already a dependency). Loading tightens a file that's too open. The yoloslack→clack migration copies through the same path. | `TestWriteCredsPermissions` |
| C-8 | Low | `%v`/`%+v` of `slack.Creds` or `SignInInput` printed the tokens and cookies. | Redacting `String`, `GoString` and `Format`. JSON output is unchanged. | `TestCredsFormattingRedacts` |
| C-11 | Low | `clack login` echoed the pasted token and cookies. | `stty -echo` while reading secrets on Unix terminals. Windows still echoes. | code review |
| P-* | — | Plugin-facing credential paths: see §3. | | |
| C-14 | Info | No macOS Keychain or Windows Credential Manager. | Not done: it needs cgo (Keychain) or new dependencies. creds.json is owner-only on every OS instead. Future work. | — |
| C-16 | Medium | Desktop sign-in (README "Sign in with a Slack link") takes a `slack://` magic-login link, which is untrusted input: anyone can send one, and a web page can make the browser open one. | `ParseMagicLink` accepts only Slack's two shapes. Every `host=` must be slack.com or a subdomain (no ports, userinfo, IP literals or IDN), and tokens must be `[A-Za-z0-9_-]`, so nothing can be smuggled into the exchange URL. The exchange's own client refuses redirects off slack.com and to `/ssb/redirect`. An `auth_redir` is only shown if it is https on a Slack host, and a `team.url` that isn't https slack.com is refused. A link the OS delivers while Clack is signed in never replaces the session (`App.SignInWithLink`). Links are masked on screen and redacted in logs and errors (`magic-login/…`, `magic_tokens=`, `<i>.tokens=`). | `TestParseMagicLinkHostile`, `TestLoginMagicErrors`, `TestSignInWithLink`, `TestRedactMagicLinks` |
| C-17 | Info | New secrets in a desktop session: the cookies Slack vends to Clack (`d`, plus `lc`, `d-s` and whatever else Slack sets, in `creds.json`'s `cookie` and `cookies`) and, for links with `dpop=1`, a P-256 DPoP private key per base domain. | `creds.json` as C-6. Rotated cookies are written through `WriteCreds` (temp file, rename, owner-only), and only while that session is the live one, so a late write can't bring back credentials after "Sign out" or an account switch. Cookies are taken only from https slack.com responses (or the configured API origin), capped at 48 cookies of 4 KB, and sent only to the same hosts. The DPoP key is PKCS#8 PEM in `<config>/dpop-keys/<domain>.pem`, 0600 in a 0700 directory, with a file name checked against slack.com. It isn't encrypted at rest (Slack desktop uses Electron `safeStorage`; Clack has no keychain yet, C-14). Anyone who can read `creds.json` can already act as you, and the key adds nothing beyond that on this machine. `dpop-unbind` from Slack deletes it. `Creds` formatting masks `cookies`. | `TestDesktopSessionPersists`, `TestDesktopSessionIgnoresOtherHosts`, `TestDPoPProofShape`, `TestDPoPUnbind`, `TestCredsFormattingRedacts` |
| C-18 | High | Clack disguised itself as a browser on a non-browser TLS/HTTP stack: the pasted browser User-Agent with Chrome `sec-ch-ua*` client hints and `Origin: https://app.slack.com` (pasted sessions), or Slack desktop's Electron `Slack_SSB` User-Agent (desktop sessions). On Hack Club's Grid that ended the session everywhere, browser included; an honest non-browser client doing the same requests survived (docs/SIGNOUT-AUDIT.md "Result"). | One User-Agent for every request to Slack, both session kinds: `Clack/<version> (+https://github.com/some-du6e/yoloslackgoclient)`. No `sec-ch-*`, `Sec-Fetch-*`, `Origin` or `Referer` anywhere, the realtime handshake included (`slack.HonestHeaders`, enforced again by `honestTransport` under every Slack client, so redirect hops and caller-added headers are covered). Plugin fetches to Slack hosts and Taut's saved-account check go out the same way. The pasted user agent is no longer asked for; an old `creds.json` `user_agent` or a pasted `"ua"` is read and ignored. | `TestNoBrowserDisguise`, `TestNoBrowserDisguiseSignIn`, `TestNoBrowserUserAgentInSource`, `TestUserAgentIsHonest`, `TestPluginFetchToSlackIsHonest` |
| C-19 | High | **The sign-out root cause.** Grid ends a session everywhere a few seconds after it sees the session's cookie and token on a connection with Go `crypto/tls`'s ClientHello, whatever the User-Agent, headers, cookies or HTTP version. The same requests over Python urllib's OpenSSL 3.0.13 hello survived, and so did the same Clack binary behind mitmproxy (docs/SIGNOUT-AUDIT.md "Root cause"). | `internal/slack/tlsfp`: every TLS connection to a Slack host (slack.com, slack-edge.com, slack-imgs.com, slack-files.com and their subdomains) replays the recorded OpenSSL hello through uTLS, with ALPN `http/1.1` only and HTTP/1.1. That covers the API transport (Web API, edgeapi, downloads, uploads, the realtime socket's handshake, sign-in), the public CDN transport (on top of its public-address dial guard, which still runs first), and `tlsfp.Mixed`, which sends plugin fetches, plugin and theme downloads and the update check to Slack's hosts through the same dialer and leaves every other host on Go's default transport. `HTTPS_PROXY`: Clack sends its own CONNECT and runs the handshake inside the tunnel, so Slack still sees the recorded hello. Certificate checks are `crypto/tls`'s own code in uTLS: system roots (`SSL_CERT_FILE`/`SSL_CERT_DIR`), the host name, expiry; nothing is skipped. Only tlsfp may import `crypto/tls` or uTLS, build an `http.Transport`, or use Go's default client. Huddle signalling to Amazon Chime keeps `crypto/tls` (no Slack credentials). | `TestHelloIsTheRecording`, `TestHelloForSlackHosts`, `TestEveryTransportSendsTheRecordedHello`, `TestVerification`, `TestSSLCertFile`, `TestProxyConnect`, `TestMixed`, `TestPublicTransportGuardUnderTLS`, `TestOnlyTLSFPDials` |

On a pasted session the `d-s` cookie is never sent or stored (old creds.json files may still hold one; it's ignored). Slack resets it to the current Unix time on every response, so a pasted copy is stale at once, and replaying an older stamp than the browser's is the leading suspect for Enterprise Grid ending the session everywhere (docs/SIGNOUT-AUDIT.md). It is still redacted like `d`. A desktop session is different: Slack vended `d-s` to Clack and Clack follows every new value Slack sets, so it sends the current one, as Slack desktop does (`internal/slack/desktop.go`).

## 2. Deep links and opening URLs

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| C-7 | Medium | Three copies of `openURL` checked only the prefix. `mailto:x?attach=~/.ssh/id_rsa` makes some mail clients attach the file. Control characters and quotes went through to `xdg-open`/`open`/`rundll32`. | One opener, `internal/ui/openurl`. It allows http, https and mailto only. mailto keeps only `subject`, `body`, `cc`, `bcc` and `to`. Control characters, spaces, quotes and backslashes are refused. The URL is re-serialized, so it always starts with its scheme and is never read as a `-flag`. Slick, Taut, slackc and the lightbox all go through it. | `TestCheck` (internal/ui/openurl) |
| C-9 | Low | `slack://` deep links: `id=C1/t/…` or `id=C1?q=…` could smuggle extra route segments in. Links only ever navigated or opened a profile; nothing sent, joined, left, downloaded or signed out. | Ids and timestamps must have Slack's shape (C/D/G channels, U/W users, `\d+\.\d+` ts). | `TestDeepLinkRouteOnlyNavigates` (internal/ui/route_test.go) |
| D-1 | Low | Single-instance socket on Linux without `XDG_RUNTIME_DIR`: `/tmp/clack-<uid>-<hash>.sock` sits in a world-writable directory. Another local user could squat on it, receive forwarded `slack://` links, or block startup. | The fallback socket now lives in `/tmp/clack-<uid>/`, which is created 0700 and must be owned by the user (not a symlink) before it's used. | `TestRuntimeDirRefusesForeignOrOpen` (internal/desktop) |
| D-2 | Info | OS URL handlers (`"Clack.exe" "%1"`, `Exec=clack %u`). | The URL is the first positional argument. Go's `flag` stops at the first non-flag, so text a browser lets into the URL can't inject `-creds` or other flags. A forwarded link goes through Slack's `handleDeepLink` thunk (which plugins can see, as in Slack) and then C-9's route check. | code review |

## 3. Plugin sandbox (Taut, Slick, jsrt)

Plugins run in QuickJS compiled to wasm on wazero (`internal/jsrt`). Wasm has no host access except the imported host functions. There's no WASI preopen, no environment variables and no arguments.

### jsrt and Taut

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| P-1 | High | Path traversal through a plugin id. The id check ran only in JS, in the plugin's own realm. A bundle whose `static get id()` returns `'Ok'` once and then `'../../x'` made Go write `plugins/<id>.js` and `.meta.json` anywhere the user can write. | `validPluginID` in Go: Taut's regex, not `.` or `..`, no Windows device names, no case-only duplicates. | `TestImportRejectsBadIDs` (internal/taut/security_test.go) |
| P-2 | High | Uninterruptible loops. QuickJS-NG's generic array methods (`Array.prototype.reverse.call({length: 2**53-1})`, sort, join, includes…) loop in C without checking the interrupt, so the JS thread stayed at 100% forever and every `Do`/`Call` caller waited forever. | `csrc/quickjs-interrupt.patch` makes the slow paths of `JS_(Try)GetPropertyInt64` check the interrupt, and jsrt.wasm is rebuilt reproducibly in CI. Backstop: a watchdog gives up on a task still running `max(Timeout, 1 s)` past its deadline. `Done` fires and callers get `ErrClosed`. wazero's `WithCloseOnContextDone` was tried and dropped because it made renders 2–3× slower. | `TestArrayLikeLoopsInterrupted`, `TestWatchdogGivesUpOnStuckTask` (internal/jsrt/security_test.go) |
| P-3 | Medium | A hung plugin was restarted after each crash, forever. | After 3 crashes in 5 minutes, plugins stay stopped with an error ("turn plugins back on one at a time"). | `TestHungPluginStopsAfterRepeatedCrashes` |
| P-4 | Medium | Install from URL and "Update code" accepted plain http (`http.DefaultClient`, no cap). | `CheckPluginURL`/`FetchPlugin`: https only (http only for localhost, 127.0.0.1 and ::1), the final URL after redirects is checked too, 16 MiB cap (too large is an error, not a silent cut), 30 s timeout. Taut's warning ("Only install code you trust! Plugins can do anything you can do in Slack.") now also shows before "Update code". | `TestPluginURLs` |
| P-5 | Medium | Plugin `fetch` had no body cap (a multi-GB response could exhaust memory) and no Go-side scheme check. | Bodies are capped at 128 MiB, and Go refuses anything but http(s), `file:` included, whatever `Options.Fetch` is. | `TestFetchSchemesAndSize` |
| P-6 | Medium | The UI goroutine could block for up to 10 s cold-starting the runtime or restarting it after a crash (`Bridge.mount`, `SetRects`, `RenderEditor` from Preferences layout). | The Bridge runs at once on a live runtime and otherwise starts one in the background. Only one start runs at a time. | `TestBridgeDoesNotBlockOnStart` |
| P-7 | Medium | `host_rejection` read its i32 flag from all 64 bits of the wasm value, only the low 32 of which are defined. This is the `dom.test.js` failure that got 5c18e8b reverted on `ui`. | Reads `uint32(st[2])`. | `TestJSSuites/dom.test.js` |
| P-8 | Low | Plugin-chosen storage namespaces (`__clack.taut_kv`) never escaped `storage/`/`secrets/`, but `a/b`, `a~b` and `a:b` shared one file, and long or device names broke writes. | Names that aren't plain get a hash suffix. Existing file names are unchanged. | `TestStorageNamespaceFiles` |
| P-9 | Low | Storage, bytecode-cache and config.json writes went through a fixed `<path>.tmp`, so a symlink planted there was written through. | `O_EXCL` `CreateTemp` then rename, which replaces a link instead of following it. 0600 files, 0700 directories. | `TestWritesDontFollowSymlinks`, `TestBytecodeCacheIgnoresSymlinks` |
| P-10 | Low | Unchecked guest memory reads, and no wasm memory cap. | `hostCall`/`hostModule` check `mem.Read` and argument counts. A `malloc` returning 0 closes the runtime. Memory is capped at `MemoryLimit + 512 MiB`. Host-function panics, nested ones included, become JS errors. | `TestMemoryBombFailsCleanly`, `TestHostPanicNested`, `TestMicrotaskFloodInterrupted` |
| P-11 | Low | `window.open`, link clicks and `location.assign` could open browser tabs in a loop. | At most 5 per 10 s, like a popup blocker; extra calls are dropped with one warning. | `TestOpenURLRateLimited` |
| P-12 | Low | Taut's AccountSwitcher `checkAuth` sent a saved account's token and `d` cookie to its stored TeamURL without a host check, and left `d-s` out. Switching wrote creds.json with plain `0600` (nothing on Windows). | Only https slack.com hosts, `d` only (never `d-s`), and `slack.WriteCreds` (owner-only, Windows ACL). | `TestCheckAuthStaysOnSlack` (internal/taut/cookies_sec_test.go), `TestAccountSendsOnlyDAndWritesOwnerOnly` (internal/taut/accounts_security_test.go) |
| P-13 | Info | By design, and unchanged: any plugin can reach any namespace through `taut_kv`, saved accounts through `taut_accounts` (Taut's shape, including xoxd), and any Web API method through `taut_api`. `cookies.get` returns `d`; `cookies.set` replaces it in memory. There is no isolation between plugins (see S-16). | — | — |

Host functions plugins can reach:

- **wazero imports (`jr`)**: `host_call`, `host_interrupt`, `host_module`, `host_rejection`, `host_free`. glue.c calls them, not JS, and every pointer and length is bounds-checked.
- **WASI**: `clock_time_get`, `fd_close`, `fd_fdstat_get`, `fd_seek`, `fd_write`, `random_get`. No filesystem preopens, environment or arguments, and stdio is discarded.
- **`__jrh`** (held privately by env.js and deleted from the global):
  - log, timers, wake, now, random (64 KiB cap), digest, document and commit plumbing, gc.
  - storage: `localStorage`/`sessionStorage` only.
  - fetch: http(s) only, size cap, no credentials added by Go.
  - fetchAbort.
  - pickFiles: native picker; returns what the user picked.
  - openURL: rate-limited, then `openurl.Check`.
  - clipboardWrite. clipboardRead is refused by the UI.
  - tz: `time.LoadLocation`, which rejects `..` and absolute paths.
- **`__clack.taut_*`**: memory-only state and UI flags. `kv` is confined to `storage/` and `secrets/`. `callGo` is queued to the UI goroutine and never blocks. `thunk`/`callNative` run native thunks, and `api`/`messageMetadata` call Slack as the user, as designed. `cookie`/`accounts` are as described in P-13.
- **`__clack.slick_*`**: see Slick below. They're captured privately and removed from the global.

Runaway bounds:

- Every call into the guest has a deadline and an interrupt.
- Loops in C check the interrupt (P-2), and the watchdog is the backstop.
- QuickJS heap limit, wasm page cap, and fetch body caps.
- The UI goroutine never waits on the JS thread (P-6).
- A plugin that keeps crashing stays off (P-3).

### Slick

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| S-1 | High | A plugin could read any local file through its own `file` setting's **default**: `{type:'file', default:'~/.ssh/id_rsa'}`, read with `storedFileUrl` or the main half's `fs`, with no user action. | Only a value stored in settings.json counts (picked in Preferences, or in a settings.json the user dropped in). The schema default never does. | `TestFileSettingsOnlyReadWhatTheUserPicked` (internal/slick/security_test.go) |
| S-2 | High | `api.settings.set(fileKey, '/any/path')` made that path readable. | `settings.set` on a `file` key accepts only `""` or `stored:…`. | same |
| S-3 | Medium | `fs` had no protected paths, and symlinks weren't treated as a boundary. | Paths are made absolute and symlinks resolved. Anything under `$CLACK_HOME`, the Slick or Taut directories, or `/proc`, `/sys` and `/dev` is refused. Every read goes through the resolved path. Reads are capped at 16 MiB. | same (`..` path, creds.json, symlink to creds) |
| S-4 | Medium | `shell.openExternal` was checked only in JS. Calling `__clack.slick_openURL` directly or `require('electron').shell.openExternal` opened any scheme, in a loop. | A Go-side check: http, https or mailto only, no leading `-`, no spaces or control characters, a length cap, and at most 3 per 10 s. It then goes through `openurl`. | `TestMainCapabilitiesAreEnforced`, `TestExternalURLAndRateLimit` |
| S-5 | Medium | A main half could give itself capabilities by pushing to its own `capabilities` array. | The allowed set is fixed at load. It's what Go's cached declaration says (and Preferences shows), intersected with what the file declares, and checked with a captured `Set.prototype.has`. | `TestMainCapabilitiesAreEnforced` |
| S-6 | Medium | `protocol.register('https', …)` took over every fetch in the runtime, including other plugins' requests and their headers. | Valid scheme names only. `http`, `https`, `file`, `slack`, `data`, `blob` and similar are reserved. One owner per scheme, matched case-insensitively. | `TestProtocolSchemesAreValidated` |
| S-7 | Medium | An auto-update (every 3 h) could quietly add a privileged main half. | Updates must stay renderers. A main half only arrives when the user imports it. The stored source URL is re-checked before each update. | `TestURLInstallsAndUpdates` |
| S-8 | Medium | URL installs followed redirects to http. | The final URL must be https (or localhost). | code review (the test harness's fetch can't redirect) |
| S-9 | Medium | Install from file or URL ran the code at once, and a main half's capabilities were never shown. | `slick.Peek` reads the header without running it. Both install paths now ask first, with Slick's wording ("Plugins you install will have FULL ACCESS to your Slack data. Only install code you trust!"), the URL and its 3-hourly updates, and the main half's capabilities. The plugin's Source card lists them as well. | `TestSlickInstallText` (internal/ui/slick_prefs_install_test.go), `TestPeekReadsTheHeaderOnly` |
| S-10 | Medium | MessageLogger stores message contents on disk (policy). | Memory-only by default. See §4. | `TestMessageKeepingPluginsStayInMemory` |
| S-11 | Low | Plugin ids: `X.main` wrote `plugins/X.main.js` and became X's main half. `.`, `..` and hidden names were allowed. Case-only duplicates collide on macOS and Windows. | Those ids are rejected, and the id is checked before any file is read. | `TestPluginIDs` |
| S-12 | Low | `__clack.slick_*` host functions were on the global. | Captured privately and deleted from the global. Blob namespaces must match `plugin:<id>[:secrets]`. | `TestHostFunctionsAreOffTheGlobal` |
| S-13 | Low | Download errors (`*url.Error`) repeated the URL, which can carry `?token=`. | Only the cause is returned and logged. | code review |
| S-14 | Low | Native file pickers could be stacked in a loop. | One picker at a time; extra calls get "cancelled". | `TestFilePickerIsOneAtATime` |
| S-15 | Low | An imported theme's name came from the raw file name. | Base name, at most 100 characters. | `TestImportedThemeNameIsABaseName` |
| S-16 | Info | Plugins aren't isolated from each other. Every Taut and Slick plugin and main half shares one JS realm, so a malicious plugin can patch shared prototypes and reach other plugins' objects and storage. `bridge.readSettings` returns the whole settings.json, as in Slick. | Not fixable without a realm per plugin. Install-time consent is the boundary between plugins. The hard limits (files, URLs, pickers, CPU, memory) are enforced in Go. Declared capabilities are a consent label and stop a main half from escalating itself. They aren't a sandbox between plugins. | — |

MainCtx capabilities:

| capability | enforcement |
|---|---|
| settings, onSettingsChange, log, emit, emitTo, storage | always on, scoped to the plugin's id; namespace checked in Go |
| secrets | gated; Taut's 0600 secret store (but see S-16) |
| net.fetch | gated; same reach as a renderer's `api.fetch` |
| net.block / net.intercept | gated; they affect only `fetch` inside the JS runtime, never Clack's Go Slack client or the image cache. The handler sees `{url, method, resourceType}` only |
| protocol.register | gated, boot only, scheme validated, reserved list, one owner per scheme |
| notifications.filter | gated; one chain over the `showNotification` thunk |
| shell.openExternal | gated in JS, checked again in Go (schemes, injection, 3 per 10 s) |
| dialog.openFile | gated; native picker, one at a time, returns what the user chose |
| cookies | gated; Taut's cookie host |
| Node `fs` | read-only; only files the user picked; protected roots; 16 MiB |
| frames, media, switches | gated; no-ops |

## 4. Hack Club scraping policy

Policy (https://news.hackclub.com/news/scraping-use-policy/): Clack writes message contents to one place only, the signed-in account's cache directory (`<cache root>/v1/<team>-<user>/`, under the OS's user cache folder; 0700/0600; deleted on sign-out by `core.App.DropCache` and cmd/clack's SignOut), which keeps the last open conversation's newest 50 messages and message thumbnails for a warm start, like Slack's persisted store. Every disk write in the codebase was audited:

| writer | contents | verdict |
|---|---|---|
| `internal/slack/creds.go` creds.json | tokens, cookies, team URLs | no messages; owner-only (C-6) |
| `cmd/clack` `migrateConfig` | copies creds.json, ui.json | no messages; the old shared `cache/` moves into the account's directory when its sidebar.json was the same user's, else is deleted (`migrateLegacyCache`) |
| `internal/store` <account>/meta.json | users and profiles, custom emoji, usergroups, prefs, channel names, bots | metadata only, 0600 |
| `internal/store` <account>/boot.json | team, self, raw prefs, sidebar conversations (names, topics, purposes), sections, unread counts, open DMs, experiments, starred, the users/emoji/usergroups/bots those rows and the cached messages name | no message text; 0600; presence never cached |
| `internal/store` <account>/history.json | the newest 50 messages of the last open conversation | **message text**, 0600, the only such file; deleted on sign-out |
| `internal/ui/prefs.go` ui.json | layout, last route (ids only), collapsed sections | search routes are no longer saved (C-12), so search queries don't reach disk |
| `internal/imgcache` <account>/img | avatars (`avatars.slack-edge.com`, `ca.slack-edge.com`, gravatar), emoji (`emoji.slack-edge.com`, `a.slack-edge.com` emoji paths) and message thumbnails (`files.slack.com/files-tmb/…` without a query) | `DiskOK`/`ThumbOK` allow-lists; full files, Slack's image proxy, unfurl and GIF images stay in memory (tested). Bytes are written only after a successful decode (C-13), 0600 temp files renamed into place; 384 MB, least recently used out; deleted on sign-out |
| `internal/ui/popups/emoji_frecency.go` | emoji names and use times | no messages |
| `internal/ui/popups/lightbox.go` Download | a file the user explicitly saves to ~/Downloads | the user's choice; name sanitized (C-10), `O_EXCL` |
| `internal/jsrt/bytecode.go` | compiled bytecode of the boot scripts, the TautAPI and installed plugin bundles, keyed by engine + source hash, SHA-256 trailer | plugin code, not messages |
| `internal/taut` taut/storage, secrets, config | plugin data, by design (docs/PLUGINS.md) | see below |
| `internal/slick` settings.json, plugins/, themes | plugin code, settings and themes | no messages |
| `internal/core` file fetch cache, history (except history.json above), search, Activity, canvases, lists, drafts | — | memory only; drafts live on Slack's servers |
| logs | ids, counts, timings | no message text; output goes through `RedactWriter` |

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| C-12 | Low | The `search?q=…` route, which holds the search query, was saved to ui.json. | Search routes aren't saved. | code review |
| C-13 | Low | The image disk tier wrote bytes before checking they decoded as an image. | It writes only after a successful decode. | `TestBombNotCached` |
| C-10 | Low | Lightbox Download: path traversal and overwrites were already refused (`O_EXCL`, cleaned base name). Windows device names (`CON.png`) and truncation in the middle of a character weren't. | Device names get a `_` prefix; truncation happens on a rune boundary. | `TestLightboxSaveSanitizesName` |

Nothing in Clack bundles a plugin or enables one by default. A user has to install every Taut or Slick plugin.

**Message-keeping plugins (MessageLogger).** Slick's MessageLogger stores deleted and edited messages, with image previews, through `api.storage`. Running it is the user's choice, but it conflicts with the policy, so Clack keeps a short Go-side list of such plugins (`storesMessages` in internal/slick/messages.go; currently only MessageLogger, since nothing else in Slick's or Taut's plugin lists stores message text). Their storage (`api.storage`, `Cache`, stored files, `MainCtx.storage`/`secrets`) stays **in memory** and is gone when Clack quits. Preferences > Slick shows the policy note as a red banner in the plugin's detail view and asks before the plugin is installed or enabled. A **Keep on disk** switch asks again before allowing disk storage. Turning it back off moves the data to memory and deletes it from disk. Remove and Clear data clear both.

We chose memory-only over a warning alone because it follows the policy without breaking the plugin. MessageLogger still works during a session, the Slick acceptance stays at 53/53, and storing to disk becomes an explicit, informed choice. Data stored on disk before this change stays where it is, unused, until the user turns Keep on disk on or uses Clear data or Remove.

Other plugin storage (Taut `api.storage`, `localStorage`) persists on disk as in Taut (docs/PLUGINS.md). That's the plugin's own data.

## 5. Network and URLs from message content

Anyone who can post in a conversation you open controls URLs Clack might load:
- file shares, including `files.remote.add` files, whose `url_private` and `external_url` point at the sender's own server;
- unfurl and attachment images, Block Kit `image_url`, bot `icon_url`;
- custom emoji, theme and plugin CSS.

None of these may steal the session, read a file off your disk, reach your local network, or reveal your IP address to the sender. The rules live in `internal/slack/hosts.go` (which hosts get what) and `internal/slack/netguard.go` (which addresses are dialled). Every fetch Clack makes on its own goes through them. This section merges this review with live-slick's `urlsec` review; where the two differed, the stricter check was kept.

**TLS only.** The Slack API, the realtime socket, files and sign-in use https/wss. There's no `InsecureSkipVerify` anywhere.

**The session cookie and token go only to `https://slack.com` and its subdomains** (`slack.CookieURL`, `Client.credsOK`). That's the scope a browser gives Slack's `d` cookie (`.slack.com`).
- Hosts must be plain ASCII DNS names. They're compared exactly after lower-casing, at a dot boundary, on port 443 with no userinfo. So `slack.com.evil.io`, `evilslack.com`, `evil.com/slack.com`, `files.slack.com@evil.com`, a trailing dot, `%2e` escapes, punycode or Unicode look-alikes, IP literals, `http://` and other ports all get nothing.
- `slack-edge.com` gets no cookie either.
- `d-s` is never sent (see above).
- The only exception is the client's own configured API origin, which is how tests run against an httptest server.

**Credential-free fetches** (`slack.PublicHTTP`):
- No cookie, and redirects only to Slack-owned hosts.
- The dialer refuses non-public addresses on the IP actually dialled. That covers loopback, RFC 1918, CGNAT, link-local and cloud metadata, ULA, multicast and documentation ranges, so DNS rebinding doesn't help.
- `HTTP(S)_PROXY` is ignored, because a proxy would dial for us.
- Images load directly only from Slack's own hosts (`slack.com`, `slack-edge.com`, `slack-imgs.com`, `slack-files.com`) and Gravatar (`secure.gravatar.com` and its `i0`–`i3.wp.com` fallbacks), which Slack's client also loads directly.
- Every other image goes through Slack's image proxy (`https://slack-imgs.com/?c=1&o1=ro&url=…`, `slack.ProxyImage`), as the web client does. The sender's host never sees the request.
- GIF picker results (`gif.search`, Giphy media) load directly only after an exact `giphy.com` host check.
- Clack makes no link previews of its own. Unfurls are whatever Slack's servers attached.

| ID | Sev | Finding | Fix | Test |
|---|---|---|---|---|
| C-2 | High | SSRF from message content. Unfurl, image block and attachment URLs were fetched with the default client: `http://`, `127.0.0.1`, `169.254.169.254` (cloud metadata), 10/8 and so on. | `imgcache.PublicFetch` refuses non-https URLs, userinfo, private IP literals and `localhost`, then loads through `slack.FetchPublic` (proxy or Slack CDN) on `PublicHTTP`'s guarded dialer. 32 MiB cap. The lightbox Save button and GIF tiles use the same path. | `TestDialControlRefusesPrivate` (internal/slack/netguard_test.go), `TestPublicFetchRefusesURLs` (internal/imgcache), `TestPublicFetch`, `TestProxyImage` (internal/slack/hosts_test.go) |
| U-1 | Medium | IP tracking. Any image URL in a message was fetched from the sender's host, so posting an image revealed every viewer's IP address and when they looked. | Third-party images go through Slack's image proxy. Only Slack's own hosts, Gravatar and exact `giphy.com` load directly. | `TestProxyImage`, `TestGifAnimURLHost` |
| U-2 | Medium | A trusted URL could bounce a request off Slack. A `files.slack.com` URL that redirected (302 or 307, which resends the token body) to another host was followed. | A hop that leaves `CookieURL` drops `Cookie` and `Authorization` (also on https→http and port changes). A hop off Slack-owned hosts isn't followed at all. | `TestCredentialsStayOnSlack`, `TestCheckRedirectStripsCredentials` |
| U-3 | Medium | Remote files. A `files.remote.add` file's `url_private` on the sender's server was fetched by `core.Fetch`, and `Download` would have attached the cookie to any URL (see C-4). | `Download`, `UploadBytes` and the realtime socket refuse non-Slack URLs (`ErrUntrustedHost`) before connecting. `core.Fetch` sends the cookie only to `CredentialsOK` URLs, fetches Slack-owned hosts without it, and refuses everything else. | `TestCredentialsStayOnSlack`, `TestFetchRefusesNonSlack`, `TestRTMRefusesForeignSocket` |
| U-4 | Low | Host matching by suffix or substring: the GIF picker matched `giphy.com` anywhere in the host, and an IDN or a trailing dot could pass `HasSuffix(".slack.com")`. | One normaliser (`normHost`) and `HostUnder`/`CookieHost` everywhere, `IsSlackHost` included. | `TestCookieURL`, `TestSlackOwnedAndHostUnder`, `TestGifAnimURLHost` |
| U-5 | Low | Taut's `api.cookies.get({url})` returned the `d` cookie for any URL filter. | Scoped like Electron's cookie store: https slack.com URLs only. | `TestCookieURLScope` (internal/taut/cookies_sec_test.go) |
| U-6 | Low | Saved file names: bidi overrides (`\u202egnp.exe` displays as `exe.png`), and trailing dots left by truncation. | Bidi controls become `_`, and trailing dots and spaces are trimmed after truncation (on top of C-10). | `TestLightboxSaveNames` (internal/ui/popups/lightbox_sec_test.go) |
| C-3 | Medium | Decompression bombs. A tiny PNG declaring 100000×100000 would try to allocate about 40 GB. | Header check before decoding: at most 16384 px per side and about 42 Mpx. GIF canvases are capped at 1024 px. | `TestDecodeRefusesBombs`, `TestBombNotCached` |
| C-15 | Info | Because the public client ignores `HTTP(S)_PROXY`, third-party images don't load behind a mandatory proxy. Slack's API and files use the normal transport. | Accepted trade-off. | — |
| D-3 | Medium | Auto-update downloads and runs a new Clack. A tampered or truncated download, a redirect off GitHub, or an oversized body could install the wrong code or fill the disk. Releases aren't code-signed yet. macOS builds are ad-hoc signed and not notarized, and the Windows installer and exe carry no Authenticode signature. | `internal/desktop/autoupdate.go`: release metadata and packages come over https from api.github.com, github.com, objects.githubusercontent.com and release-assets.githubusercontent.com only, every redirect included (the `tlsfp.Mixed` client sends these hosts through Go's default transport, never with Slack credentials). Clack's own User-Agent is sent. The size is capped at 400 MB and at what the release declares. A package is kept and offered only when its SHA-256 matches its line in the release's `SHA256SUMS`, and the hash is checked again before installing. A missing line or a mismatch is never offered. Installing needs the user's choice to restart (Restart Clack in the update dialog, Restart to Apply Update in the menus, or Preferences' Install and restart). Linux `.deb`/`.rpm` go through pkexec's password prompt or the software installer, never silent root. **The checksum only guards against corruption.** `SHA256SUMS` sits in the same release as the packages, so anyone who can publish a release in the repository (a stolen maintainer token, a compromised workflow) can ship matching sums. Until releases are signed (Developer ID plus notarization, Authenticode, a detached signature checked against a key built into Clack), a compromised repo means compromised updates. Turn off "Download updates automatically" to review releases yourself. | `TestDownloadVerifies`, `TestDownloadTampered`, `TestDownloadResumes`, `TestDownloadCap`, `TestDownloadHosts`, `TestPickAsset`, `TestUpdatePhasesPresentation`, `TestUpdateErrorsPresentation`, `TestOnlyTLSFPDials` |

**When you add a fetch** (canvas embeds, huddle assets, new previews):
- Use `core.Fetch` for authenticated Slack URLs, and `imgcache.PublicFetch` or `slack.FetchPublic` for everything else.
- Never call `http.Get` or `http.DefaultClient` on a URL taken from a message.
- Never build an `http.Transport`, use `http.DefaultClient`/`DefaultTransport`, or import `crypto/tls`: a connection to Slack must come from `internal/slack/tlsfp` (C-19). `TestOnlyTLSFPDials` enforces it.

Plugin `fetch` reaches any http(s) host by design: no CORS, as in Taut and Slick, and Clack adds no Slack cookie to it. See §3 for its limits.

## 6. Supply chain and CI

| ID | Sev | Finding | Fix | Verification |
|---|---|---|---|---|
| SC-1 | Medium | The Go module/build cache could be poisoned. Every workflow shared one setup-go cache key, and the taut and slick jobs run third-party code (`npm run build:user-plugin`, Slick's esbuild) before setup-go saves that cache. ci and release builds would then restore it. | `cache: false` in the jobs that run third-party code, and in release.yml. | all workflows green |
| SC-2 | Medium | Actions were referenced by movable tags, and setup-bun installed whatever bun was newest. | Every `uses:` is pinned to a full commit SHA with a version comment. bun is pinned to 1.4.4. supply-chain.yml fails on any unpinned action. | supply-chain.yml |
| SC-3 | Medium | Token scope. release.yml gave `contents: write` to every job; the other workflows had no `permissions:`. Checkouts left the token in `.git/config`, where third-party npm/bun code could read it. | Top-level `contents: read`; `contents: write` only on release's publish job; `persist-credentials: false` everywhere. | — |
| SC-5 | Medium | Taut (jeremy46231/taut) and Slick (3kh0/slick) were built from moving `main`. | Default to fixed commits (`TAUT_PIN`, `SLICK_PIN`). The `ref` input still overrides, and supply-chain.yml checks the pins match across workflows. npm/bun installs already used `--ignore-scripts`. | taut manual run with `ref=main` |
| SC-6 | Medium | jsrt.wasm: wasi-sdk was piped from `curl` into `tar` unchecked, QuickJS-NG came from a movable tag, and nothing compared the committed binary with what the source builds. | wasi-sdk sha256 pinned, QuickJS-NG v0.18.0 checked against its commit, and the built wasm must equal the committed one byte for byte. It is reproducible. | jsrt.yml `wasm` job |
| SC-4 | Low | `${{ inputs.ref }}` was pasted into `run:` (script injection by people with write access). | Passed through `env:`, quoted, and anything empty or starting with `-` is refused. | — |
| SC-7 | Info | Vendored React (react 19.3.1, react-reconciler 0.34.0, scheduler 0.28.0) is byte-identical to the npm tarballs, with sha512 checked against registry integrity. | Versions, sha256 and integrity recorded in internal/jsrt/vendor/README.md. supply-chain.yml re-checks both on every push. | supply-chain.yml |
| SC-8 | Low | Missing license notices: QuickJS-NG (MIT) inside jsrt.wasm and CLDR data (Unicode-3.0) in js/intl.js. | Added vendor/LICENSE.quickjs-ng and LICENSE.unicode. Still open: the repo has no top-level LICENSE, and release binaries ship without a third-party notices file. | — |
| SC-9 | Info | No workflow uses Slack credentials. The only secret is `GITHUB_TOKEN` in the publish step. Artifacts hold binaries, screenshots of synthetic data, logs and built GPL plugins. No `pull_request_target`. | — | — |
| SC-10 | Info | `go mod verify` passes, and go.sum has only expected modules. | Runs in supply-chain.yml (`go mod download all && go mod verify`). | supply-chain.yml |

Dependency licenses: gioui.org (Unlicense or MIT), wazero (Apache-2.0), coder/websocket (ISC), refraction-networking/utls (BSD-3, pure Go), andybalholm/brotli (MIT) and klauspost/compress (BSD-3; zstd), both linked by uTLS for certificate compression, which Clack's hello doesn't offer, godbus/dbus (BSD-2), golang.org/x/* (BSD-3), go-text/typesetting (Unlicense or BSD-3), go-ole (MIT), git.wow.st/gmp/jni (BSD-3), eliasnaur.com/font (Roboto license), Lato (OFL 1.1, assets/fonts/OFL.txt), React, react-reconciler and scheduler (MIT), QuickJS-NG (MIT), CLDR (Unicode-3.0). Taut and Slick (GPL-3.0) are never in the repo or the binaries; CI clones them only to build test plugins.

## Session safety

Clack never calls `auth.signout` or `auth.revoke`. "Sign out on this device" only deletes `creds.json`. See README "Sign in".

Clack loads one Slack web page with a session cookie: during desktop sign-in, `https://app.slack.com/auth?app=client&return_to=/client/<team>&teams=&iframe=1`, with the cookie Slack has just vended to Clack and Clack's own User-Agent, to read the session's tokens. That is the hidden page Slack's own web client (and so Slack desktop's window) loads with a fresh session to get its tokens. It loads nothing else, never `/ssb/redirect`, and never any page with a cookie copied from the browser.

The recommended sign-in (README "Sign in with a Slack link") gives Clack its own session, so the fingerprint problem below doesn't apply to it: Slack vended that session to Clack (the exchange says `ssb=1`, with Clack's own User-Agent, C-18), and Clack sends every cookie Slack set, follows their rotation, and signs DPoP proofs if the link asked for them. Whether Slack keeps that session when the browser signs out isn't verified yet (docs/DESKTOP-SIGNIN.md, "As built"). The rest of this section is about pasted sessions (Advanced).

Clack runs on a session copied from the user's browser. Slack can end that session everywhere, browser included. It did so three times for one tester running v0.1.0-alpha.1 on Enterprise Grid (2026-11-08). Nothing Clack sends asks for a sign-out. Alpha.2 tried to match the browser's fingerprint more closely and fared no better. The live result (docs/SIGNOUT-AUDIT.md "Result") showed why: Grid ends a session when a request claims to be a browser and plainly isn't one. So Clack now stops claiming (C-18):

| | Slack web client (Chrome, recorded 2026-11-08) | v0.1.0-alpha.1 | Now |
|---|---|---|---|
| User-Agent | the browser's own (Chrome 157 on that OS) | always `X11; Linux x86_64 … Chrome/142`, whatever the OS | `Clack/<version> (+https://github.com/some-du6e/yoloslackgoclient)` on every request, every OS, both session kinds |
| `sec-ch-ua*` client hints, `Sec-Fetch-*`, `Origin`, `Referer` on API calls | sent | `Origin` only | none |
| Gateway URL params | `slack_client=desktop`, org token, `gateway_server=T…-2`, `enterprise_id`, `flannel=3`, `agent_version=<build ts>` | the same, except `agent_version` = the current time on every dial | the same, except `agent_version` is fixed per run and a few hours in the past |
| Socket handshake | `Origin: https://app.slack.com`, `Pragma`/`Cache-Control: no-cache`, all `.slack.com` cookies | Origin, `d` only | Clack's User-Agent and the cookie (`d` only on a pasted session); no `Origin`, `Pragma` or `Cache-Control`. A server reads `Origin` to tell which web page opened a socket. Nothing in Clack's code or Slack's docs says the gateway needs it for a token-and-cookie socket, and Slack's own SDKs dial it without one |
| Socket opened | when the page loads | automatically after boot, retrying forever | only after the user opts in; gives up after 8 failed connects |
| Cookie GETs | the browser loads Slack pages itself | any https `*.slack.com` URL from message content, following redirects | file hosts and file paths only (`files*.slack.com`, `/files-pri/`, `/files-tmb/`), redirects to pages stopped, HTML answers dropped (`ErrNotAFile`) |
| After sign-out or account switch | — | the old session's socket and loops kept running | `core.Shutdown` stops them |

The User-Agent turned out not to be the trigger. Go's TLS ClientHello was, and Clack now sends OpenSSL 3.0.13's over HTTP/1.1 to every Slack host (C-19). The honest User-Agent stays: it is closer to the truth and costs nothing. One exception, outside Slack: a huddle's signaling socket goes to Amazon Chime (the meeting's `SignalingURL`, no Slack cookie or token) and still sends `Origin: https://app.slack.com`, as Slack's web client does, because Chime's signaling may check it and huddles can't be tested live (internal/huddle/session.go). The socket is opt-in because we can't test it against a real Grid account without risking it.

## Reporting

Please report vulnerabilities through a private security advisory on this repository, not a public issue.

## Open items

- Keychain storage (C-14), which would also cover the DPoP key (C-17).
- A live test of desktop sign-in on a throwaway account: DPoP on Grid, and whether the session survives a browser sign-out (docs/DESKTOP-SIGNIN.md).
- Isolating plugins from each other (S-16) would need a JS realm per plugin.
- Plugin `fetch` can reach localhost and LAN addresses (no CORS, by design). Consider the PublicFetch address check for plugin traffic, behind a per-plugin opt-out for local development servers.
- A top-level LICENSE and a third-party notices file in releases (SC-8).
