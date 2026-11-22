#!/bin/sh
# The audio fixtures (ffmpeg 6.1.1, Ubuntu 24.04's): three lavfi sines of
# 1.000 s each, 440 Hz, then 660 Hz, then 880 Hz (amplitude 1/8), in the
# formats a message's audio player gets: Slack's m4a, mp3 and wav uploads,
# the aac rendition Slack makes of them (AAC-LC), and voice clips' Opus in
# MP4 (plain, and fragmented as Chromium's MediaRecorder writes it), WebM
# and Ogg. Run in this directory; the flags make it bit-exact.
set -eu
src='-f lavfi -i sine=frequency=440:sample_rate=48000:duration=1 -f lavfi -i sine=frequency=660:sample_rate=48000:duration=1 -f lavfi -i sine=frequency=880:sample_rate=48000:duration=1'
cat='[0:a][1:a][2:a]concat=n=3:v=0:a=1[a]'
exact='-map_metadata -1 -fflags +bitexact -flags:a +bitexact'
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ar 16000 -ac 1 -c:a pcm_s16le $exact tones-3s.wav
# both channels at the sine's full level (-ac 2 would upmix it at -3 dB)
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat;[a]pan=stereo|c0=c0|c1=c0[s]" -map "[s]" -ar 44100 -c:a libmp3lame -b:a 64k $exact tones-3s.mp3
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ac 1 -c:a aac -b:a 48k -movflags +faststart $exact tones-3s-aac.m4a
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ac 1 -c:a libopus -b:a 48k -frame_duration 60 -movflags +faststart $exact -f mp4 tones-3s-opus.m4a
# a moov with no samples, then a moof and mdat a second (Chromium's
# Mp4Muxer layout)
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ac 1 -c:a libopus -b:a 48k -frame_duration 60 -movflags +empty_moov+default_base_moof -frag_duration 1000000 $exact -f mp4 tones-3s-opus-frag.m4a
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ac 1 -c:a libopus -b:a 48k $exact tones-3s.webm
# shellcheck disable=SC2086
ffmpeg -hide_banner -y $src -filter_complex "$cat" -map "[a]" -ac 1 -c:a libopus -b:a 48k $exact tones-3s.ogg
