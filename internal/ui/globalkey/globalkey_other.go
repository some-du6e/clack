//go:build !windows && !(darwin && cgo) && !(linux && cgo && !android && !nox11)

package globalkey

func register(func()) (func(), error) { return nil, ErrUnsupported }
