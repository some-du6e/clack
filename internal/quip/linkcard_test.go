package quip

import (
	"bytes"
	"testing"
)

// TestLinkUnfurlCardDecodes: a link card section (type 56) holds a
// ContentLinkUnfurlCard in Content field 50: its url, its type
// (SLACK_UNFURL 3), the slack_unfurl_id of its unfurl container and
// is_unfurled are read, and the section encodes back byte for byte.
func TestLinkUnfurlCardDecodes(t *testing.T) {
	const (
		u  = "https://example.com/repo"
		id = "suo:T0FAKE0001/C0FAKE00000/1762500000.000100"
	)
	card := bytes.Join([][]byte{
		pbBytes(1, []byte(u)),
		pbUint(2, 3),
		pbBytes(3, pbBytes(3, pbBytes(1, []byte(id)))), // card_content.slack_unfurl.slack_unfurl_id
		pbUint(6, 1),
	}, nil)
	b := bytes.Join([][]byte{
		pbBytes(1, []byte("temp:C:LC1s1")),
		pbUint(9, uint64(TypeLinkUnfurlCard)),
		pbBytes(12, pbBytes(50, card)),
	}, nil)
	var s Section
	if err := Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	gotURL, gotType, gotID, gotUnfurled := cardFields(&s)
	if gotURL != u || gotType != 3 || gotID != id || !gotUnfurled {
		t.Errorf("card url %q type %d unfurl id %q unfurled %v, want %q 3 %q true", gotURL, gotType, gotID, gotUnfurled, u, id)
	}
	if out := Marshal(&s); !bytes.Equal(out, b) {
		t.Errorf("encoded %x, want %x", out, b)
	}
}
