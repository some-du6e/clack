package quip

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func textSection(id string, seq int64, pos, rtml string) Section {
	return Section{ID: Ptr(id), Sequence: Ptr(seq), Position: Ptr(pos), PositionPath: Ptr(pos), Type: Ptr(TypeText),
		Content: &Content{Text: &ContentText{Text: Ptr(rtml)}}}
}

func loadAnswer(secs ...Section) *LoadWrapper {
	return &LoadWrapper{Response: &LoadResponse{Success: Ptr(true), Objects: &Payload{
		Threads:   []Thread{{ID: Ptr("TH"), Sequence: Ptr(int64(5000)), DocumentID: Ptr("DOCx")}},
		Documents: []Document{{ID: Ptr("DOCx"), Sequence: Ptr(int64(5000)), ThreadID: Ptr("TH"), MaxSectionSequence: Ptr(int64(5000))}},
		Sections:  secs,
	}, Indexes: []Index{{Name: Ptr("Section/DOCx"), Entries: []IndexEntry{{ObjectID: Ptr("b"), SortValue: Ptr("aab")}, {ObjectID: Ptr("a"), SortValue: Ptr("aaa")}}}}}}
}

func ids(secs []*Section) string {
	var out []string
	for _, s := range secs {
		out = append(out, *s.ID)
	}
	return strings.Join(out, ",")
}

func TestQuipReadLoad(t *testing.T) {
	d, err := ReadLoad("TH", loadAnswer(textSection("b", 2000, "zzz", "two"), textSection("a", 1000, "zzy", "one")))
	if err != nil {
		t.Fatal(err)
	}
	// the index orders them, not their own positions
	if got := ids(d.Sections()); got != "a,b" {
		t.Errorf("order %s", got)
	}
	r := d.FetchRequest()
	want := &FetchRequest{Requests: []FetchThread{{ThreadID: Ptr("TH"), ClientThreadSequence: Ptr(int64(5000)), ClientMaxSectionSequence: Ptr(int64(5000))}}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("FetchRequest %+v", r)
	}
	// preserialized sections count too
	w := loadAnswer()
	w.Preserialized = &Preserialized{Items: []PreserializedItem{{IDType: Ptr(IDTypeSection), Values: []Raw{Marshal(Ptr(textSection("a", 1000, "aaa", "one")))}},
		{IDType: Ptr(int32(79)), Values: []Raw{{0xff}}}}}
	if d, err := ReadLoad("TH", w); err != nil || ids(d.Sections()) != "a" {
		t.Errorf("preserialized: %v", err)
	}
	for name, c := range map[string]struct {
		w    *LoadWrapper
		auth bool
	}{
		"error string": {&LoadWrapper{Error: Ptr("Not authorized")}, false},
		"auth":         {&LoadWrapper{Response: &LoadResponse{Failures: []Failure{{Reason: Ptr(FailAuth)}}}}, true},
		"not found":    {&LoadWrapper{Response: &LoadResponse{Failures: []Failure{{Reason: Ptr(FailNotFound)}}}}, false},
		"other thread": {loadAnswer(), false},
	} {
		thread := "TH"
		if name == "other thread" {
			thread = "ELSE"
		}
		_, err := ReadLoad(thread, c.w)
		var le *LoadError
		if !errors.As(err, &le) || le.Auth() != c.auth {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ReadLoad("TH", &LoadWrapper{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no response: %v", err)
	}
}

func TestQuipApplyChanges(t *testing.T) {
	d, err := ReadLoad("TH", loadAnswer(textSection("a", 1000, "aaa", "one"), textSection("b", 2000, "aab", "two")))
	if err != nil {
		t.Fatal(err)
	}
	changes := func(c *Changes) *FetchResponse { return &FetchResponse{Changes: c} }
	// an older update is dropped, the same sequence applies, a newer one replaces
	old := textSection("a", 500, "aaa", "stale")
	same := textSection("b", 2000, "aab", "two again")
	newer := textSection("c", 6000, "aac", "three")
	changed, reload := d.Apply(changes(&Changes{Data: &Payload{
		Documents: []Document{{ID: Ptr("DOCx"), Sequence: Ptr(int64(6000)), MaxSectionSequence: Ptr(int64(6000))}},
		Sections:  []Section{old, same, newer},
	}, Indexes: []IndexChange{{IndexID: Ptr("Section/DOCx"), ChangedEntries: []ChangedEntry{{ID: Ptr("c"), SortValue: Ptr("aac")}}}}}))
	if reload || strings.Join(changed, ",") != "b,c" {
		t.Errorf("changed %v reload %v", changed, reload)
	}
	if SectionRTML(d.Section("a")) != "one" || SectionRTML(d.Section("b")) != "two again" || ids(d.Sections()) != "a,b,c" {
		t.Errorf("after: %q %q %s", SectionRTML(d.Section("a")), SectionRTML(d.Section("b")), ids(d.Sections()))
	}
	if *d.FetchRequest().Requests[0].ClientMaxSectionSequence != 6000 {
		t.Error("the document's max_section_sequence didn't move")
	}
	// a deletion and a rename (temp_ids)
	del := textSection("b", 7000, "aab", "two")
	del.Deleted = Ptr(true)
	d.Apply(changes(&Changes{TempIDs: []TempID{{TempID: Ptr("c"), RealID: Ptr("C1")}}, Data: &Payload{Sections: []Section{del}},
		Indexes: []IndexChange{{IndexID: Ptr("Section/DOCx"), ChangedEntries: []ChangedEntry{{ID: Ptr("b"), Remove: Ptr(true)}}}}}))
	if ids(d.Sections()) != "a,C1" || d.Section("b") == nil || !*d.Section("b").Deleted || d.Section("c") != nil {
		t.Errorf("after delete and rename: %s", ids(d.Sections()))
	}
	// the server says reload: nothing applies
	for name, c := range map[string]*Changes{
		"backlog reload": {Data: &Payload{BacklogReloadModelData: Ptr(true), Sections: []Section{textSection("a", 9000, "aaa", "x")}}},
		"index reload":   {Indexes: []IndexChange{{IndexID: Ptr("Section/DOCx"), Reload: Ptr(true)}}, Data: &Payload{Sections: []Section{textSection("a", 9000, "aaa", "x")}}},
	} {
		if _, reload := d.Apply(changes(c)); !reload || SectionRTML(d.Section("a")) != "one" {
			t.Errorf("%s: reload %v, a = %q", name, reload, SectionRTML(d.Section("a")))
		}
	}
	// a section of another document is not this one's
	other := textSection("z", 9000, "aaz", "elsewhere")
	other.DocumentID = Ptr("OTHER")
	if changed, _ := d.Apply(changes(&Changes{Data: &Payload{Sections: []Section{other}}})); len(changed) != 0 || d.Section("z") != nil {
		t.Error("took another document's section")
	}
	if changed, reload := d.Apply(&FetchResponse{}); changed != nil || reload {
		t.Error("an empty answer changed something")
	}
}

func TestQuipControls(t *testing.T) {
	rtml := `Hi <control id="c1"></control>, <annotation id="a1"><b>x</b><control data-id="c2" id="no"></control></annotation> <control id=c3></control>`
	if got := strings.Join(ControlIDs(rtml), ","); got != "c1,c2,c3" {
		t.Errorf("ControlIDs = %s", got)
	}
	if got := strings.Join(AnnotationIDs(rtml), ","); got != "a1" {
		t.Errorf("AnnotationIDs = %s", got)
	}
	ctl := func(c *Content) *Section { return &Section{SectionClass: Ptr(ClassControl), Content: c} }
	for want, s := range map[[2]any]*Section{
		{ControlUser, "U0123"}:     ctl(&Content{SlackUser: &SlackUser{SlackUserID: Ptr("su:U0123")}}),
		{ControlUser, "S0123"}:     ctl(&Content{SlackUser: &SlackUser{SlackUserID: Ptr("su:S0123")}}),
		{ControlChannel, "C0123"}:  ctl(&Content{SlackChannel: &SlackChannel{SlackChannelID: Ptr("sc:C0123")}}),
		{ControlEmoji, "smile"}:    ctl(&Content{SlackEmoji: &SlackEmoji{SlackEmojiID: Ptr("se:smile/E0123")}}),
		{ControlEmoji, "named"}:    ctl(&Content{SlackEmoji: &SlackEmoji{EmojiName: Ptr("named"), SlackEmojiID: Ptr("se:x/E1")}}),
		{ControlBroadcast, "here"}: ctl(&Content{SlackBroadcast: &SlackBroadcast{Range: Ptr("here")}}),
		{ControlDate, ""}:          ctl(&Content{SlackDate: &SlackDate{UTCDateMs: Ptr(int64(1))}}),
		{ControlNone, ""}:          {Content: &Content{SlackUser: &SlackUser{SlackUserID: Ptr("su:U1")}}}, // not a control section
	} {
		if k, id := Control(s); k != want[0] || id != want[1] {
			t.Errorf("Control = %v %q, want %v %q", k, id, want[0], want[1])
		}
	}
	d, _ := ReadLoad("TH", loadAnswer(textSection("a", 1000, "aaa", `x <control id="c1"></control><control id="gone"></control>`),
		Section{ID: Ptr("c1"), Sequence: Ptr(int64(1000)), SectionClass: Ptr(ClassControl), Content: &Content{SlackBroadcast: &SlackBroadcast{Range: Ptr("here")}}}))
	if cs := d.Controls(d.Section("a")); len(cs) != 1 || *cs[0].ID != "c1" {
		t.Errorf("Controls = %v", cs)
	}
}
