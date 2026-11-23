package quip

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func pbVarint(n uint64) []byte {
	var w writer
	w.varint(n)
	return w.b
}

func pbKey(num, wt int) []byte { return pbVarint(uint64(num)<<3 | uint64(wt)) }

func pbBytes(num int, b []byte) []byte {
	return append(append(pbKey(num, 2), pbVarint(uint64(len(b)))...), b...)
}

func pbUint(num int, n uint64) []byte { return append(pbKey(num, 0), pbVarint(n)...) }

func TestQuipUnmarshalRefusesMalformed(t *testing.T) {
	deepGroup := func(n int) []byte {
		var b []byte
		for i := 0; i < n; i++ {
			b = append(b, pbKey(900, 3)...)
		}
		for i := 0; i < n; i++ {
			b = append(b, pbKey(900, 4)...)
		}
		return b
	}
	for name, b := range map[string][]byte{
		"truncated varint":     {0x10, 0x80},
		"length past the end":  append(pbKey(1, 2), 0x64, 'a'),
		"varint of 11 bytes":   append(pbKey(2, 0), 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01),
		"tenth byte too big":   append(pbKey(2, 0), 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02),
		"string as a varint":   pbUint(1, 5),
		"message as a varint":  pbUint(12, 1),
		"group end alone":      pbKey(5, 4),
		"group ends wrong":     append(pbKey(900, 3), pbKey(901, 4)...),
		"group never ends":     pbKey(900, 3),
		"groups too deep":      deepGroup(40),
		"wire type 6":          pbKey(900, 6),
		"fixed32 cut short":    append(pbKey(900, 5), 1, 2),
		"packed number cut":    append(pbKey(2, 2), 0x02, 0x80),
		"repeated number cut":  append(pbKey(4, 2), 0x01, 0x80),
		"repeated as fixed64":  append(pbKey(4, 1), make([]byte, 8)...),
		"nested message short": pbBytes(12, pbBytes(1, []byte{0x0a, 0x05, 'a'})),
	} {
		t.Run(name, func(t *testing.T) {
			var s Section
			if err := Unmarshal(b, &s); !errors.Is(err, ErrMalformed) {
				t.Errorf("Unmarshal(%x) = %v, want ErrMalformed", b, err)
			}
		})
	}
	var s Section
	if err := Unmarshal(make([]byte, MaxMessage+1), &s); !errors.Is(err, ErrMalformed) {
		t.Errorf("an answer over MaxMessage: %v", err)
	}
	// nesting: RichTextAPI > section > block > style is fine, but a
	// message 40 deep (here: unknown groups inside a kept record's
	// message) is refused
	if err := Unmarshal(deepGroup(maxDepth-1), &s); err != nil {
		t.Errorf("groups %d deep: %v", maxDepth-1, err)
	}
}

func TestQuipUnmarshalKeepsSlacksFieldsOnly(t *testing.T) {
	// attributes: 2 checked (named), 5 cell_formatting_options (Slack's,
	// not named), 36 (not in Slack's schema), 1 indentation (named)
	b := pbBytes(16, bytes.Join([][]byte{pbUint(5, 3), pbUint(2, 1), pbUint(36, 7), pbUint(1, 2)}, nil))
	var s Section
	if err := Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	a := s.Attributes
	if a == nil || a.Checked == nil || !*a.Checked || a.Indentation == nil || *a.Indentation != 2 {
		t.Fatalf("attributes %+v", a)
	}
	if want := pbUint(5, 3); !bytes.Equal(a.Other, want) {
		t.Errorf("kept %x, want only field 5 (%x)", a.Other, want)
	}
	// written back in field order: 1, 2, then 5
	want := pbBytes(16, bytes.Join([][]byte{pbUint(1, 2), pbUint(2, 1), pbUint(5, 3)}, nil))
	if got := Marshal(&s); !bytes.Equal(got, want) {
		t.Errorf("Marshal = %x, want %x", got, want)
	}
	// Section names every field it reads; Unknown lists the others
	var u Section
	if err := Unmarshal(append(pbUint(999, 1), pbUint(300, 2)...), &u); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(u.Unknown, []int32{999, 300}) || !bytes.Equal(u.Other, pbUint(300, 2)) {
		t.Errorf("unknown %v, kept %x", u.Unknown, u.Other)
	}
}

func TestQuipMarshalWritesSetZeros(t *testing.T) {
	s := Section{ID: Ptr(""), Sequence: Ptr(int64(0)), Deleted: Ptr(false), Content: &Content{}, Parents: &Parents{}}
	want := "0a00" + "1000" + "1800" + "6200" + "6a00"
	if got := hex.EncodeToString(Marshal(&s)); got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
	if got := Marshal(&Section{}); len(got) != 0 {
		t.Errorf("an empty section = %x", got)
	}
	if got := FieldsString(&s); got != "1,2,3,12,13" {
		t.Errorf("FieldsString = %q", got)
	}
}

func TestQuipBase64(t *testing.T) {
	r := &LoadRequest{ObjectIDs: []string{"ABCdEfGhIjK"}, BulkLoads: []BulkLoad{{Type: Ptr(BulkThread), Arg0: Ptr("ABCdEfGhIjK")}}}
	// the same request's bytes from Slack's encoder (quip-wire-encode.json, "load request")
	const want = "CgtBQkNkRWZHaElqSxoPCAESC0FCQ2RFZkdoSWpL"
	if got := MarshalBase64(r); got != want {
		t.Errorf("MarshalBase64 = %s, want %s", got, want)
	}
	var back LoadRequest
	if err := UnmarshalBase64(want, &back); err != nil || !reflect.DeepEqual(&back, r) {
		t.Errorf("UnmarshalBase64 = %+v, %v", back, err)
	}
	if err := UnmarshalBase64("not base64!", &back); !errors.Is(err, ErrMalformed) {
		t.Errorf("bad base64: %v", err)
	}
}

func TestQuipValidUTF8(t *testing.T) {
	ok := &DocumentData{Sections: []Section{{ID: Ptr("a"), Content: &Content{Text: &ContentText{Text: Ptr("café")}}}}}
	if !ValidUTF8(ok) {
		t.Error("valid text refused")
	}
	bad := &DocumentData{Sections: []Section{{Content: &Content{Text: &ContentText{Text: Ptr("a\xffb")}}}}}
	if ValidUTF8(bad) {
		t.Error("invalid UTF-8 in a nested string passed")
	}
	if !ValidUTF8(&Section{Status: Raw{0xff}}) {
		t.Error("bytes are not text")
	}
}

func TestQuipSectionIDs(t *testing.T) {
	re := regexp.MustCompile(`^temp:C:XyZ[0-9a-f]{25}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewSectionID("XyZ")
		if !re.MatchString(id) || !IsNewSectionID(id, "XyZ") || seen[id] {
			t.Fatalf("NewSectionID = %q (seen %v)", id, seen[id])
		}
		seen[id] = true
	}
	const hex25 = "0123456789abcdef012345678"
	if !IsNewSectionID("temp:C:XyZ"+hex25, "XyZ") {
		t.Errorf("IsNewSectionID refused temp:C:XyZ%s", hex25)
	}
	for _, id := range []string{"temp:C:XyZ" + hex25[:24], "temp:C:XyZ" + hex25 + "9", "temp:C:AbC" + hex25,
		"temp:C:XyZ" + strings.ToUpper(hex25), "temp:XyZ" + hex25, "XyZ" + hex25} {
		if IsNewSectionID(id, "XyZ") {
			t.Errorf("IsNewSectionID(%q)", id)
		}
	}
	if s := SessionID(); !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(s) {
		t.Errorf("SessionID = %q", s)
	}
	if got := ShardChars(&Document{ID: Ptr("ABCdef"), ShardChars: Ptr("XyZ")}); got != "XyZ" {
		t.Errorf("ShardChars with shard_chars = %q", got)
	}
	if got := ShardChars(&Document{ID: Ptr("ABCdef")}); got != "ABC" {
		t.Errorf("ShardChars from the id = %q", got)
	}
	for in, want := range map[[2]string]string{
		{"user", "U0123"}: "su:U0123", {"user", "su:U0123"}: "su:U0123", {"user", "S0123"}: "su:S0123",
		{"channel", "C0123"}: "sc:C0123", {"file", "F0123"}: "sf:F0123",
	} {
		var got string
		switch in[0] {
		case "user":
			got = UserID(in[1])
		case "channel":
			got = ChannelID(in[1])
		case "file":
			got = FileID(in[1])
		}
		if got != want {
			t.Errorf("%s id of %q = %q, want %q", in[0], in[1], got, want)
		}
	}
	if got := EmojiID("smile", "E0123"); got != "se:smile/E0123" {
		t.Errorf("EmojiID = %q", got)
	}
	if got := SlackID("sc:C0123"); got != "C0123" {
		t.Errorf("SlackID = %q", got)
	}
}

func TestQuipOrderHelpers(t *testing.T) {
	if StripTemp("aab:temp") != "aab" || StripTemp("aab") != "aab" || StripTemp("a:tempb") != "a" {
		t.Error("StripTemp")
	}
	if !IsTemp("aab:temp") || IsTemp("aab") || IsTemp("aab:tempx") {
		t.Error("IsTemp")
	}
	if got := PositionPath("aaa", "aab:temp"); got != "aaa-aab:temp" {
		t.Errorf("PositionPath = %q", got)
	}
}

func TestQuipCompactRefusesBadMarks(t *testing.T) {
	for _, m := range []Mark{
		{Name: "b", Flag: true, Start: -1, End: 2},
		{Name: "b", Flag: true, Start: 2, End: 1},
		{Name: "b", Flag: true, Start: 0, End: 9},
		{Name: "b", Flag: true, Start: 2, End: 3}, // inside é
	} {
		if out, ok := CompactRTML("aé", []Mark{m}); ok {
			t.Errorf("CompactRTML took %+v: %q", m, out)
		}
	}
	if out, ok := CompactRTML("aé", []Mark{{Name: "b", Flag: true, Start: 1, End: 3}}[:0]); !ok || out != "aé" {
		t.Errorf("no marks: %q %v", out, ok)
	}
	if out, ok := CompactRTML("aéb", []Mark{{Name: "b", Flag: true, Start: 1, End: 3}}); !ok || out != "a<b>é</b>b" {
		t.Errorf("a mark on é: %q %v", out, ok)
	}
	if !strings.Contains(EscapeText("x y", false), " ") {
		t.Error("EscapeText dropped U+2028")
	}
}
