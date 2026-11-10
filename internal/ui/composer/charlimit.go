package composer

import "unicode/utf16"

// The composer's character limit, as Slack's TextyInput enforces it.
//
// MessagePaneInput, InputContainer and the edit composer all pass
// maxLength: 12e3 (client-boot-render cRQG, boot-shared 63o2 and cjbg).
// TextyInput.getNumCharsOverLimit counts the message with
// getMessageLengthForDelta (boot-shared 9ysi) and, while it is over,
// draws TextyLengthWarning (Orr3): "-N" in a red pill at the box's
// bottom right, with the tooltip "Your message is N characters too
// long". isSendingDisabledForTextState (xjGd) then turns sending off:
// the send button is disabled and Enter does nothing; an edit can't be
// saved (cjbg maybeSave) and says "text is too long!" under the box.

// composerMaxLength is the composers' maxLength.
const composerMaxLength = 12000

// slackLength is getMessageLengthForDelta over the document's delta, in
// JavaScript string units (UTF-16): text counts as typed, a link whose
// text isn't its URL counts both, an emoji its ":name:" text, a user,
// channel or user group mention its id, a broadcast its label
// ("@here"), a message slug its label; the delta's final newline
// doesn't count. Divider embeds count nothing.
func slackLength(d *Doc) int {
	n, line := 0, 0
	for i := 0; i < len(d.Runes); {
		a := d.Attrs[i]
		if at := a.Atom; at != nil {
			switch at.Kind {
			case AtomEmoji:
				n += jsLen(":" + at.ID + ":")
			case AtomBroadcast:
				n += jsLen("@" + at.ID)
			case AtomMessage:
				n += jsLen(at.Text)
			default:
				if at.ID != "" {
					n += jsLen(at.ID)
				} else {
					n += jsLen(at.Text)
				}
			}
			i++
			continue
		}
		r := d.Runes[i]
		if r == '\n' {
			n++
			i++
			line++
			continue
		}
		j := i + 1
		if line < len(d.Lines) && d.Lines[line].Kind == Divider {
			// a divider line holds no text (its delta is an embed)
			for j < len(d.Runes) && d.Runes[j] != '\n' {
				j++
			}
			i = j
			continue
		}
		for j < len(d.Runes) && d.Runes[j] != '\n' && d.Attrs[j].Atom == nil && d.Attrs[j].Link == a.Link {
			j++
		}
		run := d.Runes[i:j]
		n += jsRunes(run)
		if a.Link != "" && string(run) != a.Link {
			n += jsLen(a.Link)
		}
		i = j
	}
	return n
}

// charsOverLimit is getNumCharsOverLimit: how far the message is past
// composerMaxLength. Slack skips the count while the editor holds fewer
// than half the limit (quill.getLength(), which counts embeds as one and
// the trailing newline).
func charsOverLimit(d *Doc) int {
	if d.Len()+1 < composerMaxLength/2 {
		return 0
	}
	return max(slackLength(d)-composerMaxLength, 0)
}

func jsLen(s string) int { return jsRunes([]rune(s)) }

func jsRunes(rs []rune) int {
	n := 0
	for _, r := range rs {
		if l := utf16.RuneLen(r); l > 0 {
			n += l
		} else {
			n++ // U+FFFD in a JS string
		}
	}
	return n
}

// lengthWarningText is TextyLengthWarning's pill: "-" and the count
// with the locale's grouping (formatNumber, "1,234").
func lengthWarningText(over int) string { return "-" + groupDigits(over) }

// lengthWarningTip is its tooltip.
func lengthWarningTip(over int) string {
	if over == 1 {
		return "Your message is 1 character too long"
	}
	return "Your message is " + groupDigits(over) + " characters too long"
}
