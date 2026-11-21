package icons

// A rich_text check list's checkboxes (boot-shared J825 RichTextList,
// the 16px p-rich_text_list__check_icon SvgIcon): Slack's checkbox-filled
// (a filled square, its check a hole: the subpath winds the other way, so
// it stays empty under Gio's non-zero rule as under Slack's even-odd) and
// square, drawn on its 1.5-unit outline's centreline.
func init() {
	reg("checkbox-filled", f("M1.5 4.5a3 3 0 0 1 3-3h11a3 3 0 0 1 3 3v11a3 3 0 0 1-3 3h-11a3 3 0 0 1-3-3zm12.33 3.225a.75.75 0 0 0-1.16-.95l-3.885 4.748-1.428-1.964a.75.75 0 0 0-1.214.882l2 2.75a.75.75 0 0 0 1.187.034z"))
	reg("square", s(rrect(2.25, 2.25, 15.5, 15.5, 3)))
}
