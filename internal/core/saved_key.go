package core

import (
	"regexp"
	"strings"
)

// SavedItemID is getIdForSavedItem (boot-render 3h/T Fz): the key a saved
// item's notification carries as its msg and its Later link's item_id. A
// message's is "channel-ts", a list record's "list-record", a canvas
// section's "file-section", a list field's "list-record-column", anything
// else its item id. (Clack's saved set names a list record by the event's
// ts when it has no record_id.)
func SavedItemID(it *SavedItem) string {
	if it == nil {
		return ""
	}
	switch it.ItemType {
	case "message":
		return it.ItemID + "-" + it.TS
	case "list_record":
		return it.ItemID + "-" + firstNonEmpty(it.RecordID, it.TS)
	case "canvas_section":
		return it.ItemID + "-" + it.SectionID
	case "list_record_field":
		return it.ItemID + "-" + firstNonEmpty(it.RecordID, it.TS) + "-" + it.ColumnID
	}
	return it.ItemID
}

// savedMessageKeyRE is looksLikeSavedItemId's message form: a
// conversation's id and a message's ts.
var savedMessageKeyRE = regexp.MustCompile(`^([CDG][A-Z0-9]{2,})-([0-9]{1,12}\.[0-9]{1,8})$`)

// savedOtherKeyRE is looksLikeSavedItemId's other forms: a file's or a
// list's id, alone or with a record, a section or a draft record after it.
var savedOtherKeyRE = regexp.MustCompile(`^F[A-Z0-9]{2,}(-[A-Za-z0-9:_.-]+)?$`)

// ParseSavedItemID is looksLikeSavedItemId over a notification's msg: ok for
// a saved item's key, with the conversation and the message for a
// message's.
func ParseSavedItemID(key string) (ch, ts string, ok bool) {
	if m := savedMessageKeyRE.FindStringSubmatch(key); m != nil {
		return m[1], m[2], true
	}
	return "", "", savedOtherKeyRE.MatchString(key) && !strings.HasPrefix(key, "F-")
}
