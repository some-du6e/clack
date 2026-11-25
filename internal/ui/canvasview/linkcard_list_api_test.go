package canvasview

import "github.com/some-du6e/clack/internal/lists"

// listShown is the first card's list entity as drawn: its labels and the
// first column of each record row (an item's: its fields' column names).
func listShown(v *View) (primary, second string, cells []string) {
	b := embedBlk(v)
	if b == nil {
		return "", "", nil
	}
	d := &b.card.ls
	for _, r := range d.rows {
		if len(d.cols) > 0 {
			col := d.cols[0]
			if d.todo && len(d.cols) > 1 {
				col = d.cols[1]
			}
			if f := r.Rec.Field(col.Key); f != nil {
				cells = append(cells, lists.Flatten(f).S)
			}
		}
	}
	for _, f := range d.fields {
		cells = append(cells, f.c.Name)
	}
	return d.primary, d.second, cells
}

// listCode is the first card's list refusal ("" with none).
func listCode(v *View) string {
	if b := embedBlk(v); b != nil {
		return b.card.ls.code
	}
	return ""
}

// canvasShownID is the canvas the first card draws ("" with none).
func canvasShownID(v *View) string {
	if b := embedBlk(v); b != nil && b.card.fc.known {
		return b.card.fc.f.ID
	}
	return ""
}
