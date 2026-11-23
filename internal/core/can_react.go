package core

import "strings"

// Whether the viewer can react to a message: Slack's canAddReaction
// (boot-shared, build 35b42de8b870, the module exporting canAddReaction;
// ids renumber between builds, match it by name). ReactionBar's connector
// passes it on as canReact (canReactOverride && canAddReaction), and a
// Reaction pill renders disabled={!canReact}: .c-reaction:disabled is
// pointer-events: none, so the pill takes no click, hover or tooltip.
// The add button and the quick reactions read the same rule. False for:
//
//   - a message that isn't actionable (iub5 isActionable): unknown,
//     ekm_access_denied, dlp_tombstone, dsa_restricted and
//     retention_threshold messages, an EKM error, an ephemeral message, a
//     tombstone, a moderated or thread_hidden one, a temporary (local ts)
//     one; and a file_comment (isFileComment);
//   - in a canvas's file channel, a viewer who may not react to the
//     canvas's comments (CanReactToComment), once files.info is known;
//   - a frozen channel (is_frozen), or a pending Slack Connect DM
//     (is_pending_ext_shared on an IM);
//   - a message in a thread whose root has its replies turned off
//     (isThreadLocked on the message's thread_ts).
//
// The rule's file channel of a thread_share file (fileIsThreadShare) is
// a file Clack never opens as a channel.

// CanAddReaction is canAddReaction for m.
func (a *App) CanAddReaction(m *Message) bool {
	if m == nil || !reactionActionable(m) || m.Subtype == "file_comment" {
		return false
	}
	if strings.HasPrefix(m.Channel, "C") {
		if info, ok := a.KnownCanvasInfo(FileOfChannel(m.Channel)); ok && !a.CanReactToComment(info) {
			return false
		}
	}
	if c, ok := a.S.Conv(m.Channel); ok && (c.IsFrozen || c.IsPendingExtShared && c.IsIM) {
		return false
	}
	return m.ThreadTS == "" || !a.ThreadLocked(m.Channel, m.ThreadTS)
}

// reactionActionable is isActionable as a message's reactions read it.
func reactionActionable(m *Message) bool {
	switch m.Subtype {
	case "ekm_access_denied", "dlp_tombstone", "dsa_restricted", "retention_threshold", "tombstone", "moderated", "thread_hidden":
		return false
	}
	return !m.Tombstone && !m.Ephemeral && !m.Pending && !m.Failed && m.TS != ""
}
