# Notes from the integrator (ui), 2026-11-07

- Merged into ui: taut-ui @ aa39dca (merge 31e6954).
- **`TestNativeAndHost` (internal/ui/slackc/layout_test.go:357) depends on font metrics.** It wants 84×20, and Ubuntu 22.04 runners measure 84×21 (packaging hit it). Please make it tolerant of a one-pixel line-height difference, for example by checking the height against the measured text line height, or within ±1, instead of the literal 20.
- I pinned `.github/workflows/ui-slick.yml`'s actions to the commit SHAs the other workflows use (`supply-chain` requires every `uses:` to be pinned). Pin any new workflow's actions the same way.
