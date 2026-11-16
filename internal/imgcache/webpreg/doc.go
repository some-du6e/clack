// Package webpreg has no code of its own; it exists only so its test
// binary can check, in a build that does not link golang.org/x/image/webp,
// that importing imgcache registers a budgeted WebP decoder with the
// standard image package.
//
// imgcache's own test binary links golang.org/x/image/webp (its tests
// decode reference images with it), and that package's init registers
// "webp" with image before imgcache's init runs — so image.DecodeConfig
// and image.Decode there reach x/image's unbudgeted decoder whether or not
// imgcache registers its own, and a test in that package can't tell the
// difference. imgcache's production code links only the vendored decoder
// (internal/webp), so this package, importing imgcache but not
// x/image/webp, sees exactly what the shipped binary sees. The
// supply-chain / ci guard that `go list -deps ./cmd/clack` excludes
// golang.org/x/image/webp keeps that true of the binary itself.
package webpreg
