package hunspell

import (
	"os"
	"slices"
	"testing"
	"time"
)

// A small dictionary in Hunspell's text format, exercising the rule kinds
// the shipped dictionaries use.
const testAff = `SET UTF-8
TRY esianrtolcdugmphbyfvkwz'
ICONV 1
ICONV ’ '
NOSUGGEST !
FORBIDDENWORD X
KEEPCASE K
NEEDAFFIX N
COMPOUNDMIN 1
ONLYINCOMPOUND c
COMPOUNDRULE 2
COMPOUNDRULE n*1t
COMPOUNDRULE n*mp
REP 2
REP f ph
REP alot a_lot
PFX U Y 1
PFX U 0 un .
SFX D Y 4
SFX D 0 d e
SFX D y ied [^aeiou]y
SFX D 0 ed [^ey]
SFX D 0 ed [aeiou]y
SFX S Y 4
SFX S y ies [^aeiou]y
SFX S 0 s [aeiou]y
SFX S 0 es [sxzh]
SFX S 0 s [^sxzhy]
SFX G Y 2
SFX G e ing e
SFX G 0 ing [^e]
SFX L N 1
SFX L 0 ly/S .
`

const testDic = `16
hello/S
lock/UDSG
happy/U
try/DS
phone/S
the
lot/S
a
NASA
iPhone
OpenGL/K
shit/X
wordy/!
don't
0/nm
1/n1
2/nm
1st/p
1th/tc
`

func testDict() *Dict { return ParseAffDic(testAff, testDic) }

func TestCheckAffixes(t *testing.T) {
	d := testDict()
	for _, w := range []string{
		"hello", "hellos", "lock", "locked", "locking", "locks", "unlock", "unlocked", "unlocks",
		"happy", "unhappy", "tried", "tries", "phones", "the", "a",
		"Hello", "HELLO", "Unlocked", "NASA", "iPhone", "OpenGL",
		"don’t", "21st", "11th", "100", "3.14", "1,000",
	} {
		if !d.Check(w) {
			t.Errorf("Check(%q) = false, want true", w)
		}
	}
	for _, w := range []string{
		"helo", "lockd", "unhello", "tryed", "happyed", "nasa", "Iphone", "openGL",
		"shit", "Shit", "1t", "1th", "hellly",
	} {
		if d.Check(w) {
			t.Errorf("Check(%q) = true, want false", w)
		}
	}
}

func TestSuggest(t *testing.T) {
	d := testDict()
	cases := map[string]string{
		"helo":   "hello",
		"teh":    "the",
		"lokc":   "lock",
		"fone":   "phone", // REP f→ph
		"unlokc": "unlock",
		"Helo":   "Hello", // re-cased
		"HELO":   "HELLO",
		"alot":   "a lot", // REP with a space
	}
	for in, want := range cases {
		got := d.Suggest(in)
		if !slices.Contains(got, want) {
			t.Errorf("Suggest(%q) = %q, want it to contain %q", in, got, want)
		}
	}
	if got := d.Suggest("wordyy"); slices.Contains(got, "wordy") {
		t.Errorf("Suggest offered a NOSUGGEST word: %q", got)
	}
}

func TestAddWord(t *testing.T) {
	d := testDict()
	if d.Check("Clack") {
		t.Fatal("Clack is in the test dictionary")
	}
	d.Add("Clack")
	if !d.Check("Clack") || !d.Check("CLACK") {
		t.Error("an added word isn't accepted")
	}
}

// TestBDicEnUS loads Chromium's en-US dictionary, the file Slack's Linux
// app downloads (CI fetches it into CLACK_TEST_BDIC_DIR).
func TestBDicEnUS(t *testing.T) {
	dir := os.Getenv("CLACK_TEST_BDIC_DIR")
	if dir == "" {
		t.Skip("CLACK_TEST_BDIC_DIR not set")
	}
	data, err := os.ReadFile(dir + "/en-US-10-1.bdic")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseBDic(data)
	if err != nil {
		t.Fatal(err)
	}
	if d.Words() < 40000 {
		t.Fatalf("only %d roots", d.Words())
	}
	for _, w := range []string{"hello", "world", "spelling", "dictionaries", "unbelievable", "running", "don't", "don’t", "Monday", "NASA", "1st", "22nd", "103rd", "Wednesday", "children's"} {
		if !d.Check(w) {
			t.Errorf("Check(%q) = false", w)
		}
	}
	for _, w := range []string{"helo", "teh", "recieve", "definately", "wierd", "monday", "accomodate"} {
		if d.Check(w) {
			t.Errorf("Check(%q) = true", w)
		}
	}
	for in, want := range map[string]string{"Slackk": "Slack", "ammount": "amount", "thier": "their", "xyzzyq": "", "teh": "the", "recieve": "receive", "definately": "definitely", "wierd": "weird", "accomodate": "accommodate", "helo": "hello"} {
		start := time.Now()
		got := d.Suggest(in)
		if len(got) > 5 {
			got = got[:5]
		}
		t.Logf("Suggest(%q) = %q in %v", in, got, time.Since(start))
		if want != "" && !slices.Contains(got, want) {
			t.Errorf("Suggest(%q)[:5] = %q, want %q", in, got, want)
		}
	}
}
