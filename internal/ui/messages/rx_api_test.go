package messages

// rxOff reports whether r's reaction pills are disabled (a red run's stub
// is the code before it).
func rxOff(r *row) bool { return r.reactOff }
