package richtext

import "testing"

// Synthetic fixtures only.

func TestTableBlock(t *testing.T) {
	d := FromMessage(R, msg(t, `{"ts":"1.0","text":"fallback","blocks":[{"type":"table",
		"column_settings":[{"is_wrapped":true},{"align":"right"}],
		"rows":[
			[{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Score"}],
			[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"ann","style":{"bold":true}},{"type":"text","text":" "},{"type":"user","user_id":"U1"}]}]},
			 {"type":"raw_text","text":"42"}],
			[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"line1"}]},{"type":"rich_text_list","style":"bullet","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"item"}]}]}]},
			 {"type":"raw_text","text":""}]
		]}]}`))
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != Table {
		t.Fatalf("blocks: %+v", d.Blocks)
	}
	tb := d.Blocks[0]
	if len(tb.Rows) != 3 || len(tb.Rows[0]) != 2 || SpansText(tb.Rows[0][1]) != "Score" {
		t.Fatalf("rows: %+v", tb.Rows)
	}
	if len(tb.ColAlign) != 2 || tb.ColAlign[1] != "right" || !tb.ColWrap[0] {
		t.Errorf("columns: %v %v", tb.ColAlign, tb.ColWrap)
	}
	cell := tb.Rows[1][0]
	if cell[0].Style&Bold == 0 || cell[0].Text != "ann" {
		t.Errorf("styled cell: %+v", cell)
	}
	hasUser := false
	for _, s := range cell {
		hasUser = hasUser || s.Kind == User && s.ID == "U1"
	}
	if !hasUser {
		t.Errorf("user mention lost in cell: %+v", cell)
	}
	if got := SpansText(tb.Rows[2][0]); got != "line1\n• item" {
		t.Errorf("multi-block cell = %q", got)
	}
	if tb.Rows[2][1] != nil {
		t.Errorf("empty cell = %+v", tb.Rows[2][1])
	}
	if got := PlainText(d); got != "Name\tScore\nann @alice\t42\nline1 • item" {
		t.Errorf("plain = %q", got)
	}
}

func TestPDFThumb(t *testing.T) {
	d := FromMessage(R, msg(t, `{"ts":"1.0","files":[{"id":"F1","name":"doc.pdf","mimetype":"application/pdf","filetype":"pdf",
		"url_private":"https://files.slack.com/files-pri/T1-F1/doc.pdf",
		"thumb_pdf":"https://files.slack.com/files-tmb/T1-F1/doc_thumb_pdf.png","thumb_pdf_w":909,"thumb_pdf_h":1286}]}`))
	var f *File
	for _, b := range d.Blocks {
		if b.File != nil {
			f = b.File
		}
	}
	if f == nil || f.Thumb == nil || f.Thumb.URL != "https://files.slack.com/files-tmb/T1-F1/doc_thumb_pdf.png" || f.Thumb.W != 909 || f.Thumb.H != 1286 || f.IsImage {
		t.Errorf("pdf thumb: %+v", f)
	}
}
