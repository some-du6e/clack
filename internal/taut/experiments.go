package taut

import (
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/core"
)

// Slack's experiments, for plugins (docs/EXPERIMENTS.md). The mirror's
// `experiments` slice is experiments.getByUser's records, as Slack's store
// holds them. Plugins change what Slack's code reads by patching that
// slice (Slick's Experiments, redux.patchSlice) or through api.experiments
// (Taut's Experiments, BringBackSlackbot), which patches it too; after
// every patch change the runtime compares the patched slice with the raw
// one and sends Go the groups that differ (03_redux.js
// tellGoExperiments), and Go forces them in core
// (core.SetExperimentGroups), so every one of Clack's experiment checks
// reads them at once. No plugin is special-cased.

type pluginExperiments struct {
	mu     sync.Mutex
	forced map[string]*string // what the plugins' patches force: group, nil = not assigned
}

// setPluginExperiments takes the plugins' forced groups (all of them, each
// time) and forces in core what changed since the last call; groups no
// longer forced go back to experiments.getByUser's.
func (t *Taut) setPluginExperiments(forced map[string]*string) {
	t.exp.mu.Lock()
	prev := t.exp.forced
	t.exp.forced = forced
	t.exp.mu.Unlock()
	change := map[string]*string{}
	for name := range prev {
		if _, ok := forced[name]; !ok {
			change[name] = nil
		}
	}
	for name, g := range forced {
		if p, ok := prev[name]; ok && sameGroup(p, g) {
			continue
		}
		v := "" // null: reads as not assigned
		if g != nil {
			v = *g
		}
		change[name] = &v
	}
	if len(change) > 0 && t.app != nil {
		t.app.SetExperimentGroups(change)
	}
}

func sameGroup(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// experimentReadsSource is the source the runtime registers as the
// module "clack-experiment-reads": Clack's own experiment checks, written
// the way Slack's modules read experiments (`"on"===(0,x.getGroupForUser)(e,
// "name")` with x the experiments selectors module), so a plugin that
// scans module sources for experiment checks (both Experiments plugins
// do) finds which experiments Clack reads and the groups it compares.
func experimentReadsSource() string {
	var b strings.Builder
	b.WriteString(`function(e,t,n){"use strict";var x=n("experiments");/* Clack's experiment checks (internal/core/experiments.go) */[`)
	for i, r := range core.ExperimentReads() {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("s=>")
		for j, g := range r.Groups {
			if j > 0 {
				b.WriteString("||")
			}
			b.WriteString(strconv.Quote(g) + `===(0,x.getGroupForUser)(s,` + strconv.Quote(r.Name) + `)`)
		}
	}
	b.WriteString("]}")
	return b.String()
}
