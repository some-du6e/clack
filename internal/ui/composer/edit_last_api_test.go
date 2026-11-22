package composer

import "github.com/some-du6e/clack/internal/core"

// The /s tests reach edit_last.go only through these.

type elCmd = editLastCmd

// elIsCommand is whether the composer's document runs as a slash command.
func elIsCommand(q qDelta) bool { return looksLikeSlashCommand(q) }

// elParse is the /s command in the composer's document.
func elParse(q qDelta) (elCmd, bool) { return parseEditLast(q) }

// elApply is the edit of a message's ops: "noop", "reject" with the
// error, or "save" with the delta saveMessageEdit gets.
func elApply(ops []qOp, c elCmd) (result, errText string, out []qOp) {
	ops, changed := editLastOps(ops, c)
	if !changed {
		return "noop", "", nil
	}
	if ops = qTrim(ops, true, true); len(ops) == 0 {
		return "reject", errEditLastNoText.Error(), nil
	}
	return "save", "", ops
}

// elDoc is the edit of message m: whether it edits, and whether it
// rejected "Message not found".
func elDoc(m core.Message, c elCmd) (edited, notFound bool) {
	d, err := editLastDoc(m, c, nil)
	return d != nil, err == errEditLastNotFound
}
