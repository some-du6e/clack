package listview

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/lists"
)

// LinkColumn shows a cell's first link (getTypedFieldValue: link[0]): its
// URL when it displays as one, else its name, else its unfurl's title.
func TestLinkCellText(t *testing.T) {
	unfurl := json.RawMessage(`{"https://a.example/doc":{"title":"The doc","fallback":"Doc"}}`)
	for _, c := range []struct {
		l    lists.LinkValue
		want string
	}{
		{lists.LinkValue{OriginalURL: "https://a.example/doc"}, "https://a.example/doc"},
		{lists.LinkValue{OriginalURL: "https://a.example/doc", DisplayName: "Spec"}, "Spec"},
		{lists.LinkValue{OriginalURL: "https://a.example/doc", DisplayName: "Spec", DisplayAsURL: true}, "https://a.example/doc"},
		{lists.LinkValue{OriginalURL: "https://a.example/doc", Attachment: unfurl}, "The doc"},
	} {
		if got := linkText(c.l); got != c.want {
			t.Errorf("%+v: %q, want %q", c.l, got, c.want)
		}
	}
	col := &lists.Column{ID: "ColL", Key: "ColL", Type: lists.Link}
	f := &lists.Field{Key: "ColL", Link: []lists.LinkValue{{OriginalURL: "https://a.example", DisplayName: "First"}, {OriginalURL: "https://b.example"}}}
	if got := CellText(nil, col, f); got != "First" {
		t.Errorf("two links: %q", got)
	}
}
