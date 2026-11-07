package jsrt

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
)

// maxInflate bounds what one DecompressionStream may produce.
const maxInflate = 512 << 20

// zlibOp compresses (mode "c") or decompresses (mode "d") data in one of
// the Compression Streams formats.
func zlibOp(mode, format string, data []byte) ([]byte, error) {
	switch mode {
	case "c":
		var buf bytes.Buffer
		var w io.WriteCloser
		switch format {
		case "gzip":
			w = gzip.NewWriter(&buf)
		case "deflate":
			w = zlib.NewWriter(&buf)
		case "deflate-raw":
			fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
			if err != nil {
				return nil, err
			}
			w = fw
		default:
			return nil, errors.New("Unsupported compression format: '" + format + "'")
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "d":
		var r io.Reader
		var err error
		src := bytes.NewReader(data)
		switch format {
		case "gzip":
			var gr *gzip.Reader
			if gr, err = gzip.NewReader(src); err == nil {
				r = gr
			}
		case "deflate":
			r, err = zlib.NewReader(src)
		case "deflate-raw":
			r = flate.NewReader(src)
		default:
			return nil, errors.New("Unsupported compression format: '" + format + "'")
		}
		if err != nil {
			return nil, errors.New("The compressed data was not valid")
		}
		out, err := io.ReadAll(io.LimitReader(r, maxInflate+1))
		if err != nil {
			return nil, errors.New("The compressed data was not valid")
		}
		if len(out) > maxInflate {
			return nil, errors.New("The decompressed data is too large")
		}
		return out, nil
	}
	return nil, errors.New("zlib: bad mode")
}
