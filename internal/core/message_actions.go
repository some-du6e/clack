package core

import "strings"

// Message predicates Slack's message actions read (boot-shared and
// client-boot, build 83aff18a1847b8e9; module ids renumber between builds,
// so match them by export name):
//
//   - isEphemeral (w9uO a): the message's is_ephemeral, "Only visible to
//     you". Clack keeps it on the message (Message.Ephemeral) and in the
//     Interactive index of the socket's ephemerals.
//   - isTempMsg (xmpJ A: isTempTs, dUDN, a ts that is empty or holds an
//     "x"): a send Slack hasn't confirmed, whose ts is the client's fake
//     one (`${unix}.${n padded to 6 with "x"}`), and an ephemeral the
//     client made itself (addEphemeralMessage with no ts takes the same
//     fake ts: slash command feedback, the send checks' notices). Clack's
//     pending and failed sends, and its LocalEphemeral notices
//     (Interactive.LocalNotice), whose local ts has no "x".
//   - isActionable (iub5 nM): not unknown, ekm_access_denied,
//     dlp_tombstone, dsa_restricted, an EKM error or retention_threshold,
//     and neither ephemeral, a tombstone, moderated (moderated or
//     thread_hidden: nPOq isModerated) nor a temporary message.

// IsEphemeral is isEphemeral for m.
func (a *App) IsEphemeral(m *Message) bool {
	if m == nil {
		return false
	}
	if m.Ephemeral {
		return true
	}
	return a != nil && m.TS != "" && a.Interactive().Ephemeral(m.Channel, m.TS)
}

// IsTempMsg is isTempMsg for m.
func (a *App) IsTempMsg(m *Message) bool {
	if m == nil {
		return false
	}
	if m.Pending || m.Failed || m.TS == "" || strings.Contains(m.TS, "x") {
		return true
	}
	return a != nil && a.Interactive().LocalNotice(m.Channel, m.TS)
}

// Actionable is isActionable for m.
func (a *App) Actionable(m *Message) bool {
	if m == nil {
		return false
	}
	switch m.Subtype {
	case "ekm_access_denied", "dlp_tombstone", "dsa_restricted", "retention_threshold", "tombstone", "moderated", "thread_hidden":
		return false
	}
	return !m.Tombstone && !a.IsEphemeral(m) && !a.IsTempMsg(m)
}
