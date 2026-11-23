package ui

import "image"

// What TestPluginGapHideResolved taps, on the red tree (over the code
// before HideResolved's port): the channel header has no ViewHeaderSearch
// for a patch to draw beside, so nothing calls it.
var hdrSearchTap func(ch string, placed image.Rectangle, button image.Point)
