package slackc

import (
	"log"
	"os"
	"sort"
	"strings"
)

// treeLog names the components whose delivered host trees are logged
// (CLACK_TAUT_TREELOG=Reaction,UnstyledButton or "*"): a debugging aid for
// plugin patches. Only the shape is logged (tags, classes, component names,
// prop keys), never text or prop values, so no message contents reach the log.
var treeLog = func() map[string]bool {
	v := os.Getenv("CLACK_TAUT_TREELOG")
	if v == "" {
		return nil
	}
	m := map[string]bool{}
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			m[n] = true
		}
	}
	return m
}()

func logTree(name string, root uint64, nodes []*Node) {
	if treeLog == nil || !(treeLog[name] || treeLog["*"]) {
		return
	}
	log.Printf("slackc tree %s #%d: %s", name, root, TreeShape(nodes))
}

// TreeShape is a one-line outline of a host tree without any text or prop
// values: `span.a.b>(@Emoji{emoji,size} #)`.
func TreeShape(nodes []*Node) string {
	var b strings.Builder
	var walk func([]*Node)
	walk = func(ns []*Node) {
		for i, n := range ns {
			if i > 0 {
				b.WriteByte(' ')
			}
			switch n.T {
			case "#":
				b.WriteByte('#')
				continue
			case "@":
				b.WriteString("@" + n.N)
			default:
				b.WriteString(n.T)
			}
			for _, c := range n.Class {
				b.WriteString("." + c)
			}
			if len(n.P) > 0 {
				keys := make([]string, 0, len(n.P))
				for k := range n.P {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				b.WriteString("{" + strings.Join(keys, ",") + "}")
			}
			if len(n.C) > 0 {
				b.WriteString(">(")
				walk(n.C)
				b.WriteByte(')')
			}
		}
	}
	walk(nodes)
	return b.String()
}
