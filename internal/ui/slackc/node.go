package slackc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// Node is one host-tree node from the JS renderer (see docs/PLUGINS.md):
//
//	{"t":"div","k":12,"p":{"className":"a b","style":{…}},"on":{"click":12},"c":[…]}
//	{"t":"#","v":"text"}
//	{"t":"@","n":"MessageSender","k":13,"p":{…},"c":[…]}
//
// Trees are immutable once delivered: the JS side sends a whole new tree per
// commit, and per-node UI state (hover, editors, scroll) lives in the View,
// keyed by K.
type Node struct {
	T string // tag ("div", "span", "svg", …), "#" for text, "@" for a Slack component
	N string // "@": the Slack component name
	V string // "#": the text
	K int64  // stable host-instance id (0 for text)
	// Focus marks the node JS focused (el.focus(), autoFocus): the layout
	// engine moves key focus to it.
	Focus bool

	// P holds every prop as decoded JSON, minus className, id, style and
	// children. "@" props are decoded with DecodeProps ($fn/$gofn/$el).
	P     Props
	Class []string
	ID    string
	Style css.Decls // the inline style
	On    map[string]int64
	C     []*Node

	parent *Node
	index  int // among element siblings
	nsib   int // element sibling count
	tindex int // among same-tag siblings
	ntsib  int
}

// IsText reports a text node.
func (n *Node) IsText() bool { return n.T == "#" }

// IsComponent reports a Slack component ("@") node.
func (n *Node) IsComponent() bool { return n.T == "@" }

// Parent returns the parent node (nil at a root).
func (n *Node) ParentNode() *Node { return n.parent }

// Attr returns a prop as an attribute string (data-qa, aria-label, …).
func (n *Node) AttrString(name string) (string, bool) {
	switch name {
	case "class":
		return strings.Join(n.Class, " "), len(n.Class) > 0
	case "id":
		return n.ID, n.ID != ""
	}
	v, ok := n.P[name]
	if !ok {
		// React spells some attributes differently.
		switch name {
		case "for":
			v, ok = n.P["htmlFor"]
		case "tabindex":
			v, ok = n.P["tabIndex"]
		case "readonly":
			v, ok = n.P["readOnly"]
		}
		if !ok {
			return "", false
		}
	}
	switch v := v.(type) {
	case string:
		return v, true
	case bool:
		if !v {
			return "", false
		}
		return "", true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case nil:
		return "", false
	}
	return fmt.Sprint(v), true
}

// rawNode is the wire shape.
type rawNode struct {
	T  string                     `json:"t"`
	N  string                     `json:"n"`
	V  string                     `json:"v"`
	K  int64                      `json:"k"`
	F  bool                       `json:"focus"`
	P  map[string]json.RawMessage `json:"p"`
	On map[string]int64           `json:"on"`
	C  []json.RawMessage          `json:"c"`
}

// ParseTree decodes a delivered tree: one node or an array of top-level
// nodes. Safe on any goroutine.
func ParseTree(b []byte) ([]*Node, error) {
	b = trimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	if b[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(b, &raws); err != nil {
			return nil, err
		}
		return parseKids(nil, raws)
	}
	n, err := parseNode(nil, b)
	if err != nil || n == nil {
		return nil, err
	}
	return []*Node{n}, nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	return b
}

func parseKids(parent *Node, raws []json.RawMessage) ([]*Node, error) {
	out := make([]*Node, 0, len(raws))
	for _, r := range raws {
		r = trimSpace(r)
		if len(r) == 0 || string(r) == "null" {
			continue
		}
		if r[0] == '[' { // a fragment array: flatten
			var sub []json.RawMessage
			if err := json.Unmarshal(r, &sub); err != nil {
				return nil, err
			}
			kids, err := parseKids(parent, sub)
			if err != nil {
				return nil, err
			}
			out = append(out, kids...)
			continue
		}
		if r[0] == '"' { // a bare string child
			var s string
			if err := json.Unmarshal(r, &s); err != nil {
				return nil, err
			}
			out = append(out, &Node{T: "#", V: s, parent: parent})
			continue
		}
		n, err := parseNode(parent, r)
		if err != nil {
			return nil, err
		}
		if n != nil {
			out = append(out, n)
		}
	}
	indexKids(out)
	return out, nil
}

// indexKids fills sibling positions for structural pseudo-classes.
func indexKids(kids []*Node) {
	n := 0
	for _, k := range kids {
		if !k.IsText() {
			n++
		}
	}
	i := 0
	var counts map[string]int
	for _, k := range kids {
		if k.IsText() {
			continue
		}
		k.index, k.nsib = i, n
		i++
		if counts == nil {
			counts = map[string]int{}
		}
		k.tindex = counts[k.T]
		counts[k.T]++
	}
	for _, k := range kids {
		if !k.IsText() {
			k.ntsib = counts[k.T]
		}
	}
}

func parseNode(parent *Node, b []byte) (*Node, error) {
	var r rawNode
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	n := &Node{T: r.T, N: r.N, V: r.V, K: r.K, Focus: r.F, On: r.On, parent: parent}
	if n.T == "" {
		n.T = "div"
	}
	if n.T == "#" {
		return n, nil
	}
	if len(r.P) > 0 {
		n.P = make(Props, len(r.P))
		for k, raw := range r.P {
			switch k {
			case "children", "key", "ref":
				continue
			case "className", "class":
				var s string
				if json.Unmarshal(raw, &s) == nil {
					n.Class = strings.Fields(s)
				}
				if n.T != "@" { // components get className as a prop too
					continue
				}
			case "id":
				var s string
				if json.Unmarshal(raw, &s) == nil {
					n.ID = s
				}
				if n.T != "@" {
					continue
				}
			case "style":
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil {
					n.Style = css.FromReactStyle(m)
				} else {
					var s string
					if json.Unmarshal(raw, &s) == nil {
						n.Style = css.ParseDecls(s)
					}
				}
				if n.T != "@" {
					continue
				}
			}
			v, err := decodeValue(raw)
			if err != nil {
				return nil, fmt.Errorf("prop %s: %w", k, err)
			}
			n.P[k] = v
		}
	}
	if len(r.C) > 0 {
		kids, err := parseKids(n, r.C)
		if err != nil {
			return nil, err
		}
		n.C = kids
	}
	return n, nil
}
