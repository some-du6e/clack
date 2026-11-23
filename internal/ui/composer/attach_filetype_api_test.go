package composer

// fileMeta is files.info's answer for a draft's file as loadMeta passes it
// on (its mimetype too).
func fileMeta(name, mime string) *attMeta { return &attMeta{name: name, mime: mime} }
