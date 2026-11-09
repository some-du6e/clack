// Command report turns a plugin bench run (TestPluginBench's per-plugin
// JSON files) into the baked-in data file and docs/PLUGIN-BENCH.md, and
// fails when a committed winner regressed.
//
//	go run ./internal/pluginbench/report -in dist/bench -data internal/pluginbench/bench.json \
//	    -doc docs/PLUGIN-BENCH.md [-check] [-run <url>]
//
// With -check, the data and doc files are written next to themselves as
// *.new instead, and the exit status is 1 when Regressions finds any.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginbench"
)

func main() {
	in := flag.String("in", "", "directory of bench result files (*.json)")
	dataPath := flag.String("data", "internal/pluginbench/bench.json", "bench.json to write")
	docPath := flag.String("doc", "docs/PLUGIN-BENCH.md", "markdown table to write")
	check := flag.Bool("check", false, "compare with the committed data; exit 1 on a regression")
	run := flag.String("run", "", "URL of the run (recorded in the data)")
	flag.Parse()
	if *in == "" {
		flag.Usage()
		os.Exit(2)
	}
	cur, err := collect(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cur.Run = *run
	for i := range cur.Plugins {
		pluginbench.Decide(&cur.Plugins[i])
	}
	cur.Sort()

	var old pluginbench.Data
	if b, err := os.ReadFile(*dataPath); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	out, doc := *dataPath, *docPath
	if *check {
		out, doc = out+".new", doc+".new"
	}
	b, _ := json.MarshalIndent(cur, "", "  ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	md := markdown(cur)
	if err := os.WriteFile(doc, []byte(md), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if s := os.Getenv("GITHUB_STEP_SUMMARY"); s != "" {
		if f, err := os.OpenFile(s, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			_, _ = f.WriteString(summaryTable(cur))
			f.Close()
		}
	}
	fmt.Print(summaryTable(cur))
	if regs := pluginbench.Regressions(&old, cur); len(regs) > 0 {
		fmt.Println("\nRegressions against the committed bench data:")
		for _, r := range regs {
			fmt.Println("  - " + r)
		}
		if *check {
			os.Exit(1)
		}
	}
	for _, p := range cur.Plugins {
		if o := findPlugin(&old, p.Name); o != nil && o.Winner != p.Winner {
			fmt.Printf("note: %s winner changed %s -> %s (%s)\n", p.Name, o.Winner, p.Winner, p.Reason)
		}
	}
}

func findPlugin(d *pluginbench.Data, name string) *pluginbench.Plugin {
	for i := range d.Plugins {
		if d.Plugins[i].Name == name {
			return &d.Plugins[i]
		}
	}
	return nil
}

// collect reads every result file under dir (recursively: one artifact
// directory per shard).
func collect(dir string) (*pluginbench.Data, error) {
	d := &pluginbench.Data{Schema: pluginbench.Schema, Generated: time.Now().UTC().Format(time.RFC3339)}
	srcs := map[string]bool{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var r struct {
			pluginbench.Plugin
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if r.Name == "" || len(r.Variants) == 0 {
			return nil
		}
		for _, s := range r.Sources {
			srcs[s] = true
		}
		if findPlugin(d, r.Name) == nil {
			d.Plugins = append(d.Plugins, r.Plugin)
		}
		return nil
	})
	for s := range srcs {
		d.Sources = append(d.Sources, s)
	}
	sort.Strings(d.Sources)
	if err == nil && len(d.Plugins) == 0 {
		err = fmt.Errorf("no bench results under %s", dir)
	}
	return d, err
}

func sys(s string) string {
	if s == "slick" {
		return "Slick"
	}
	return "Taut"
}

func ms(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func cell(v *pluginbench.Variant) string {
	if v == nil {
		return "—"
	}
	if !v.Loaded {
		return "doesn't load"
	}
	s := fmt.Sprintf("%d/%d", v.Passed(), len(v.Checks))
	if v.Rerenders() {
		s += " **re-renders**"
	}
	return s
}

func summaryTable(d *pluginbench.Data) string {
	var b strings.Builder
	b.WriteString("| Plugin | Winner | Why | Checks (Taut / Slick) | Load ms (Taut / Slick) | Frame +µs (Taut / Slick) | Scroll CPU +ms (Taut / Slick) | JS heap KB (Taut / Slick) | Idle trees (Taut / Slick) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for i := range d.Plugins {
		p := &d.Plugins[i]
		t, s := p.Variant("taut"), p.Variant("slick")
		pair := func(f func(v *pluginbench.Variant) string) string {
			g := func(v *pluginbench.Variant) string {
				if v == nil || !v.Loaded {
					return "—"
				}
				return f(v)
			}
			return g(t) + " / " + g(s)
		}
		fmt.Fprintf(&b, "| %s | **%s** | %s | %s / %s | %s | %s | %s | %s | %s |\n", p.Name, sys(p.Winner), mdEscape(p.Reason), cell(t), cell(s),
			pair(func(v *pluginbench.Variant) string { return ms(v.InitMS) }),
			pair(func(v *pluginbench.Variant) string { return ms(v.FrameCost()) }),
			pair(func(v *pluginbench.Variant) string { return ms(v.CPUCost()) }),
			pair(func(v *pluginbench.Variant) string { return ms(v.JSHeapKB) }),
			pair(func(v *pluginbench.Variant) string { return fmt.Sprint(v.IdleDeliveries) }))
	}
	return b.String()
}

func mdEscape(s string) string { return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s) }

func markdown(d *pluginbench.Data) string {
	var b strings.Builder
	b.WriteString(`# Plugin bench: Taut or Slick?

<!-- Generated by ./internal/pluginbench/report from a plugin-bench run; don't edit by hand. -->

Many plugins in Clack's Library ship in both Taut and Slick. The bench runs
each variant, unmodified, in Clack's real plugin host (QuickJS on wazero,
slackc's React reconciler mapped to native widgets) over the whole client
signed in to internal/fakeslack, and the Library's Install button installs
the winner. In Preferences > Plugins > Browse, the chevron beside Install
opens "Install for" with both variants: the winner first, marked
"(recommended)", with the reason below it.

Every variant runs in a fresh client, against the same synthetic workspace
(` + "`fakeslack.Size.PluginBench`" + `: a 500-message #plugin-bench with
senders with and without pronouns, reactions with more reactors than an
avatar row shows, threads, a relay bot posting for a person, an app posting
with a person's token, the words Censorship masks by default; an
admins-only channel and a Slackbot DM). The bench installs the bundle the
Library publishes (same sha256) through the Library's own install path and
turns it on, then:

- **Checks** run the plugin's scenario and look at what the user would get:
  the host trees its patches delivered for visible rows, menus, the thread
  pane, the message views and member views plugins rewrite, composer send
  checks and transforms, thunks it patches. The same checks run against
  both variants.
- **Load** is install + enable until the plugin runs (the JS runtime and
  Slick's layer are already up, so this is the plugin's own start).
- **Frame +µs** is the median layout time per frame while scrolling the
  500-message channel top to bottom, minus the same pass before install.
  **Scroll CPU +ms** is the process CPU over that pass (layout plus the JS
  renders of rows scrolling in), minus the plugin-free pass.
- **JS heap** is QuickJS heap growth from installing the plugin.
- **Idle trees** are host trees delivered over an idle window (frames
  drawn, no input, no events) after everything settled. Ten or more, over
  two a second, means the variant keeps re-rendering, which disqualifies it.

The winner: a variant that loads beats one that doesn't; one that doesn't
re-render while idle beats one that does; more passing checks beat fewer;
no console errors beats some. Between variants that work the same, the
cheaper one wins on the costs above, each compared only past a noise
floor; a tie goes to the smaller bundle. The CI workflow
(` + "`.github/workflows/plugin-bench.yml`" + `) re-runs the bench and fails
when a committed winner regresses (stops loading, starts re-rendering,
passes fewer checks, or loses on correctness).

Neither upstream repository has per-plugin screenshots of the native
clients (Taut's and Slick's READMEs show the apps, not plugins), so
fidelity compares the two variants' screenshots of the same scene with
each other: **Pixel diff** is the share of the window that differs.
The screenshots are in the run's ` + "`plugin-bench-shots`" + ` artifact.

`)
	if len(d.Sources) > 0 {
		fmt.Fprintf(&b, "Bundles: %s. Generated %s", strings.Join(d.Sources, ", "), d.Generated)
	} else {
		fmt.Fprintf(&b, "Generated %s", d.Generated)
	}
	if d.Run != "" {
		fmt.Fprintf(&b, " by [this run](%s)", d.Run)
	}
	b.WriteString(".\n\n## Host fixes and gaps the bench found\n\n" + hostNotes + "\n## Results\n\n")
	b.WriteString(summaryTable(d))
	b.WriteString("\n## Per plugin\n")
	for i := range d.Plugins {
		p := &d.Plugins[i]
		fmt.Fprintf(&b, "\n### %s\n\n**%s**: %s.", p.Name, sys(p.Winner), p.Reason)
		if p.Notes != "" {
			b.WriteString(" " + p.Notes)
		}
		b.WriteString("\n\n| | Taut | Slick |\n|---|---|---|\n")
		t, s := p.Variant("taut"), p.Variant("slick")
		row := func(label string, f func(v *pluginbench.Variant) string) {
			g := func(v *pluginbench.Variant) string {
				if v == nil {
					return "—"
				}
				return mdEscape(f(v))
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", label, g(t), g(s))
		}
		row("Bundle", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("`%s` %.1f KB, sha256 `%s`", v.ID, float64(v.Size)/1024, short(v.SHA256))
		})
		row("Loads", func(v *pluginbench.Variant) string {
			if v.Loaded {
				return "yes"
			}
			return "no: " + v.StartError
		})
		for ci := range maxChecks(t, s) {
			label := ""
			if t != nil && ci < len(t.Checks) {
				label = t.Checks[ci].Name
			} else if s != nil && ci < len(s.Checks) {
				label = s.Checks[ci].Name
			}
			row(label, func(v *pluginbench.Variant) string {
				if ci >= len(v.Checks) {
					return "—"
				}
				c := v.Checks[ci]
				out := "fail"
				if c.Pass {
					out = "pass"
				}
				if c.Detail != "" {
					out += ": " + c.Detail
				}
				return out
			})
		}
		row("Console errors", func(v *pluginbench.Variant) string {
			if v.ErrorCount == 0 {
				return "none"
			}
			return fmt.Sprintf("%d: %s", v.ErrorCount, strings.Join(v.Errors, " · "))
		})
		row("Patches", func(v *pluginbench.Variant) string {
			if len(v.Patched) == 0 {
				return "—"
			}
			return strings.Join(v.Patched, ", ")
		})
		row("Host trees delivered", func(v *pluginbench.Variant) string { return fmt.Sprint(v.Roots) })
		row("Load", func(v *pluginbench.Variant) string { return ms(v.InitMS) + " ms" })
		row("Frame (median / p95)", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("%s / %s µs (no plugin: %s)", ms(v.FrameUS), ms(v.FrameP95US), ms(v.BaseFrameUS))
		})
		row("Scroll pass CPU", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("%s ms (no plugin: %s)", ms(v.ScrollCPUMS), ms(v.BaseCPUMS))
		})
		row("Allocations / frame", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("%.0f (no plugin: %.0f)", v.AllocsFrame, v.BaseAllocs)
		})
		row("JS heap / Go heap", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("+%s KB / +%s KB", ms(v.JSHeapKB), ms(v.GoHeapKB))
		})
		row("Idle", func(v *pluginbench.Variant) string {
			return fmt.Sprintf("%d trees in %.0f s, %d wakeups (no plugin: %d), CPU %s ms/s (no plugin: %s)",
				v.IdleDeliveries, v.IdleSeconds, v.IdleWakeups, v.BaseWakeups, ms(v.IdleCPUMS), ms(v.BaseIdleCPUMS))
		})
		row("Pixel diff", func(v *pluginbench.Variant) string {
			if v.Shot == "" {
				return "—"
			}
			return fmt.Sprintf("%.1f%% (`%s`)", v.PixelDiff*100, v.Shot)
		})
	}
	return b.String()
}

func maxChecks(t, s *pluginbench.Variant) int {
	n := 0
	for _, v := range []*pluginbench.Variant{t, s} {
		if v != nil && len(v.Checks) > n {
			n = len(v.Checks)
		}
	}
	return n
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// hostNotes are what the bench turned up in the host (not per plugin: the
// APIs Clack emulates), kept with the table so its numbers read right.
const hostNotes = `- **Fixed: rows re-rendering forever.** The Taut mirror fingerprints a
  channel's messages to tell JS when they changed; it hashed the cached
  threads in Go's random map order, so a channel with two or more cached
  threads looked changed on every 750 ms sweep. The mirror re-sent
  messages, channelHistory and reactions to JS each time, and every row a
  messages-reading plugin patched re-rendered: ShowRealUser and
  ShowSendingBot in both systems and Slick's Censorship "spazzed" while
  nothing happened. Threads are hashed in ts order now
  (internal/taut/mirror.go, TestMessagesFingerprintStable).
- **Fixed: Taut StreamerMode's MessageBackground errors.** It logged
  "componentElement MessageBackground: TypeError: not a function" and a
  React #31 (jsrt's __clack object rendered as a child) on some rows. Not
  the row props: jsrt released a handle twice. Value.Free released it,
  and the GC cleanup released the same number again later, by when the
  engine had handed it to another value (handles are reused, last freed
  first). Every promise Go settles (a plugin's API calls, thunks) freed its
  settle functions that way, so componentElement's function, props or
  element could turn into whatever came next. Free cancels the cleanup now
  (internal/jsrt/value.go, TestFreedValueNotReleasedTwice,
  TestPluginGapStreamerModeRows).
- **Fixed (fixture): pronouns in the thread pane header.** Clack's pane
  hands ThreadSenderAndTimestampGeneric what Slack's does (msg, adjacent,
  visible, omitTimestamp, omitLinebreak, keepSenderAccessible,
  timestampLabel). The bench saw nothing because fakeslack answered the
  bench threads' conversations.replies with no messages (a generic case
  ran before the bench's), so the pane never drew a root
  (TestPluginGapPronounsThread).
- **Fixed: message_mention in msg.** The msg prop carried blocks rebuilt
  from Clack's parsed text, where a message_mention is only its label
  ("alice in #idle"). It carries the message's blocks as Slack sent them
  now, as Slack's client does; a plugin's changed blocks are parsed like
  the message, so the pill stays (TestPluginMessageMentionBlocks).
- **Fixed: clicks on native links.** Clack draws message links natively,
  so Slick's Quick Join, which listens for clicks on the document and
  looks for a[data-channel-id], never saw one. While anything listens for
  clicks on the window or document, a click on a native link (URL,
  mention, channel, user group) is dispatched at a stand-in element with
  Slack's tag and attributes, kept per link outside <body>. The link's
  action is that element's activation behavior: it runs when the click
  isn't canceled, including a click a plugin replays later
  (internal/ui/slackc/links.go, TestPluginGapQuickJoinDoubleClick).
- **Not a host gap: Click to Load.** Clack draws link unfurls natively and
  never loads third-party frames, so there is nothing for either variant
  to hold.
`
