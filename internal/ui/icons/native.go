package icons

// Glyphs Chrome itself paints for native form controls, which Slack's
// unstyled controls show: the checkbox tick of an <input type=checkbox>
// (Slack's Checkbox, client-boot QG0J, is a native c-input_checkbox with
// no appearance:none). Chromium's NativeThemeBase::PaintCheckbox
// (ui/native_theme/native_theme_base.cc) strokes it on the box's rect r:
// from (r.x+0.2w, r.centerY) by (0.2w, 0.2h) to (r.right-0.2w,
// r.y+0.2h), 0.16h wide with Skia's default butt caps and miter join. On
// the set's 20-unit square that is (4,10) to (8,14) to (16,4), 3.2 wide;
// chrome-check is that stroke's outline as a flw part 0.5 inside it.
// TestIconsMatchSlack holds it to the outline (inlineRef).
func init() {
	reg("chrome-check", flw(1, "M8.09 15.65 L16.55 5.08 L14.83 3.7 L7.91 12.35 L5.13 9.58 L3.58 11.13 Z"))
}
