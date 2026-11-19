//go:build !js

package popups

import _ "embed"

// Slack's own pictures, copied from its bundles (THIRD_PARTY_NOTICES.md,
// "Slack"). The browser demo draws stand-ins at the same sizes instead
// (slack_assets_js.go).

//go:embed assets/user-group-section-group-3de1100.png
var ugSectionPNG []byte

//go:embed assets/empty-search-light-b73db25@1x.png
var srcEmptyPNG1x []byte

//go:embed assets/empty-search-light-b73db25@2x.png
var srcEmptyPNG2x []byte

//go:embed assets/default_new_app_icon-936d613@256.png
var defaultAppIconPNG []byte

//go:embed assets/magnifying-glass-d8a6430@512.png
var adGlassPNG []byte
