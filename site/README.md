# Clack's website

The marketing and download page for Clack. It follows the layout of
slack.com's homepage section for section, with values measured from it
(sizes, spacing, colours, buttons and breakpoints): the nav, the hero with
its product frame, the aubergine band, the section tabs, four feature
sections, the stats band, the numbers, the cards and the closing band. Like
slack.com's, the section tabs stick under the nav only through the feature
sections, and the pill tabs under the hero sit on one row from 1024px and
are hidden below that. The name, the logo, the screenshots and every word are Clack's own,
and nothing on the page is copied from slack.com.

It loads instantly: one HTML file with its CSS and its only script inlined,
one 6 KB web font for the headings, AVIF and WebP screenshots with
`srcset`, and no requests to any other host. There's no framework and
nothing renders on the client. The script points the download buttons at
the build for the visitor's system. Without it every button goes to the
download section, which lists all three.

The site is live at https://getclack.club, served by a Cloudflare Worker
([Cloudflare](#cloudflare-live)). GitHub Pages isn't turned on; its steps
below are the alternative.

## Files

| path | what it is |
|---|---|
| `src/index.html` | the page, a template (see the top of `build.mjs`) |
| `src/style.css`, `src/os.js` | inlined into the page by the build |
| `src/404.html` | the not-found page |
| `build.mjs` | writes `dist/`, with Node built-ins only |
| `site.json` | the canonical base (`url`) and the GitHub repository |
| `CNAME` | the domain again, for GitHub Pages; `tools/check.mjs` keeps it in step with `site.json` |
| `data/release.json` | the newest complete release and its downloads |
| `data/plugins.json` | the plugin Library index: names, systems and ids |
| `data/perf.json` | the numbers in the stats band and where they come from |
| `fonts/` | the heading font, its coverage list and its licence |
| `img/` | the screenshots, made by `tools/images.mjs`, with `images.json` saying where each came from |
| `tools/check.mjs`, `tools/weigh.mjs`, `lighthouserc*.json` | the budgets CI holds the page to |
| `tools/shots.mjs` | before and after pictures of the page against an earlier commit's build |
| `demo/` | the web demo's page; its program comes from CI ([The web demo](#the-web-demo)) |
| `tools/demo-pack.mjs`, `tools/demo-test.mjs` | the web demo's packing and its test in headless Chrome |
| `tools/font.py` | makes the heading font from TeX Gyre Adventor |
| `wrangler.jsonc` | for serving the site from Cloudflare instead |
| `package.json` | Lighthouse CI for CI, and sharp for `tools/images.mjs`; the build needs neither |

The plugin verdicts on the page come from `internal/pluginlib/compat.go`,
the table Clack itself shows in Preferences, so the build reads it from the
repository.

## Build it and look at it

Node 22 or later:

```sh
cd site
node build.mjs                     # dist/ from the snapshots in data/
node build.mjs --refresh           # fetch the newest release and Library index first
python3 -m http.server -d dist 8080
```

`--pages` adds the CNAME file for GitHub Pages. `--preview` makes a build
for a preview host: `noindex`, and a robots.txt that turns every crawler
away. `--strict` fails when any download is missing from the release.

The download links come from the newest release, by tag version, that has
every download up. If the newest release is missing one, an ARM build for
example, the page stays on the last complete release. Every Clack release
so far is a pre-release, so GitHub's `/releases/latest` doesn't work and the
build bakes the version into the links.

## Budgets

`.github/workflows/site.yml` runs on every push or pull request that
touches `site/`, `assets/icon.svg` or `compat.go`, and by hand with
`gh workflow run site.yml`. It checks two builds: the committed snapshot,
and a fresh `--refresh --pages` build like the one the Pages deploy makes.
For each:

- `tools/check.mjs`: `index.html`, with all of its CSS and JS, is at most
  30 KB gzipped. Nothing loads from another host, the only font is the
  local subset, and it covers every character the headings use. Every local
  file a page names exists, every image has alt text, width and height, and
  every image but the hero is lazy. The production build is indexable, with
  a canonical link, a sitemap and CNAME; `--preview` checks the opposite.
- `tools/weigh.mjs`: headless Chrome loads the page from a gzip server on
  127.0.0.1 on desktops at 1440 and 1280, a 1024 laptop, a tablet and two
  phones, and scrolls to the bottom so every lazy image loads. A whole visit has to stay under 300 KB
  with the font, load one font, keep every request on 127.0.0.1, and fit
  the window. Before that it loads a page that's too wide on purpose and
  fails the run if the width check doesn't catch it.
- Lighthouse CI on the home page and the 404 page, mobile and desktop, three
  runs each: performance at least 99 on the median run, accessibility 100,
  FCP at most 1 s and LCP at most 1.2 s on mobile (0.4 s and 0.5 s on
  desktop), TBT at most 50 ms (the median of the three runs: Lighthouse's
  median run was one with a 121 ms stall on a busy runner, beside 0 and
  42 ms; 150 ms on the 404 page, which runs no script of its own but whose
  one long task, Chrome's own work about 600 ms in, comes and goes between
  runs: 68, 94 and 0 ms in one), CLS at most 0.01, the page at most 30 KB
  on the wire, one font, and no third-party, script or stylesheet
  requests.
  SEO is checked on the home page only, because the 404 page is noindex on
  purpose.

`site-pages.yml` runs the same checks on the build it deploys, and uploads
that same directory.

The workflow's compare job builds the site at an earlier commit
(`SITE_BEFORE` in `site.yml`, now round 2's 1608949c, the build
getclack.club went live with) next to this one, and `tools/shots.mjs`
takes the same sections at the same widths from both: the top of the page,
the strip and pills, the sections, the stats band, the footer at 1024px
and two phone views. The pairs are in the run's `site-before-after`
artifact, with an `index.html` that shows them side by side. An empty
`SITE_BEFORE` skips the job.

## Screenshots

Every picture is `clack demo`, a made-up company on a fake Slack server
inside the app, taken by the demo workflow's `TestDemoSiteShots`
(`.github/workflows/demo.yml`, `internal/ui/demo_site_test.go`). No real
workspace, person or message appears on the site. The shots leave out the
app's "Demo workspace" banner, and the page says once, under the hero, that
the workspace is made up. slack.com has no dark mode, so neither does the
page, and every shot is the light theme except the Mood Indigo tile.

| picture | file in the run's `demo-shots` artifact |
|---|---|
| hero | `site/demo-main.png`, #sales-leaderboard |
| aubergine band | `site/demo-emoji-picker.png`, cropped to 1314×900 |
| Channels | `site/demo-huddle.png`, #party-planning-crew with a live huddle |
| Threads | `site/demo-thread.png`, #it-help's busiest thread |
| Activity | `site/demo-activity.png`, the newest mention open beside the feed |
| Themes | `site/demo-theme-{jade,lagoon,clementine}-light.png`, `site/demo-theme-mood-indigo-dark.png`, top-left 720×450 |

The current set is from demo run 44440446799. Each demo run also runs
`tools/images.mjs` on its own shots and uploads the result as `site-img`,
so new pictures need no local tools:

```sh
gh workflow run demo.yml --repo some-du6e/clack --ref <branch>
gh run download <run> --repo some-du6e/clack --name site-img --dir <empty dir>
cp <empty dir>/* site/img/
```

The demo places its messages from the clock, so where the day dividers
land changes with the time of the run: look at the pictures before
committing them. `node tools/images.mjs --run <run> --from <demo-shots dir>`
makes the same files locally, with sharp from `npm install`.

## The web demo

`demo/` is Clack in the browser, at getclack.club/demo/: `cmd/clack-web`,
the real Clack built with Gio's js/wasm target, signed in to Quillfeather
Paper Co. with the fake Slack server running in the same page. The homepage
only links to it and never loads any of it (`tools/weigh.mjs` fails a visit
that does).

It can't reach the network:

- A page has no sockets, so the fake serves on an in-memory listener
  (`internal/memnet`) and every connection Clack opens goes to it
  (`demo.StartInMemory`, `tlsfp.SetLoopbackDialer`), the realtime socket's
  too. `third_party/websocket` is coder/websocket without its browser
  backend, which would have opened a real WebSocket to 127.0.0.1.
- Files live in the page's memory (`demo/fs.js`, an in-memory file system
  for Go's js runtime), so a reload starts over.
- The CI test fails on any request or WebSocket that isn't to its own
  local server.

Left out, behind `GOOS=js` build constraints, and said so on the page in one
line:

- Plugins: the JavaScript runtime (`internal/jsrt/engine_js.go` and
  `pack_embed_js.go`: no QuickJS, React or wazero), Taut's and Slick's own
  JavaScript (`js_embed_js.go` in each), the plugin benchmark's data and
  Taut's author credits (`internal/taut/authors_js.go`).
- Huddles, their audio and their video. `internal/huddle/rtc`,
  `internal/huddle/opus` and `internal/huddle/h264` stand in for pion/webrtc,
  opus-go and goh264 and never open a connection or a codec, so joining
  fails with "huddles aren't part of the browser demo". `bgblur_js.go` keeps
  the MediaPipe selfie model and the TFLite interpreter out, and
  `apm_js.go` the microphone's RNNoise model. The LGPL-2.1 decoder (goh264)
  and the Apache-2.0 model aren't in the demo at all.
- Desktop integrations, which have no browser counterpart (Clack's
  fallbacks for other systems apply), and Slack's taskbar badge images
  (`internal/desktop/badge_embed_js.go`).

Before anything big downloads, the page checks for WebAssembly and WebGL
(Gio draws with WebGL 2 or 1) and says plainly if one is missing. On a
phone, a touch-only device or a small screen, or with the browser's
save-data on, it gives the download's size (build.json's brotli and
unpacked sizes) and waits for a tap. The download shows as a percentage of
that size; screen readers hear it each quarter, not each chunk. What
crosses the network is main.wasm's brotli or gzip size when the host
compresses `application/wasm` (the demo job's summary has all three), and
the whole of it when it doesn't. A bar above Clack's window links back to
the homepage, the download and the licences: build.mjs copies Clack's
LICENSE and THIRD_PARTY_NOTICES.md into `dist/demo/` as `LICENSE.txt` and
`THIRD_PARTY_NOTICES.txt`, which the homepage's footer links too.

It's built and tested in CI only, by the site workflow's demo job:

1. `go test ./internal/memnet/` and `TestInMemory` (internal/demo), the
   in-memory wiring on a normal process.
2. `node build.mjs`, then
   `GOOS=js GOARCH=wasm go build -o dist/demo/main.wasm ./cmd/clack-web`,
   with Go's `wasm_exec.js` beside it.
3. `tools/demo-pack.mjs` splits main.wasm into parts of at most 24 MiB
   (Cloudflare's static assets take 25 MiB a file) and moves them, with
   the page's scripts, into `demo/p/<hash>/`, named by a hash of all of
   them. The page, the one entry point, loads its scripts from there and
   carries the manifest (the parts and their sizes) inline, so a page and
   its parts are always one build; `_headers` (from build.mjs) serves
   `/demo/p/*` as immutable for a year, and everything else revalidates.
   build.json is the same manifest, for the record.
4. `tools/check.mjs --demo`.
5. `tools/demo-test.mjs`: headless Chrome, with WebGL on SwiftShader as a
   machine without a GPU has it, opens the demo, waits for the first frame
   and the channel list, types a message into the composer, sends it with
   Enter and checks the fake has it, with no console error. Its screenshots
   and timings are the run's `site-demo-test` artifact.

The run's `site-demo` artifact is `dist/demo/` with the program, ready to
deploy. To deploy the site with it:

```sh
node build.mjs --refresh
gh run download <site run> --repo some-du6e/clack --name site-demo --dir <empty dir>
cp -r <empty dir>/* dist/demo/
npm run deploy
```

The deploy is guarded: `npm run deploy` runs `tools/check.mjs --production
--demo` before `wrangler deploy`, and wrangler.jsonc's build command runs
the same check before any `wrangler deploy`, so a `dist/` without the
program (the page alone, which says the demo isn't in that build) never
goes live behind TRY THE DEMO.

## The heading font

slack.com sets its headings in a licensed Avant Garde. The page uses
"Clack Heading", a subset of TeX Gyre Adventor Bold 2.501 from GUST's
e-foundry, which is free under the GUST Font License (`fonts/`). On the
page's strings its widths are within 1.4% of slack.com's, and cap height
and x-height are the same. The subset is 107 characters with kerning,
renamed as the licence asks of derived fonts. `@font-face` sets Avant
Garde's vertical metrics, so baselines land where slack.com's do, and the
fallback face is Arial Bold scaled to the same widths, so nothing moves when
the font arrives. To rebuild it, see the top of `tools/font.py`.

Body text uses the system UI font.

## The numbers

`data/perf.json` holds the stats and their notes, taken from one perf run
(`.github/workflows/perf.yml`). The current ones are from run 44220175072
on 557993a, the commit tagged v0.1.0-alpha.106. Perf builds that commit with
the `perfharness` tag so it can talk to a local fake server, which a
release build refuses. To update them, take a run on a release commit and
copy its startup.md (head rows) and scale.md (Windows) figures into
`data/perf.json`, notes included.

## The canonical base

`site.json`'s `url` and `CNAME` are the only places the domain lives, and
`tools/check.mjs` fails when they disagree. The build uses the URL for the
canonical link, `og:url`, `og:image`, `sitemap.xml` and the sitemap line in
robots.txt. `SITE_URL` or `--site-url` overrides it for one build, and an
empty value leaves those tags out. Every other URL on the page is relative.
The 404 page links to `/`, so it expects to be served at a domain's root,
as it will be on getclack.club.

## Going live on GitHub Pages at getclack.club

Not used: the domain went to Cloudflare instead. If it ever moves to GitHub
Pages, getclack.club is registered at Porkbun, whose parking records (an
ALIAS on the apex and a wildcard CNAME to pixie.porkbun.com) come back if
the nameservers return to Porkbun. Do the steps in this order. Pointing DNS at GitHub
before the domain is verified and claimed would let someone else's Pages
site claim it in the meantime, which GitHub's docs warn about.

1. **Verify the domain for the account.** In github.com/settings/pages
   (the some-du6e account's settings, not the repository's), add
   getclack.club. GitHub shows a TXT record. Add it at Porkbun (Domain
   Management, then getclack.club, then DNS) with Host
   `_github-pages-challenge-some-du6e` and GitHub's code as the Answer, then
   click Verify. After that, only some-du6e's repositories can use the
   domain.

2. **Turn on Pages and claim the domain for the repository.** In the
   repository's Settings, then Pages, set Source to "GitHub Actions" and
   Custom domain to `getclack.club`, then Save. The DNS check fails until
   step 3; that's expected. With an Actions deploy, GitHub reads the domain
   from this setting and ignores a CNAME file in the upload. The build adds
   `site/CNAME` anyway, so the upload names its domain.

3. **Point DNS at GitHub Pages.** At Porkbun, delete the parking records:
   the ALIAS on the apex and the wildcard CNAME `*.getclack.club`, both
   pointing at `pixie.porkbun.com`. GitHub warns against wildcard records.
   Then add these, with Host empty for the apex:

   | Type | Host | Answer | TTL |
   |---|---|---|---|
   | A | | 185.199.108.153 | 600 |
   | A | | 185.199.109.153 | 600 |
   | A | | 185.199.110.153 | 600 |
   | A | | 185.199.111.153 | 600 |
   | AAAA | | 2606:50c0:8000::153 | 600 |
   | AAAA | | 2606:50c0:8001::153 | 600 |
   | AAAA | | 2606:50c0:8002::153 | 600 |
   | AAAA | | 2606:50c0:8003::153 | 600 |
   | CNAME | www | some-du6e.github.io | 600 |

   The AAAA records are optional and give IPv6 visitors a direct route.
   With both the apex and `www` set up, GitHub redirects www.getclack.club
   to getclack.club.

4. **Deploy.** `site-pages.yml` has to be on `main` before it can be
   dispatched. Then run
   `gh workflow run site-pages.yml --repo some-du6e/clack` and open
   https://getclack.club.

5. **Turn on HTTPS.** When the certificate is ready, which can take a while
   after the DNS change, tick "Enforce HTTPS" in the Pages settings.

6. **Redeploy on releases.** Uncomment `workflow_run` in `site-pages.yml`
   so each release redeploys with its downloads, and `push` to deploy site
   changes that land on `main`. Until then the site stays on the release it
   was built with. The download links point into the release itself, so
   they break for as long as a rerun of release.yml has that release back
   in draft.

## Cloudflare (live)

getclack.club is served from Cloudflare, and the orchestrator does the live
deploys:

- The getclack.club zone is in Karim's Cloudflare account. Porkbun's
  "Connect to Cloudflare" switched the domain's nameservers to Cloudflare's.
- The Worker `clack-site` serves `dist/` as static assets. `wrangler.jsonc`
  holds its setup: getclack.club and www.getclack.club as custom domains,
  workers.dev off, and `404.html` for unknown paths. Deploy with
  `node build.mjs --refresh && npm run deploy`, with the web demo's program
  copied into `dist/demo/` in between ([The web demo](#the-web-demo)); the
  deploy refuses a `dist/` without it.
- Always Use HTTPS is on.
- A Response Header Transform Rule removes Cloudflare's Network Error
  Logging headers (`nel` and `report-to`), so visitors' browsers don't
  report failed loads to a.nel.cloudflare.com and the page keeps its "no
  requests to any other host" promise.

Previews go up with `wrangler deploy --temporary` and a private
`XDG_CONFIG_HOME`, never to the live domain. The GitHub Pages steps above
stay ready as the alternative; use one host for the apex, not both.
