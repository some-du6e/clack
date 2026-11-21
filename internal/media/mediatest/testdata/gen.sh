#!/bin/sh
# The video fixtures (ffmpeg 6.1.1, Ubuntu 24.04's): lavfi colour and sine
# sources, libx264 Constrained Baseline 320x180 at 30 fps tagged BT.709
# limited range, ffmpeg's AAC-LC at 48 kHz, 3.000 s, a keyframe every
# 0.5 s. Grey 0x404040 for the first second, 0xc0c0c0 for the second,
# red for the third. Run in this directory; the flags make it bit-exact.
set -eu
ffmpeg -hide_banner -y -f lavfi -i "color=c=0x404040:s=320x180:r=30:d=1" -f lavfi -i "color=c=0xc0c0c0:s=320x180:r=30:d=1" \
  -f lavfi -i "color=c=0xff0000:s=320x180:r=30:d=1" -f lavfi -i "sine=frequency=440:sample_rate=48000:duration=3" \
  -filter_complex "[0:v][1:v][2:v]concat=n=3:v=1:a=0,format=yuv420p[v]" -map "[v]" -map 3:a \
  -c:v libx264 -profile:v baseline -preset veryslow -g 15 -bf 0 -colorspace bt709 -color_primaries bt709 -color_trc bt709 -color_range tv \
  -c:a aac -b:a 48k -movflags +faststart -map_metadata -1 -fflags +bitexact -flags:v +bitexact -flags:a +bitexact grey-grey-red-3s.mp4
# the same streams with the moov after the mdat (an original not made for
# progressive playback)
ffmpeg -hide_banner -y -i grey-grey-red-3s.mp4 -c copy -movflags -faststart -map_metadata -1 -fflags +bitexact moov-at-end-3s.mp4
# no audio track (a runner with no audio device plays this one)
ffmpeg -hide_banner -y -i grey-grey-red-3s.mp4 -an -c copy -movflags +faststart -map_metadata -1 -fflags +bitexact grey-grey-red-3s-silent.mp4
