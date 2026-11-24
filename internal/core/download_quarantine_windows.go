package core

import "os"

// MarkDownloaded is Chromium's Mark-of-the-Web on a completed download on
// Windows, which Slack desktop's downloads all get (DownloadItemImpl's
// OnDownloadCompleting → RenameAndAnnotate → BaseFile's
// AnnotateWithSourceInformation → quarantine's
// SetInternetZoneIdentifierDirectly): the file's Zone.Identifier stream
// (zoneIdentifier), so SmartScreen, Office's Protected View and the
// macro block treat it as from the internet. A filesystem without
// alternate streams (FAT, exFAT) can't hold it; as Chromium's
// ANNOTATION_FAILED, the download is complete all the same. Hard links
// share the stream, so the final path is enough.
func MarkDownloaded(path, url string) {
	f, err := os.OpenFile(path+":Zone.Identifier", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(zoneIdentifier(url, ""))
	_ = f.Close()
}
