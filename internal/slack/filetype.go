package slack

import "strings"

// FileType is the type Chromium gives a file the user picks, drops or
// pastes (File.type), from its name. Slack's client posts an upload to
// files.getUploadURL's upload_url as FormData with the File as "file"
// (UploadQueue's _uploadFilesFetcher), whose part carries that type, or
// application/octet-stream when it's "". Slack's server files the upload by
// it: an octet-stream part becomes a Binary file whatever its name (Karim's
// .js, 2026-11-22, filetype binary).
//
// The lists are Chromium 156's (Electron 45.3.2, Slack desktop 4.53.292),
// net/base/mime_util.cc kPrimaryMappings then kSecondaryMappings, the first
// entry in order that has the extension, which is the text after the name's
// last "." (blink's GetContentTypeFromFileName) compared ignoring ASCII
// case. Chromium asks the OS between the two lists (the Windows registry,
// the XDG database, macOS's UTIs), which differs from machine to machine;
// Clack keeps to the lists, so an extension neither has (.go, .py, .ts) is
// "".
func FileType(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	ext := name[i+1:]
	if ext == "" || strings.IndexByte(ext, 0) >= 0 {
		return ""
	}
	for _, list := range [...][]mimeInfo{chromiumPrimaryMappings, chromiumSecondaryMappings} {
		for _, m := range list {
			for _, e := range strings.Split(m.exts, ",") {
				if equalFoldASCII(e, ext) {
					return m.mime
				}
			}
		}
	}
	return ""
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
