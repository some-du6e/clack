# Slack parity checks

`tools/parity.py` answers one question per dimension: does Clack still do what Slack does? A dimension is a group of Go tests whose expected values are Slack's own output, either Slack's JavaScript run in node or answers captured from live Slack. The answer is `same` or a description of how Clack differs. Every run also says which parts of Slack no check covers, so the totals can't be read as "Clack matches Slack".

The idea comes from horizon's `tools/parity.py`, which runs differential harnesses against vanilla Minecraft. Horizon's README carries a "vanilla parity 100%" badge, and Karim's verdict on it was that it isn't true: some things are missing. This report has no percentage on purpose, and a test (`test_the_report_never_claims_more_than_it_measured`) fails if one appears.

## Run it

```
python3 -I tools/parity.py                       # every dimension
python3 -I tools/parity.py emoji-rank markdown   # just these: only their packages build
python3 -I tools/parity.py --list                # dimensions, tests, snapshots
python3 -I tools/parity.py --out DIR             # also DIR/parity-report.md and .json
python3 -I tools/parity.py --refresh emoji-rank  # rerun Slack's side, rewrite its snapshot
python3 -I tools/parity.py --from DIR            # read go test -json files instead of running go
```

The default run is `go test -json -run <the registered tests>` over their packages, then a comparison. Slack's side is committed, so nothing on this path starts node or touches the network, and Go's test cache answers for packages you didn't change. Run it after each change and it tells you whether you drifted from Slack. It prints one line per dimension, and under a dimension that differs, each case that differs. This is CI's run of 2026-11-14 (ci run 43644570005), cut a little, from before branch emoji-v17 fixed the two cases it shows as known divergences; since then (ci run 43655459669) its first line reads `parity: 10 of 10 dimensions the same as Slack; 26 of 26 checks, 122 of 122 itemised cases.`

```
parity: 9 of 10 dimensions the same as Slack (1 with known divergences); 23 of 24 checks, 116 of 118 itemised cases.
  Only what these checks compare: 19 of the 28 areas of Slack's client have no check at all. This is not a parity percentage.
emoji-rank         DIVERGES (known)  2 of 3 checks same
    internal/acrank TestEmojiAutocompleteSlackOracle: diverges (known), 2 of 13 cases are known differences
      tu cold fallback model: known difference: Clack's standard emoji set has the 8 Unicode 17 emoji (ballet_dancer, ...
        row 25: Slack person_in_tuxedo, Clack hairy_creature
      tu with turtle picks, fallback model: known difference: the same reason
        row 25: Slack person_in_tuxedo, Clack hairy_creature
emoji-list         same              3 checks (2 partial), 9 cases
fuzzy-match        same              1 check, 8 cases
markdown           same              2 checks, 81 cases
link-slugs         same              2 checks (2 partial)
snippet-truncate   same              1 check
invite-tokens      same              3 checks
theme-hct          same              3 checks
window-bounds      same              3 checks
notifications      same              3 checks
slug-display       same              1 check, 57 cases  [fake server, not counted]
not covered: Composer: sending, editing, drafts and scheduling; Messages: rich text, Block Kit, attachments and unfurls; Threads; ...
different on purpose: The Windows toast's layout (Karim, 2026-11-12)
```

A check without subtests gives one result, so "3 checks" with no case count means three whole tests passed. That CI run took 66 seconds, building the eight packages with a cold cache; the next one (ci run 43645423249), with setup-go's cache warm, took 8.6 seconds. Go's test cache replays the results of packages whose inputs didn't change, so a rerun after editing one package rebuilds and reruns only that one, and naming dimensions narrows it further.

On swiss `go test` is refused, so use CI. The `parity` job in `ci.yml` runs the runner's own tests, then the default run. Its job summary is the full report, and the `parity-report` artifact has the markdown and JSON. The job is report-only: `continue-on-error`, and the runner exits 0 when Clack differs from Slack. `--strict` exits 1 instead, for when the team wants it to gate merges.

## What counts

A **check** is one Go test registered in `tools/parity_checks.json`. Its expected values must be Slack's output: `slack-js` (Slack's modules run in node, the ground-truth harnesses) or `slack-live` (an API answer or a message Slack's client sent). Values someone wrote down from reading Slack's code are rules, not Slack's output, so those tests don't go in the registry. About 250 tests carry "Slack" in their names; the report's "Not counted" section lists the unregistered ones.

A **dimension** is `same` only when every case of every check in it matched. Otherwise it is `DIVERGES` (a case gave something other than Slack's output), `DIVERGES (known)` (only recorded divergences, see below), `FAILS` (an assert beside the comparison failed, such as a setup step or a rule in the same test) or `NOT MEASURED` (no result: renamed test, build failure, filtered out). Only `same` counts as the same.

Checks marked `partial` compare part of Slack's output, for example an element's keys and their order. Fake-server checks (`tier: fixture`) compare Clack's test doubles with live Slack. They keep the tests that run against fakeslack honest, but they measure the fake, not Clack, so they stay out of the totals.

Slack's side is frozen. Each snapshot and registry entry names the Slack build and the date its values were captured, and a match means Clack does what that build did on those inputs. Slack may have moved since.

Coverage is listed, not summarised. The registry lists the 28 areas of Slack's client that Clack implements, each with its manual audit doc if `score.py` counts one (`audit`) and other notes (`docs`). The report names every area with no check, and for areas with checks it says what isn't compared there. It also lists the comparisons that run in other workflows and that this runner doesn't collect yet (`icon-shots`, `font-shots`, `compact-shots`, `activity-shots`, `threads-audit`, `emoji-picker`), and the differences Clack keeps on purpose.

## Snapshots and `--refresh`

A snapshot is `testdata/parity/<dimension>.json` next to the test:

```json
{
  "dimension": "emoji-rank",
  "slack": {"kind": "slack-js", "source": "...", "build": "web client efbd526e8fb3", "captured": "2026-11-14", "refresh": "emoji-rank"},
  "cases": [
    {"name": "tu cold", "in": {"q": "tu", "model": "remote"}, "slack": ["tulip", "root_vegetable", "..."]}
  ]
}
```

The Go test loads it with `internal/paritytest` and gives it Clack's output for each input:

```go
s := paritytest.Load[emojiRankIn, []string](t, "testdata/parity/emoji-rank.json")
s.Run(t, paritytest.Check[emojiRankIn, []string]{Clack: func(t *testing.T, in emojiRankIn) []string { ... }})
```

Each case runs as a subtest named after it. When Clack differs, the subtest fails and prints `slack:` and `clack:` as JSON, which the runner turns into "row 25: Slack person_in_tuxedo, Clack hairy_creature".

`--refresh DIMENSION` is the separate, slow step. It reruns the oracle wired up for the dimension (`REFRESHERS` in `tools/parity.py`), prints each case where Slack's output changed, and rewrites the snapshot with one case per line, so `git diff` shows what moved. It never runs Go. Then the default run tells you whether Clack still matches. To add a case, write its `name` and `in` with `"slack": null` and refresh; until then the case reports "no Slack output yet". Today only `emoji-rank` has an oracle wired up: `/root/clack-ops/scratch/emoji-rank/tools/oracle.cjs` on swiss (`CLACK_PARITY_EMOJI_ORACLE` overrides the path). It takes 3 seconds for 13 cases. The harnesses live outside the repo because they load Slack's own code from its CDN, which this repo doesn't redistribute. The markdown harness is gone from disk, nobody recorded where the fuzzy matcher's values came from, and the slug labels are live `chat.slugifyUrl` answers, so those three snapshots can't be refreshed until someone rebuilds or reruns their oracle.

The first refresh found a real divergence. The emoji-rank values had been printed by Slack's ranking code run on Clack's emoji set. Slack's own set, with `emoji_v17` off as in Karim's group, lacks the 8 Unicode 17 emoji Clack's `standard.json.gz` has. `hairy_creature` matches `:tu`, so with the fallback model it took row 25 where Slack shows `person_in_tuxedo`. Branch emoji-v17 (2026-11-14) made Clack's standard set follow `emoji_v17` the way Slack's does, so those cases match now and carry no mark.

A case's `in` can also set experiment groups over Karim's (`"experiments": {"emoji_v17": "on"}`): the refresher passes them to the harness as `--exp=NAME=GROUP`, and the Go test ranks Clack's set for those groups. The emoji-rank snapshot has `emoji_v17 on` twins of the `:tu` fallback cases and of `:orc`, where Slack's 17.0 set puts `hairy_creature` at row 25 and `orca` first.

## Known divergences

A case where Clack differs, and that isn't being fixed in the same change, gets `"known": "<why>"` and `"clack": <what Clack gives now>`. The test then skips while Clack gives exactly that output. It fails when Clack matches Slack (drop the mark) or gives anything else (the rest of the case is still checked). The runner reports the dimension as `DIVERGES (known)`, never as `same`. This is horizon's `known_flaky` list turned around: every entry says why, and it shows up loudly in every run until the cause is fixed.

Differences that are decisions, not bugs, go in the registry's `deliberate` list with who decided and when (today the Windows toast's layout, Karim on 2026-11-12, and the guard for a `slack://channel?thread_ts=<reply>` link, thread-open-root's review on 2026-11-18: Slack's own ThreadView draws such a link as a broken pane, the reply as the root with its whole thread under it, where Clack opens the reply's thread at the reply and logs that it did). No check measures them, and the report lists them on every run.

## Add a check

1. Write the test. For a new dimension, prefer a snapshot and `paritytest`. For an existing table, one `t.Run` per case is enough for the runner to count cases.
2. Register it in `tools/parity_checks.json` under a dimension, with `compares` (what of Slack's output it compares) and, without a snapshot, `oracle` (`kind`, `source`, `build`, `captured`, and `harness` if there is one). Add `"partial": true` if it compares only part of the output, and fill in the area's `gaps` if part of that area stays uncompared.
3. If the oracle can run again, add a function to `REFRESHERS`.
4. `python3 -I tools/parity_test.py` checks the registry: every test function exists and every snapshot is well formed. The CI job runs it first.

## How this differs from `docs/gaps/score.py`

`score.py` counts the manual audit docs' rows (PREFS, CHANNELS, FILES, VIEWS, SEARCH, MESSAGES, HUDDLES, COMPOSER, THREADS, EXPERIMENTS): people comparing Clack with Slack by eye and by reading Slack's code, a row at a time. `tools/parity.py` counts automated comparisons with Slack's own output, case by case, on every CI run. They answer different questions, and neither is a parity percentage for the whole client.

What came over from horizon: one runner over every differential harness, a per-dimension result, a list of known problems that each carries its reason and is reported loudly on every run, and a CI job that publishes the report. What didn't: horizon's oracle replay shim (committed snapshots do that job here), its dependency tracing and remote hosts (Go's test cache and CI cover those), and the percentage badge.
