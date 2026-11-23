package core

// snippetFile is the snippet dialog's upload (composer's snippetUpload):
// content as a Blob of the snippet type's mime type.
func snippetFile(name, snippetType, mime, content string) UploadFile {
	return UploadFile{Name: name, Data: []byte(content), SnippetType: snippetType, Type: mime}
}
