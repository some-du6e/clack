//go:build !linux && !windows && !(darwin && cgo)

package screenlock

func watch(func(bool)) (func(), error) { return nil, ErrUnsupported }
