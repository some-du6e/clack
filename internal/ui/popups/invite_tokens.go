package popups

// The invite modal's email field logic (popups/invite.go), ported from
// Slack's web client (build 4d0c78085232):
//
//   - splitting typed or pasted text into addresses: boot-shared m5BY
//     splitToEmails over client-boot sdt0 defaultSplitQuery (with the
//     members' matchEmailField option: "Name <a@b.c>" lists give their
//     addresses), splitBareList, the mailto: prefix dropped;
//   - when the text becomes tokens: m5BY tokenizeFilter (a separator, a
//     trailing space), EmailInput's Enter and blur (B7l2);
//   - each token's errors and the counts: boot-shared 1Glt
//     deriveTokenState (invalid_email, duplicate, self_invite,
//     already_in_workspace), the 200-address limit (CJ8h
//     TokenizerEmailInput), isBlocked and getBlockedReason;
//   - the address check: client-boot itIg isEmail.

import (
	"regexp"
	"strings"
)

// Token errors (1Glt).
const (
	invErrInvalid   = "invalid_email"
	invErrDuplicate = "duplicate"
	invErrSelf      = "self_invite"
	invErrAlready   = "already_in_workspace"
)

// invEmailLimit is TokenizerEmailInput's limit: 200 addresses at a time.
const invEmailLimit = 200

// hXK4 gy: the email pattern (itIg anchors it; sdt0 searches with it).
const invEmailPat = "[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*@(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?"

var (
	invEmailRe    = regexp.MustCompile("(?i)^" + invEmailPat + "$")
	invContainsRe = regexp.MustCompile("(?i)" + invEmailPat)
	invAngleRe    = regexp.MustCompile("(?i)<(" + invEmailPat + ")>")
	invParenRe    = regexp.MustCompile("(?i)\\((" + invEmailPat + ")\\)")
	// sdt0 i and m5BY r: the separators
	invSepRe = regexp.MustCompile("[,，、;；|｜\t\n\r]")
	// m5BY s: text that ends in a separator
	invSepEndRe = regexp.MustCompile("[,，、;；|｜]$")
	// m5BY c: nothing but separators and spaces
	invEmptyRe = regexp.MustCompile("^[,，、;；|｜\\s]*$")
	// sdt0 o: before a run of spaces and an @ (RE2 has no lookahead; see
	// invSplitAt)
	invAtRe     = regexp.MustCompile(`\s+[@\x{FF20}]`)
	invMailtoRe = regexp.MustCompile(`(?i)^mailto:`)
	invTrailWS  = regexp.MustCompile(`\s$`)
)

// isInviteEmail is itIg isEmail.
func isInviteEmail(s string) bool { return s != "" && invEmailRe.MatchString(s) }

// invSplitAt is s.split(/(?=\s+[@＠])/): cut before each run of spaces
// that ends in an @.
func invSplitAt(s string) []string {
	var out []string
	last := 0
	for _, m := range invAtRe.FindAllStringIndex(s, -1) {
		if m[0] == 0 {
			continue
		}
		out = append(out, s[last:m[0]])
		last = m[0]
	}
	return append(out, s[last:])
}

// invDefaultSplit is sdt0 defaultSplitQuery(e, {entities: {members:
// {matchEmailField: true}}}).
func invDefaultSplit(e string) []string {
	var n []string
	switch {
	case invAngleRe.MatchString(e) || invParenRe.MatchString(e):
		n = invContainsRe.FindAllString(e, -1)
	case invSepRe.MatchString(e):
		n = invSepRe.Split(e, -1)
	case invAtRe.MatchString(e) && invAtRe.FindStringIndex(e)[0] > 0:
		n = invSplitAt(e)
	}
	if len(n) == 0 {
		n = []string{e} // entities set: the whole query
	}
	out := n[:0:0]
	for _, s := range n {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// invBareEmail is m5BY isBareEmail.
func invBareEmail(e string) bool {
	if strings.ContainsAny(e, "<>(),;\"") {
		return false
	}
	t := strings.Index(e, "@")
	return t > 0 && t == strings.LastIndex(e, "@") && t < len(e)-1
}

// invSplitBareList is m5BY splitBareList: "a@x b@y" is two addresses.
func invSplitBareList(e string) []string {
	var t []string
	for _, p := range strings.Split(e, " ") {
		if p != "" {
			t = append(t, p)
		}
	}
	if len(t) > 1 {
		for _, p := range t {
			if !invBareEmail(p) {
				return []string{e}
			}
		}
		return t
	}
	return []string{e}
}

// inviteSplit is m5BY splitToEmails: text to the addresses it holds.
func inviteSplit(e string) []string {
	t := invDefaultSplit(e)
	if len(t) == 0 {
		t = []string{e}
	}
	var out []string
	for _, s := range t {
		for _, p := range invSplitBareList(strings.TrimSpace(s)) {
			p = strings.TrimSpace(invMailtoRe.ReplaceAllString(p, ""))
			if !invEmptyRe.MatchString(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// inviteTokenize is tokenizeFilter's commit for the field's text q:
// commit reports whether q becomes the tokens add (and the field
// empties); a q of nothing but separators and spaces is dropped (EmailInput
// onFilterQueryChange).
func inviteTokenize(q string) (add []string, commit bool) {
	if invSepRe.MatchString(q) {
		e := inviteSplit(q)
		if len(e) != 1 || invSepEndRe.MatchString(q) {
			return e, true
		}
	}
	if q != "" && invTrailWS.MatchString(q) {
		if e := inviteSplit(q); len(e) > 0 {
			return e, true
		}
	}
	if q != "" && len(inviteSplit(q)) == 0 {
		return nil, true
	}
	return nil, false
}

// invToken is one address with its errors (1Glt deriveTokenState).
type invToken struct {
	Email  string
	Errors []string
	Index  int
}

func (t invToken) has(e string) bool {
	for _, x := range t.Errors {
		if x == e {
			return true
		}
	}
	return false
}

// invTokenState is deriveTokenState's result.
type invTokenState struct {
	Tokens                         []invToken
	ErrorCount, Invitable          int
	Blocking, OverLimit            int
	Counts                         map[string]int
	InvalidN, DupN, SelfN, MemberN int
}

// deriveInviteTokens is deriveTokenState(emails, {creatorEmail, annotations,
// limit: 200}): member reports an address of a workspace member (the
// annotator).
func deriveInviteTokens(emails []string, creator string, member func(email string) bool) invTokenState {
	creator = strings.ToLower(strings.TrimSpace(creator))
	st := invTokenState{Counts: map[string]int{}}
	first := map[string]int{}
	for i, e := range emails {
		n := strings.ToLower(strings.TrimSpace(e))
		_, dup := first[n]
		if !dup {
			first[n] = i
		}
		var errs []string
		switch {
		case !isInviteEmail(n):
			errs = append(errs, invErrInvalid)
		default:
			if dup {
				errs = append(errs, invErrDuplicate)
			}
			if creator != "" && n == creator {
				errs = append(errs, invErrSelf)
			} else if member != nil && member(n) {
				errs = append(errs, invErrAlready)
			}
		}
		for _, x := range errs {
			st.Counts[x]++
		}
		st.Tokens = append(st.Tokens, invToken{Email: e, Errors: errs, Index: i})
		if len(errs) > 0 {
			st.ErrorCount++
		} else {
			st.Invitable++
		}
	}
	st.Blocking = st.ErrorCount // nothing is skipped off Grid's smart tokens
	if len(emails) > invEmailLimit {
		st.OverLimit = len(emails) - invEmailLimit
	}
	st.InvalidN, st.DupN, st.SelfN, st.MemberN = st.Counts[invErrInvalid], st.Counts[invErrDuplicate], st.Counts[invErrSelf], st.Counts[invErrAlready]
	return st
}

// blocked is CJ8h isBlocked: Send is off.
func (s invTokenState) blocked() bool { return s.Invitable == 0 || s.OverLimit > 0 || s.Blocking > 0 }

// invitable is the addresses Send would invite.
func (s invTokenState) invitable() []string {
	var out []string
	for _, t := range s.Tokens {
		if len(t.Errors) == 0 {
			out = append(out, t.Email)
		}
	}
	return out
}

// withoutError is the emails less every token with error e (the error
// rows' Remove all…, TokenizerEmailInput V).
func (s invTokenState) withoutError(e string) []string {
	var out []string
	for _, t := range s.Tokens {
		if !t.has(e) {
			out = append(out, t.Email)
		}
	}
	return out
}

// withinLimit drops the addresses past the 200th ("Remove the last …").
func (s invTokenState) withinLimit() []string {
	var out []string
	for i, t := range s.Tokens {
		if i < invEmailLimit {
			out = append(out, t.Email)
		}
	}
	return out
}
