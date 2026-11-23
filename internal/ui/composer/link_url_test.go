package composer

import (
	"encoding/json"
	"os"
	"testing"
)

// The link helpers against Slack's own code: testdata/link_url_oracle.json
// is what psdB truncateUrl, MqLx normalizeLinkUrl / isLinkUrlValid and
// +BPJ getTrimLength (build 83aff18a1847, run in node over the public CDN
// chunks by /root/clack-ops/scratch/composer-link-edit/harness) answer
// for each case: long-links' URLs and more, the Link field's shapes, and
// selections with punctuation and brackets at either end.
func TestLinkURLSlackOracle(t *testing.T) {
	b, err := os.ReadFile("testdata/link_url_oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		Truncate []struct {
			In, Out string
		}
		Normalize []struct {
			In, Out string
			Valid   bool
		}
		Trim []struct {
			In          string
			Lead, Trail int
		}
	}
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Truncate) < 90 || len(o.Normalize) < 30 || len(o.Trim) < 20 {
		t.Fatalf("oracle: %d truncate, %d normalize, %d trim cases", len(o.Truncate), len(o.Normalize), len(o.Trim))
	}
	for _, c := range o.Truncate {
		if got := truncateURL(c.In); got != c.Out {
			t.Errorf("truncateURL(%q)\n = %q\nwant %q", c.In, got, c.Out)
		}
	}
	for _, c := range o.Normalize {
		if got := normalizeLinkURL(c.In); got != c.Out {
			t.Errorf("normalizeLinkURL(%q) = %q, want %q", c.In, got, c.Out)
		}
		if got := isLinkURLValid(c.In); got != c.Valid {
			t.Errorf("isLinkURLValid(%q) = %v, want %v", c.In, got, c.Valid)
		}
	}
	for _, c := range o.Trim {
		rs := []rune(c.In)
		for i, r := range rs {
			if r == '\uE000' { // getContentsForRange's embedReplacement
				rs[i] = ObjRune
			}
		}
		if lead, trail := linkTrimLength(rs, false), linkTrimLength(rs, true); lead != c.Lead || trail != c.Trail {
			t.Errorf("linkTrimLength(%q) = %d, %d; want %d, %d", c.In, lead, trail, c.Lead, c.Trail)
		}
	}
}
