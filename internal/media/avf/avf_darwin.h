// Clack's video player on macOS: AVFoundation's AVPlayer reading a file
// Go serves through a resource loader (a made-up URL scheme, never the
// file's), its frames pulled from an AVPlayerItemVideoOutput as Metal
// textures on Gio's own device through a CVMetalTextureCache
// (avf_darwin.m).
#ifndef CLACK_AVF_H
#define CLACK_AVF_H

#include <stddef.h>
#include <stdint.h>

// clackAVState is the player's state, read on its queue.
typedef struct {
	double pos, dur, fps, rate;
	int paused, ended, waiting, ready, err;
	int32_t errCode;
	uint32_t w, h;
} clackAVState;

// The events the player's queue hands Go (clackAVFEvent).
enum {
	CLACK_AV_STATE = 0, // the state only
	CLACK_AV_METADATA,
	CLACK_AV_DATA,
	CLACK_AV_PLAY,
	CLACK_AV_PLAYING,
	CLACK_AV_PAUSE,
	CLACK_AV_WAITING,
	CLACK_AV_SEEKING,
	CLACK_AV_SEEKED,
	CLACK_AV_ENDED,
	CLACK_AV_DURATION,
	CLACK_AV_ERROR,
	CLACK_AV_FIRSTFRAME,
};

// The counts of live objects (clack_avf_live), for tests.
enum { CLACK_AV_LIVE_PLAYERS, CLACK_AV_LIVE_BUFFERS, CLACK_AV_LIVE_TEXTURES, CLACK_AV_LIVE_REQUESTS, CLACK_AV_LIVE_N };

void *clack_avf_new(uintptr_t h, int64_t total, int seekable, const char *uti, const char *url, int drawW, int drawH);
void clack_avf_play(void *p);
void clack_avf_pause(void *p);
void clack_avf_seek(void *p, double t);
void clack_avf_volume(void *p, double v);
void clack_avf_muted(void *p, int m);
void clack_avf_loop(void *p, int l);
void clack_avf_rate(void *p, double r);
void clack_avf_drawsize(void *p, int w, int h);
void clack_avf_asked(void *p);
int clack_avf_ask(void *p, uintptr_t device, uintptr_t *tex, uint64_t *key);
int clack_avf_snapshot(void *p, uint8_t *dst, size_t cap, int *w, int *h);
void clack_avf_respond(void *p, uint64_t req, const void *bytes, int n);
void clack_avf_finish(void *p, uint64_t req, int failed);
void clack_avf_counts(void *p, int64_t *pulls, int64_t *frames, uint32_t *w, uint32_t *h);
int clack_avf_watching(void *p); // the state watch timer runs (tests)
// tests: players made while the history is on keep one; debug is it and
// AVPlayer's state now, malloc'd (free it)
void clack_avf_history(int on);
char *clack_avf_debug(void *p);
void clack_avf_close(void *p);
void clack_avf_live(int32_t *out);
void clack_avf_run_main_loop(void);
void clack_avf_stop_main_loop(void);

#endif
