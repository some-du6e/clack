<img src="assets/icon.svg" width="64" height="64" alt="">

# yoloslack

A fast 1:1 Slack client. A single Go binary talks to Slack with your browser session and serves a UI that looks like Slack's web client to a local browser window. It is built for the Hack Club Slack (Enterprise Grid) and works with other workspaces too.

## Log in

yoloslack signs in with your Slack `d` cookie, the long-lived session cookie your browser already has.

1. Open your workspace in a browser (e.g. https://hackclub.slack.com) and sign in.
2. Open DevTools → Application → Cookies → `https://app.slack.com`, and copy the value of the cookie named `d` (it starts with `xoxd-`).
3. Run:

   ```sh
   yoloslack login hackclub
   ```

   and paste the cookie when asked. yoloslack loads the workspace once to find your session tokens (team and, on Enterprise Grid, org), checks them with `auth.test`, and saves them to `~/.config/yoloslack/creds.json` (mode 600). Set `YOLOSLACK_HOME` to keep config somewhere else.

Treat `creds.json` like a password: it is a full session for your account.

## Run

```sh
yoloslack            # serve on http://127.0.0.1:7777/ and print the URL
yoloslack -app       # open a chromeless app window (Chrome, Chromium, Edge or Brave)
yoloslack -open      # open in your default browser
```

Flags:

| flag | default | |
|---|---|---|
| `-addr` | `127.0.0.1:7777` | listen address (keep it on loopback: the API acts as you) |
| `-app` | off | open the UI in an app window with its own profile (`~/.config/yoloslack/window`) |
| `-open` | off | open the UI in the default browser |
| `-dev <dir>` | embedded UI | serve the UI from `<dir>` (e.g. `./web`) instead of the copy embedded in the binary, so UI edits show up on reload without a rebuild |
| `-creds <file>` | `~/.config/yoloslack/creds.json` | credentials file |

Builds come from CI: every push runs gofmt, vet, tests and cross-compiles for Linux, macOS and Windows (`.github/workflows/ci.yml`). Download a binary from the run's artifacts.

## Speed

Measured on the Hack Club Slack:

- Cold boot is about 0.5 s with a warm metadata cache. Conversations, counts, sections, prefs and team info load in parallel; the 65k custom emoji, the user directory and usergroups come from the disk cache and refresh in the background. The first run also fetches emoji, which takes about a second.
- The realtime socket connects about 0.3 s after boot.
- Switching to a cached channel takes 2–30 ms server-side (cached ≈1.3 MB of HTML gzips to ~300 KB). Unread channels are prefetched in the background right after boot, at most 24 of them, 3 at a time. Opening an uncached channel costs one `conversations.history` call (~0.3 s).
- HTTP keep-alive/HTTP2 to Slack, at most 12 requests in flight, user lookups batched 100 per edge call, gzip to the browser, and a memory-only LRU for file thumbnails.

The server logs timings (never contents), e.g. `boot: ready in 512ms (…)`, `history C0123: 2ms (cache)`.

## Data & scraping policy

This follows the [Hack Club scraping policy](https://news.hackclub.com/news/scraping-use-policy/):

- **Message contents are never written to disk.** Messages, threads, files and rendered HTML live only in memory for the lifetime of the process, as in any Slack client. Nothing is logged except method names, channel ids, counts and timings.
- The only thing cached on disk is metadata, in `~/.config/yoloslack/cache/meta.json`: the user directory, the custom emoji map and usergroup names.
- File downloads go through an in-memory proxy and are not saved.
- yoloslack makes the same kind of requests the official web client makes, on demand and rate-limited. It does not crawl.
