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

// The id shapes looksLikeSavedItemId and looksLikeSalesforceTaskTodoId
// (client-boot 3h/T) read: a reminder's (QXHQ looksLikeReminderId), a
// file's or a list's (oRye looksLikeFileId), a conversation's (nQiy
// looksLikeChannelId), a message's ts (e5ct looksLikeMessageTimestamp)
// and a list record's (iLfW looksLikeRecordId).
var (
	savedReminderRE = regexp.MustCompile(`^@?Sa[A-Z0-9]{8,}$`)
	savedFileRE     = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)
	savedChannelRE  = regexp.MustCompile(`^(C|G|D|Z)[A-Z0-9]{8,}$`)
	savedTSRE       = regexp.MustCompile(`^[0-9]{10}\.[0-9]{6}$`)
	savedRecordRE   = regexp.MustCompile(`^Rec[A-Z0-9]{8,}$`)
)

// looksLikeSavedItemID is looksLikeSavedItemId: a reminder's or a file's
// id, or a conversation and a message's ts, a list and a record, or a list
// and a draft record ("temp:C:…"), joined by "-".
func looksLikeSavedItemID(key string) bool {
	if savedReminderRE.MatchString(key) || savedFileRE.MatchString(key) {
		return true
	}
	first, rest, _ := strings.Cut(key, "-")
	second, _, _ := strings.Cut(rest, "-")
	return savedChannelRE.MatchString(first) && savedTSRE.MatchString(second) ||
		savedFileRE.MatchString(first) && (savedRecordRE.MatchString(second) || strings.HasPrefix(second, "temp:C:"))
}

// looksLikeSalesforceTaskTodoID is looksLikeSalesforceTaskTodoId: a
// Salesforce record's composite id ("S:org:record", FNHU) whose record is
// a task (key prefix 00T, 8gF2).
func looksLikeSalesforceTaskTodoID(key string) bool {
	parts := strings.Split(key, ":")
	part := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	return part(0) == "S" && part(1) != "" && part(2) != "" && strings.HasPrefix(part(2), "00T")
}

// ParseSavedItemID is notificationClicked's test of a notification's msg
// (looksLikeSavedItemId or looksLikeSalesforceTaskTodoId: the toast opens
// Later, openSavedForLaterView), with the conversation and the message
// when the key is a message's (the one openSavedForLaterView opens beside
// the list: looksLikeChannelId and looksLikeMessageTimestamp).
func ParseSavedItemID(key string) (ch, ts string, ok bool) {
	if !looksLikeSavedItemID(key) && !looksLikeSalesforceTaskTodoID(key) {
		return "", "", false
	}
	first, rest, _ := strings.Cut(key, "-")
	second, _, _ := strings.Cut(rest, "-")
	if savedChannelRE.MatchString(first) && savedTSRE.MatchString(second) {
		return first, second, true
	}
	return "", "", true
}
