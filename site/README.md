# Clack's website

The marketing and download page for Clack, built to load instantly. It's one
HTML file with its CSS and its only script inlined, system fonts, AVIF and
WebP screenshots with `srcset`, and no requests to any other host. There's
no framework and nothing renders on the client. The one script points the
download buttons at the build for the visitor's system, and without it every
button goes to the download section, which lists all three.

Nothing here is public yet. The canonical base is https://getclack.club, but
GitHub Pages isn't turned on and the domain still has Porkbun's parking
records ([Going live](#going-live-on-github-pages-at-getclackclub)).

## Files

| path | what it is |
|---|---|
| `src/index.html` | the page, a template (see the top of `build.mjs`) |
| `src/style.css`, `src/os.js` | inlined into the page by the build |
| `src/404.html` | the not-found page |
| `build.mjs` | writes `dist/`, with Node built-ins only |
| `site.json` | the canonical base (`url`) and the GitHub repository |
| `CNAME` | `getclack.club`, copied into the GitHub Pages upload |
| `data/release.json`, `data/plugins.json` | snapshots of the newest release and the plugin Library index |
| `img/` | screenshots from CI, made by `tools/images.mjs`, with `images.json` saying where each came from |
| `tools/check.mjs`, `tools/weigh.mjs`, `lighthouserc*.json` | the budgets CI holds the page to |
| `package.json` | Lighthouse CI for CI, and sharp for `tools/images.mjs`; the build needs neither |

## Build it and look at it

Node 22 or later:

```sh
cd site
node build.mjs                     # dist/ from the snapshots in data/
node build.mjs --refresh           # fetch the newest release and Library index first
python3 -m http.server -d dist 8080
```

`--preview` makes a build for a preview host: `noindex`, and a robots.txt
that turns every crawler away. `--strict` fails when any download is
missing from the release.

The download links come from the newest `v*` release that has its files
up. Every Clack release so far is a pre-release, so GitHub's
`/releases/latest` doesn't work and the build bakes the version into the
links. The Pages deploy runs `--refresh` each time, so it should run after
each release (see step 6 below).

## Budgets

`.github/workflows/site.yml` runs on every push or pull request that
touches `site/`, and by hand with `gh workflow run site.yml`:

- `tools/check.mjs`: `index.html`, with all of its CSS and JS, is at most
  30 KB gzipped. Nothing loads from another host. Every local file a page
  names exists, every image has alt text, width and height, and every
  image but the hero is lazy.
- `tools/weigh.mjs`: headless Chrome loads the page from a gzip server on
  127.0.0.1 at desktop 1x and 2x and on two phones, in light and dark. It
  scrolls to the bottom so every lazy image loads. A whole visit has to stay
  under 300 KB, no request may leave 127.0.0.1, and the page must not scroll
  sideways.
- Lighthouse CI on the home page, mobile and desktop, three runs each:
  performance at least 99 on the median run, accessibility 100, FCP at
  most 1 s and LCP at most 1.2 s on mobile (0.4 s and 0.5 s on desktop),
  TBT at most 50 ms, CLS at most 0.01, the page at most 30 KB on the wire,
  and no third-party, font, script or stylesheet requests.

The run's summary has both tables, and the `site` artifact has the build
and the Lighthouse reports.

## Screenshots

Every picture is `clack demo`, a made-up company on a fake Slack server
inside the app, taken by the demo workflow (`.github/workflows/demo.yml`).
No real workspace, person or message appears on the site.

| picture | file in the run's `demo-shots` artifact | test |
|---|---|---|
| hero | `readme/demo-main-{light,dark}.png`, #sales-leaderboard | `TestDemoReadmeShots` |
| Channels | `readme/demo-huddle-{light,dark}.png`, #party-planning-crew with a live huddle | `TestDemoReadmeShots` |
| Threads | `readme/demo-thread-{light,dark}.png`, #it-help's busiest thread | `TestDemoReadmeShots` |
| Activity | `readme/demo-activity-{light,dark}.png` | `TestDemoReadmeShots` |
| Themes | `site/demo-theme-{jade,lagoon,clementine}-light.png`, `site/demo-theme-mood-indigo-dark.png`, top-left 720×450 | `TestDemoSiteShots` |

The light or dark version follows the visitor's system. The current set
is from demo run 44198572787. To take new ones:

```sh
gh workflow run demo.yml --repo some-du6e/clack --ref <branch>
gh run download <run> --repo some-du6e/clack --name demo-shots --dir <dir>
cd site && npm install && node tools/images.mjs --run <run> --from <dir>
```

## The canonical base

`site.json`'s `url` is the only place the domain lives. The build uses it
for the canonical link, `og:url`, `og:image`, `sitemap.xml` and the
sitemap line in robots.txt. `SITE_URL` or `--site-url` overrides it for one
build, and an empty value leaves those tags out. Every other URL on the
page is relative, so the same build works at the domain's root, on a
preview host, or under `some-du6e.github.io/clack/` before the domain is
attached.

## Going live on GitHub Pages at getclack.club

None of this is done yet. getclack.club is registered at Porkbun, and on
2026-11-17 it still had Porkbun's parking records: the apex resolved to
Porkbun's parking addresses, and every subdomain was a CNAME to
pixie.porkbun.com.

1. **Remove the parking records.** In Porkbun, open Domain Management,
   then getclack.club, then DNS. Delete the ALIAS record on the apex and the
   wildcard CNAME `*.getclack.club`, both of which point at
   `pixie.porkbun.com`. GitHub warns against wildcard records, because they
   let someone else's Pages site claim your subdomains.

2. **Add GitHub Pages' records.** Leave Host empty for the apex:

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
   With both the apex and `www` set up, GitHub redirects
   www.getclack.club to getclack.club.

3. **Verify the domain for the account.** In github.com/settings/pages
   (the some-du6e account's settings, not the repository's), add
   getclack.club. GitHub shows a TXT record. Add it at Porkbun with Host
   `_github-pages-challenge-some-du6e` and GitHub's code as the Answer, then
   click Verify. After that, only some-du6e's repositories can use the
   domain.

4. **Turn on Pages for the repository.** In Settings, then Pages, set
   Source to "GitHub Actions" and Custom domain to `getclack.club`, then
   Save. GitHub checks the DNS. When the certificate is ready, which can
   take a while after the DNS change, tick "Enforce HTTPS".

   With an Actions deploy, GitHub reads the domain from this setting and
   ignores a CNAME file in the upload. `site-pages.yml` copies `site/CNAME`
   into the upload anyway, so the upload names its domain, and a deploy from
   a branch would read it.

5. **Deploy.** `site-pages.yml` has to be on `main` before it can be
   dispatched. Then run
   `gh workflow run site-pages.yml --repo some-du6e/clack` and open
   https://getclack.club.

6. **Redeploy on releases.** Uncomment `workflow_run` in `site-pages.yml`
   so each release redeploys with its downloads, and `push` to deploy site
   changes that land on `main`.

## Or Cloudflare

The same `dist/` works on Cloudflare, and the temporary preview is a
Cloudflare Worker with static assets. To serve getclack.club from
Cloudflare instead of GitHub:

1. Add getclack.club as a site in a Cloudflare account. At Porkbun, under
   the domain's Authoritative Nameservers, replace Porkbun's four
   nameservers with the two Cloudflare gives you. Don't add GitHub's
   records from above.
2. Deploy the build:
   `node build.mjs --refresh && wrangler deploy ./dist --name clack-site --compatibility-date <today>`.
3. In the Worker's Settings, then Domains & Routes, add `getclack.club`
   and `www.getclack.club` as custom domains. Cloudflare creates the DNS
   records and the certificate.

Cloudflare Pages works the same way, with a custom domain on the Pages
project. It reads `_headers` and `_redirects` files, which this site
doesn't need. Use one host for the apex, not both.
