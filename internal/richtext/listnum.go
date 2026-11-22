package richtext

import (
	"math"
	"strconv"
)

// A rich_text_list's indent and offset as Slack's client takes them: the
// indent 0-8 (data-indent), and the offset only when it is ≥ 0 (RichTextList
// sets counterReset "list-{indent} {offset}" for an offset ≥ 0, else counts
// from 1), in the 32 bits Chromium keeps a CSS counter in (it clamps a
// counter-reset beyond them and saturates each increment). A list's
// markers, its plain text and a notification's text all count from these,
// so a sender's huge or negative numbers can't overflow a count, index a
// marker table out of range or ask for a gigabyte of "m".
const maxListIndent = 8

func listIndent(n int) int { return min(max(n, 0), maxListIndent) }

func listOffset(n int) int { return min(max(n, 0), math.MaxInt32) }

// ListNumber is the number of list b's item i: the list's offset + i + 1,
// held at the counter's 32-bit limit.
func ListNumber(b *Block, i int) int {
	return int(min(int64(b.Offset)+int64(i)+1, math.MaxInt32))
}

// LowerRoman is CSS's lower-roman counter style for n: its range is 1 to
// 3999, and a number outside it falls back to decimal, as the browser
// draws an ordered list's marker and Slack's client shows it.
func LowerRoman(n int) string {
	if n < 1 || n > 3999 {
		return strconv.Itoa(n)
	}
	digits := [4][10]string{
		{"", "i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix"},
		{"", "x", "xx", "xxx", "xl", "l", "lx", "lxx", "lxxx", "xc"},
		{"", "c", "cc", "ccc", "cd", "d", "dc", "dcc", "dccc", "cm"},
		{"", "m", "mm", "mmm"},
	}
	return digits[3][n/1000] + digits[2][n/100%10] + digits[1][n/10%10] + digits[0][n%10]
}
