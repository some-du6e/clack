package kit

import "testing"

// Synthetic fixtures only.

// Slack's SvgIcon (SvgIconPrimitive) defaults size to "inherit" and sets
// --s only for another size, and its svg's rule (client-boot-styles
// [data-5jx]{width:1em;height:1em;font-size:var(--s,inherit)}) makes the
// icon its --s square or, with no size, 1em of the font it sits in: the
// 13px of a small Button's label (Activity's Mark Complete check), the
// 15px of the page. Clack drew every SvgIcon without a size at 20.
func TestSvgIconSizeInherits(t *testing.T) {
	js := `{"t":"div","k":1,"c":[` +
		`{"t":"div","k":2,"p":{"style":{"fontSize":"13px"}},"c":[` +
		`{"t":"@","n":"SvgIcon","k":3,"p":{"name":"check"}},` +
		`{"t":"@","n":"SvgIcon","k":4,"p":{"name":"check","size":"inherit"}},` +
		`{"t":"@","n":"SvgIcon","k":5,"p":{"name":"check","size":"16"}},` +
		`{"t":"@","n":"SvgIcon","k":6,"p":{"name":"check","size":20}}]},` +
		`{"t":"@","n":"SvgIcon","k":7,"p":{"name":"check"}},` +
		`{"t":"@","n":"Button","k":8,"p":{"type":"outline","size":"small"},"c":[` +
		`{"t":"@","n":"SvgIcon","k":9,"p":{"name":"check"}},{"t":"#","v":"Mark Complete"}]}]}`
	for _, dark := range []bool{false, true} {
		e := newOwlEnvTree(t, dark, js)
		for _, c := range []struct {
			k    int64
			want float32
			what string
		}{
			{3, 13, "no size in a 13px div: 1em"},
			{4, 13, `size "inherit" in a 13px div: 1em`},
			{5, 16, `size "16": --s 16px`},
			{6, 20, "size 20 (a number)"},
			{7, 15, "no size on the page: the body's 15px"},
			{9, 13, "no size in a small Button: its 13px label's em"},
		} {
			r := e.rect(c.k)
			if !owlNear(r[2], c.want) || !owlNear(r[3], c.want) {
				t.Errorf("dark %v: %s is %vx%v, want %vx%v", dark, c.what, r[2], r[3], c.want, c.want)
			}
		}
	}
}
