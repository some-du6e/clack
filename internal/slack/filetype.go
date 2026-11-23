package slack

import (
	"runtime"
	"strings"
	"unicode/utf16"
)

// FileType is the type Chromium gives a file the user picks, drops or
// pastes (File.type), from its name. Slack's client posts an upload to
// files.getUploadURL's upload_url as FormData with the File as "file"
// (UploadQueue's _uploadFilesFetcher), whose part carries that type, or
// application/octet-stream when it's "". Slack's server files the upload by
// it: an octet-stream part becomes a Binary file whatever its name (Karim's
// .js, 2026-11-22, filetype binary).
//
// It's Chromium 156's GetMimeTypeFromExtension (Electron 45.3.2, Slack
// desktop 4.53.292) for the text after the name's last "." (blink's
// GetContentTypeFromFileName, the lookup a user's File gets): net/base/
// mime_util.cc's kPrimaryMappings, then what this machine's OS gives the
// extension (platformMimeType: the Windows registry, macOS's UTType, the
// XDG shared-mime-info database), then kSecondaryMappings. The lists match
// ignoring ASCII case, the first entry in order that has the extension
// winning; the OS's answer differs from machine to machine as it does for
// Slack desktop on that machine (.py, which neither list has, is
// text/x-python-script on a Mac and "" where the OS knows no type).
func FileType(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	return mimeTypeFromExtension(name[i+1:])
}

// mimeTypeFromExtension is mime_util.cc's GetMimeTypeFromExtensionHelper
// with include_platform_types: nothing for an extension of more than 65536
// path units (UTF-16 on Windows, bytes elsewhere) or with a NUL in it, else
// the primary list's type, the OS's, the secondary list's, or "".
func mimeTypeFromExtension(ext string) string {
	if extPathUnits(ext) > 65536 || strings.IndexByte(ext, 0) >= 0 {
		return ""
	}
	if m := findMimeType(chromiumPrimaryMappings, ext); m != "" {
		return m
	}
	if m, ok := platformMimeType(ext); ok {
		return m
	}
	return findMimeType(chromiumSecondaryMappings, ext)
}

// findMimeType is mime_util.cc's FindMimeType: the first entry in list
// order with ext among its extensions, ignoring ASCII case.
func findMimeType(list []mimeInfo, ext string) string {
	for _, m := range list {
		for _, e := range strings.Split(m.exts, ",") {
			if equalFoldASCII(e, ext) {
				return m.mime
			}
		}
	}
	return ""
}

// extPathUnits is ext's length as base::FilePath::StringType counts it:
// UTF-16 units on Windows (each byte of invalid UTF-8 one U+FFFD), bytes
// elsewhere.
func extPathUnits(ext string) int {
	if runtime.GOOS != "windows" {
		return len(ext)
	}
	n := 0
	for _, r := range ext {
		n += utf16.RuneLen(r)
	}
	return n
}

// equalFoldASCII is base::EqualsCaseInsensitiveASCII: only A-Z fold
// (strings.EqualFold would also match "ſ" to "s" and "K" to "k").
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

type mimeInfo struct{ mime, exts string }

// chromiumPrimaryMappings is mime_util.cc's kPrimaryMappings: these win
// over the OS, so they're every platform's.
var chromiumPrimaryMappings = []mimeInfo{
	{"video/webm", "webm"},
	{"audio/mpeg", "mp3"},
	{"application/wasm", "wasm"},
	{"application/x-chrome-extension", "crx"},
	{"application/xhtml+xml", "xhtml,xht,xhtm"},
	{"audio/flac", "flac"},
	{"audio/matroska", "mka"},
	{"audio/mp3", "mp3"},
	{"audio/ogg", "ogg,oga,opus"},
	{"audio/wav", "wav"},
	{"audio/webm", "webm"},
	{"audio/x-m4a", "m4a"},
	{"image/avif", "avif"},
	{"image/gif", "gif"},
	{"image/jpeg", "jpeg,jpg,jpe"},
	{"image/jxl", "jxl"},
	{"image/png", "png"},
	{"image/apng", "png,apng"},
	{"image/svg+xml", "svg,svgz"},
	{"image/webp", "webp"},
	{"multipart/related", "mht,mhtml"},
	{"text/css", "css"},
	{"text/html", "html,htm,shtml,shtm"},
	{"text/javascript", "js,mjs"},
	{"text/xml", "xml"},
	{"video/matroska", "mkv"},
	{"video/mp4", "mp4,m4v"},
	{"video/ogg", "ogv,ogm"},
	{"text/csv", "csv"},
}

// chromiumSecondaryMappings is mime_util.cc's kSecondaryMappings (the
// ChromeOS-only line left out): Chromium uses these when the OS has nothing.
var chromiumSecondaryMappings = []mimeInfo{
	{"image/x-icon", "ico"},
	{"application/epub+zip", "epub"},
	{"application/font-woff", "woff"},
	{"application/gzip", "gz,tgz"},
	{"application/javascript", "js"},
	{"application/json", "json"},
	{"application/msword", "doc,dot"},
	{"application/octet-stream", "bin,exe,com"},
	{"application/pdf", "pdf"},
	{"application/pkcs7-mime", "p7m,p7c,p7z"},
	{"application/pkcs7-signature", "p7s"},
	{"application/postscript", "ps,eps,ai"},
	{"application/rdf+xml", "rdf"},
	{"application/rss+xml", "rss"},
	{"application/rtf", "rtf"},
	{"application/vnd.android.package-archive", "apk"},
	{"application/vnd.mozilla.xul+xml", "xul"},
	{"application/vnd.ms-excel", "xls"},
	{"application/vnd.ms-powerpoint", "ppt"},
	{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx"},
	{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
	{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"},
	{"application/x-gzip", "gz,tgz"},
	{"application/x-mpegurl", "m3u8"},
	{"application/x-shockwave-flash", "swf,swl"},
	{"application/x-tar", "tar"},
	{"application/x-x509-ca-cert", "cer,crt"},
	{"application/zip", "zip"},
	{"audio/webm", "weba"},
	{"image/bmp", "bmp"},
	{"image/jpeg", "jfif,pjpeg,pjp"},
	{"image/tiff", "tiff,tif"},
	{"image/vnd.microsoft.icon", "ico"},
	{"image/x-png", "png"},
	{"image/x-xbitmap", "xbm"},
	{"message/rfc822", "eml"},
	{"text/calendar", "ics"},
	{"text/html", "ehtml"},
	{"text/markdown", "md"},
	{"text/plain", "txt,text"},
	{"text/vtt", "vtt"},
	{"text/x-sh", "sh"},
	{"text/xml", "xsl,xbl,xslt"},
	{"video/mpeg", "mpeg,mpg,mpe"},
}
