package ui

// TestDemoSurfaces walks every surface of `clack demo` (internal/demo) the
// way a visitor would and fails on any request the demo workspace doesn't
// answer (fakeslack's Gaps: no hook or handler knew the method, or the
// answer was {"ok":false}), any image fetch the session refused, and any
// toast shown, so a new API call without a demo answer fails CI. With
// CLACK_DEMO_SHOTS set every surface is also saved, light and dark
// (demo-surface-<mode>-<step>.png), with the gap report beside them.
// Synthetic only: the demo runs fakeslack on loopback.

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/views"
)

// demoGapOK are gaps a real workspace answers the same way: a request
// Slack itself refuses here, with the error it gives. Each needs a reason.
var demoGapOK = map[string]string{}

// demoStep is one surface of the walk.
type demoStep struct {
	name string
	do   func(e *perfEnv, s *demo.Session)
	wait time.Duration // settle after do (default 1.2 s)
}

func demoSurfaceSteps(s *demo.Session) []demoStep {
	var steps []demoStep
	add := func(name string, do func(e *perfEnv, s *demo.Session)) {
		steps = append(steps, demoStep{name: name, do: do})
	}
	closeAll := func(e *perfEnv) {
		e.a.overlays = nil
		e.a.CloseSecondary()
	}
	// every conversation in the sidebar, and a thread in each that has one
	for _, c := range s.WS.Convs {
		c := c
		name := c.Name
		if name == "" {
			name = c.ID
		}
		add("conv-"+name, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.openChannel(c.ID)
		})
		for parent := range s.WS.Replies[c.ID] {
			parent := parent
			add("thread-"+name, func(e *perfEnv, s *demo.Session) {
				e.a.OpenThread(c.ID, parent)
			})
			break
		}
	}
	// the pages
	for _, p := range []string{"home", "unreads", "threads", "dms", "later", "saved", "drafts", "drafts?tab=scheduled", "files",
		"huddles", "directories?tab=people", "directories?tab=channels", "directories?tab=usergroups", "directories?tab=external",
		"search?q=paper", "search?q=from%3A%40gordon"} {
		p := p
		add("page-"+strings.NewReplacer("?", "-", "=", "-", "%3A%40", "-").Replace(p), func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.Navigate(p)
		})
	}
	// Activity, every tab
	for i, tab := range []string{"all", "dms", "mentions", "threads"} {
		i := i
		add("activity-"+tab, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.Navigate("activity")
			e.frame()
			if v, ok := e.a.views[Route("activity")].(*views.Activity); ok {
				v.ShowTab(i)
			}
		})
	}
	// channel details, every tab, and the Files tab
	for _, tab := range []string{"About", "Members", "Agents & apps", "Automations", "Tabs", "Settings"} {
		tab := tab
		add("details-general-"+strings.ReplaceAll(strings.ToLower(tab), " & ", "-"), func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.openChannel(demo.GeneralID)
			e.a.OpenDetailsTab(demo.GeneralID, tab)
		})
	}
	add("channel-files-tab", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.a.openChannelTab(demo.GeneralID, "files")
	})
	add("channel-workflows-tab", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.openChannel(demo.GeneralID)
		e.a.openWorkflowsTab(demo.GeneralID)
	})
	add("search-in-channel", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.openChannel(demo.GeneralID)
		e.a.OpenSearchInChannel(demo.GeneralID)
	})
	// people: every profile, a hover card for a person and the bots
	for _, p := range s.WS.People {
		p := p
		add("profile-"+p.Handle, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.OpenUserProfile(p.ID)
		})
	}
	for _, id := range []string{"U0DEMOGORD", demo.InkwellID, demo.PulseID} {
		id := id
		add("card-"+id, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.openChannel(demo.GeneralID)
			e.a.OpenProfileCard(image.Pt(500, 300), id)
		})
	}
	// canvases and lists
	for _, c := range s.WS.Canvases {
		c := c
		add("canvas-"+c.ID, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.OpenCanvas(c.ID)
		})
	}
	for _, l := range s.WS.Lists {
		l := l
		add("list-"+l.ID, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.OpenList(l.ID)
		})
	}
	// the overlays: emoji picker, quick switcher, search, rail peeks
	add("emoji-picker", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.openChannel(demo.GeneralID)
		e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
	})
	add("quick-switcher", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.a.OpenQuickSwitcher()
	})
	add("search-popover", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.a.openSearch("ream")
	})
	for _, k := range []string{"platform", "dms", "activity", "later", "files"} {
		k := k
		add("peek-"+k, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.openChannel(demo.GeneralID)
			e.frame()
			e.a.openTabPeek(k)
		})
	}
	add("rail-more", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.a.openRailMorePeek(true)
	})
	// Preferences, every section, and the plugins page
	for _, sec := range prefsNav {
		sec := sec
		add("prefs-"+sec.id, func(e *perfEnv, s *demo.Session) {
			closeAll(e)
			e.a.showPrefs(sec.id)
		})
	}
	add("prefs-plugins", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.a.showPrefs(pluginsTabID)
	})
	// huddles: an incoming ring
	add("huddle-ring", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.openChannel(demo.GeneralID)
		s.World.RingHuddle("gordon")
	})
	// the code channel: Inkwell's run
	add("code-channel-agent", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		e.openChannel(demo.CodeID)
		s.World.RunAgent(demo.CodeID, "", "can you add a test for 100 reams?")
	})
	add("poll", func(e *perfEnv, s *demo.Session) {
		closeAll(e)
		s.World.PostPoll(demo.LunchID)
		e.openChannel(demo.LunchID)
	})
	return steps
}

// demoGapReport is what one step left unanswered.
type demoGapReport struct {
	step    string
	gaps    []fakeslack.Gap
	refused []string
	toasts  []string
}

func TestDemoSurfaces(t *testing.T) {
	modes := []bool{false}
	if os.Getenv("CLACK_DEMO_SHOTS") != "" {
		modes = []bool{false, true}
	}
	for _, dark := range modes {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, s := demoEnv(t, demo.Options{Quiet: true, Speed: 40, Seed: 5})
			if dark {
				e.a.setDark(true)
			}
			e.until("the demo sidebar", 30*time.Second, func() bool { return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.CodeID) })
			e.settle(2 * time.Second) // boot's own requests
			var reports []demoGapReport
			gapAt, refAt := 0, 0
			collect := func(step string, toasts []string) {
				g, r := s.FS.Gaps(), s.Refused()
				rep := demoGapReport{step: step, gaps: g[gapAt:], refused: r[refAt:], toasts: toasts}
				gapAt, refAt = len(g), len(r)
				if len(rep.gaps)+len(rep.refused)+len(rep.toasts) > 0 {
					reports = append(reports, rep)
				}
			}
			collect("boot", nil)
			for _, st := range demoSurfaceSteps(s) {
				st.do(e, s)
				wait := st.wait
				if wait == 0 {
					wait = 1200 * time.Millisecond
				}
				var toasts []string
				for dl := time.Now().Add(wait); time.Now().Before(dl); {
					e.frame()
					if tx := e.a.toast.text; tx != "" && (len(toasts) == 0 || toasts[len(toasts)-1] != tx) {
						toasts = append(toasts, tx)
					}
					time.Sleep(2 * time.Millisecond)
				}
				s.World.WaitIdle(5 * time.Second)
				e.frame()
				if dir := os.Getenv("CLACK_DEMO_SHOTS"); dir != "" {
					e.shotTo(t, dir, "demo-surface-"+mode+"-"+st.name)
				}
				collect(st.name, toasts)
			}
			report := demoFormatGaps(reports)
			if dir := os.Getenv("CLACK_DEMO_SHOTS"); dir != "" {
				_ = os.WriteFile(filepath.Join(dir, "demo-gaps-"+mode+".txt"), []byte(report), 0o644)
			}
			bad := 0
			for _, rep := range reports {
				for _, g := range rep.gaps {
					if _, ok := demoGapOK[demoGapKey(g)]; !ok {
						bad++
					}
				}
				bad += len(rep.refused) + len(rep.toasts)
			}
			if bad > 0 {
				t.Errorf("%d request(s) the demo didn't answer, images it refused or toasts it showed:\n%s", bad, report)
			} else {
				t.Logf("every surface answered:\n%s", report)
			}
		})
	}
}

func demoGapKey(g fakeslack.Gap) string {
	k := g.Method + " " + string(g.Kind)
	if g.Error != "" {
		k += ":" + g.Error
	}
	return k
}

func demoFormatGaps(reports []demoGapReport) string {
	var b strings.Builder
	total := map[string]int{}
	for _, rep := range reports {
		fmt.Fprintf(&b, "== %s\n", rep.step)
		for _, g := range rep.gaps {
			k := demoGapKey(g)
			total[k]++
			if _, ok := demoGapOK[k]; ok {
				k += " (ok: " + demoGapOK[k] + ")"
			}
			var fs []string
			for f, v := range g.Form {
				fs = append(fs, f+"="+v)
			}
			sort.Strings(fs)
			fmt.Fprintf(&b, "  %s  %s\n", k, strings.Join(fs, " "))
		}
		for _, r := range rep.refused {
			total["refused image"]++
			fmt.Fprintf(&b, "  refused image %s\n", r)
		}
		for _, tx := range rep.toasts {
			total["toast"]++
			fmt.Fprintf(&b, "  toast %q\n", tx)
		}
	}
	keys := make([]string, 0, len(total))
	for k := range total {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("== totals\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  %4d  %s\n", total[k], k)
	}
	return b.String()
}
