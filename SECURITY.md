# Security

## URLs from message content (external files, images, links)

Anyone who can post in a conversation you open controls URLs that Clack might load. That covers file shares (including `files.remote.add` files, whose `url_private` and `external_url` point at the sender's own server), unfurl and attachment images, Block Kit `image_url`, bot `icon_url`, custom emoji, theme and plugin CSS. Clack must never let such a URL steal the session, read a file off your disk or reveal your IP address. The rules live in `internal/slack/hosts.go`; every fetch goes through them.

**The session cookie and token go only to `https://slack.com` and its subdomains** (`slack.CookieURL`), the same scope the browser gives Slack's `d` cookie (`.slack.com`). Hosts are compared exactly after lower-casing, at a dot boundary, so none of these get credentials: `slack.com.evil.io`, `evilslack.com`, `evil.com/slack.com`, `files.slack.com@evil.com`, `files.slack.com.` (trailing dot), `%2e` escapes, punycode or Unicode look-alikes, IP literals, `http://`, or a non-443 port. Slack's own CDN (`slack-edge.com`) gets no cookie either.

- `slack.Client.Download`, `UploadBytes` and the realtime socket refuse any other URL (`ErrUntrustedHost`) before connecting. A test fake server is trusted only because it is the client's configured API origin.
- Redirects: a hop that leaves `CookieURL` drops `Cookie` and `Authorization` (Go's client already drops a manually set Cookie off-domain; Clack also drops it on an https→http downgrade or a port change). A hop off Slack's hosts is not followed at all, so a `files.slack.com` URL can't bounce a request (or a 307-resent token body) to another host.
- `core.Fetch` (thumbnails, downloads) sends the cookie to slack.com URLs and fetches Slack's CDNs without it. It refuses everything else, including a remote file's `url_private` on the sender's host. The lightbox's Download button goes through it for files.
- Sign-in accepts only a slack.com workspace, and ignores an `auth.test` URL off slack.com. Taut's saved accounts are checked only against slack.com. Taut's `api.cookies.get({url})` returns the `d` cookie only for slack.com URLs, as Electron's cookie store does.

**No IP tracking.** Images on Slack's own hosts (`slack.com`, `slack-edge.com`, `slack-imgs.com`, `slack-files.com`) and Gravatar (profile pictures, which Slack's client loads directly) are fetched directly. Any other image goes through Slack's image proxy, `https://slack-imgs.com/?c=1&o1=ro&url=…`, as the web client does (`slack.ProxyImage`, `slack.FetchPublic`). The proxy refuses loopback and private targets. These fetches carry no cookie, and their redirects may only go to the same allowed hosts. GIF picker results (Slack's `gif.search`, Giphy media) load directly only after an exact `giphy.com` host check. Clack makes no link previews of its own: unfurls are whatever Slack's servers attached.

**Links** open in the system browser only for `http`, `https` and `mailto` (all three `openURL`s). `file:`, `javascript:`, custom schemes and paths are ignored.

**Downloads** (lightbox Save) go to `~/Downloads` under a cleaned single file name. Separators, `..`, control and bidi-override characters (`‮gnp.exe`) and Windows device names (`CON`, `NUL`, `COM1`…) are neutralised. Files are created with `O_EXCL`, mode 0644 (never executable), and never opened automatically.

**Plugins** (Taut, Slick) are code you chose to install and have full access, as in Taut and Slick: they can read the session token (`localConfig_v2`, `api.cookies`) and `fetch` any URL. Clack adds no Slack cookie to a plugin's own `fetch`. Only install plugins you trust.

Tests: `internal/slack/hosts_test.go` (host rules, Download/Upload refusal, redirects, public client, realtime socket, sign-in), `internal/core` `TestFetchRefusesNonSlack`, `internal/taut/cookies_sec_test.go`, `internal/ui/popups/lightbox_sec_test.go` (file names, saving, GIF hosts).

When adding a fetch (canvas embeds, huddle assets, new previews), use `core.Fetch` for authenticated Slack URLs and `slack.FetchPublic` for everything else. Never call `http.Get` on a URL taken from a message.

## Session safety

Clack never calls `auth.signout` or `auth.revoke`, and never loads Slack web pages with the session cookie. "Sign out on this device" only deletes `creds.json` (mode 0600). See README "Sign in".

## Reporting

Open a private security advisory on this repository.
