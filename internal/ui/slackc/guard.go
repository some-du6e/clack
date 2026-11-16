package slackc

import (
	"sort"
	"strconv"
	"strings"

	"gioui.org/layout"
)

// For widgets that must not vanish under a plugin (the composer of a
// conversation you can post in): when the plugin's tree left the widget
// out, it draws itself without plugins (Unpatched), says what the trees
// held (KeyedTrees) and has them mounted again (RemountKeyed).
//
// A plugin's tree can come back empty: every patched component is a React
// root of its own, and React unmounts a root on an error nothing catches,
// so one throw in a patch left the root's host tree "[]" until its props
// changed (internal/taut's PatchGuard now catches them), and Go drew
// nothing for the component.

// unpatched counts the Unpatched calls in progress (UI goroutine).
var unpatched int

// Unpatched runs draw with every Slack component boundary drawn by its
// native, as though no plugin patched anything (no plugin trees, no
// reserved slots), and returns what draw returned. UI goroutine.
func Unpatched(draw func() layout.Dimensions) layout.Dimensions {
	unpatched++
	defer func() { unpatched-- }()
	return draw()
}

// RemountKeyed unmounts the plugin roots of every instance whose props'
// "$key" is key (a composer's boundaries all carry its key), so the next
// draw mounts fresh ones that the plugins render again from the start. It
// returns how many it unmounted. UI goroutine.
func RemountKeyed(key string) int {
	if key == "" {
		return 0
	}
	b := bridge()
	n := 0
	for k, in := range insts {
		if k.key == key {
			delete(insts, k)
			dropRoot(in, b)
			n++
		}
	}
	return n
}

// KeyedTrees describes, for a log line, the plugin trees of the instances
// keyed key, sorted by component: "MessagePaneInput: an empty tree;
// TypingNames: 3 nodes". "" when there are none. UI goroutine.
func KeyedTrees(key string) string {
	if key == "" {
		return ""
	}
	var out []string
	for k, in := range insts {
		if k.key == key {
			out = append(out, k.name+": "+treeState(in))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

// KeyedPanes are the panes (Pane) of component name's instances keyed
// key, sorted: one per list that draws the row, for tests. UI goroutine.
func KeyedPanes(name, key string) []string {
	var out []string
	for k := range insts {
		if k.name == name && k.key == key {
			out = append(out, k.pane)
		}
	}
	sort.Strings(out)
	return out
}

// treeState is what an instance draws from: no tree yet (its native draws),
// an empty tree (nothing draws), or n nodes with or without the "@" node
// for its own native (<Original>).
func treeState(in *inst) string {
	if in.sub != nil {
		return "a sub tree"
	}
	p := in.tree.Load()
	switch {
	case p == nil:
		return "no tree yet"
	case len(*p) == 0:
		return "an empty tree"
	}
	n, orig := 0, false
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, x := range ns {
			n++
			if x.T == "@" && x.N == in.name {
				orig = true
			}
			walk(x.C)
		}
	}
	walk(*p)
	s := strconv.Itoa(n) + " nodes"
	if !orig {
		s += " without its <Original>"
	}
	return s
}
