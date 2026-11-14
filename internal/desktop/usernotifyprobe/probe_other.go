//go:build !(darwin && cgo)

package usernotifyprobe

const unsupported = "UNUserNotificationCenter is macOS only (and needs cgo)"

func bundle() string                                                { return unsupported }
func centerErr() string                                             { return unsupported }
func settings(int) string                                           { return unsupported }
func request(int, int) string                                       { return unsupported }
func post(string, string, string, string, string, bool, int) string { return unsupported }
func delivered(int) string                                          { return unsupported }
func deliveredJSON(int) string                                      { return "null" }
func categoriesJSON(int) string                                     { return "null" }
func actionConstants() string                                       { return unsupported }
func delegateClass() string                                         { return unsupported }
func respond(string, string, string, bool, int) string              { return unsupported }
func willPresent(string, int) int                                   { return -1 }
