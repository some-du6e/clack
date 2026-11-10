package bgblur

import _ "embed"

// ModelTFLite is MediaPipe's selfie segmenter, landscape variant
// (selfie_segmenter_landscape.tflite, float16 weights, 256×144 RGB in
// [0, 1] → 256×144 person probability), as published by Google at
// https://storage.googleapis.com/mediapipe-models/image_segmenter/selfie_segmenter_landscape/float16/latest/selfie_segmenter_landscape.tflite
// (sha256 490e9ea734313e0de10fa0cd9e3c6133e36ea4db2b7a49bde9ef019f72796b8e).
// Apache License 2.0 (MediaPipe's model card); see THIRD_PARTY_NOTICES.md.
// Its weights are byte-for-byte those of the selfie_segmentation_landscape
// model the Chime SDK's BackgroundBlurProcessor loads.
//
//go:embed selfie_segmenter_landscape.tflite
var ModelTFLite []byte
