"""Reference outputs for internal/tflite's TestMediaPipeReference.

Runs MediaPipe's own runtime (the Tasks ImageSegmenter: TensorFlow Lite with
XNNPack and MediaPipe's Convolution2DTransposeBias custom op, which a plain
tflite-runtime Interpreter can't resolve) on each PNG and writes each
confidence mask as little-endian float32, <name>.mask<k>.f32, plus a JSON
summary. Run by .github/workflows/bgblur.yml (job "reference"):

    python -I mediapipe_ref.py MODEL OUTDIR IMAGE...
"""
import json
import os
import sys

import mediapipe as mp
import numpy as np
from mediapipe.tasks.python import vision
from mediapipe.tasks.python.core.base_options import BaseOptions


def main():
    model, outdir, images = sys.argv[1], sys.argv[2], sys.argv[3:]
    os.makedirs(outdir, exist_ok=True)
    opts = vision.ImageSegmenterOptions(
        base_options=BaseOptions(model_asset_path=model),
        running_mode=vision.RunningMode.IMAGE,
        output_confidence_masks=True,
        output_category_mask=False,
    )
    summary = {"mediapipe": mp.__version__, "images": {}}
    with vision.ImageSegmenter.create_from_options(opts) as seg:
        for path in images:
            img = mp.Image.create_from_file(path)
            res = seg.segment(img)
            name = os.path.splitext(os.path.basename(path))[0]
            masks = []
            for k, m in enumerate(res.confidence_masks):
                a = np.ascontiguousarray(m.numpy_view(), dtype="<f4")
                a.tofile(os.path.join(outdir, "%s.mask%d.f32" % (name, k)))
                masks.append({"shape": list(a.shape), "min": float(a.min()), "max": float(a.max()), "mean": float(a.mean())})
            summary["images"][name] = {"width": img.width, "height": img.height, "format": str(img.image_format), "masks": masks}
    with open(os.path.join(outdir, "summary.json"), "w") as f:
        json.dump(summary, f, indent=1)
    print(json.dumps(summary, indent=1))


if __name__ == "__main__":
    main()
