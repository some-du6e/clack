// Clack's video player on Windows: Media Foundation's Media Engine in
// frame-server mode on a Direct3D 11 device of its own, on the adapter
// Gio draws with; frames go to Gio through a ring of shared textures
// (keyed mutexes), copied into a texture of Gio's device (mf_windows.c).
#ifndef CLACK_MF_H
#define CLACK_MF_H

#include <stdint.h>

typedef struct clackMF clackMF;

// clackMFState is the engine's state, as HTMLMediaElement's attributes.
typedef struct {
	double pos, dur, fps;
	int paused, ended, seeking, ready, err;
	int32_t errExt;
	uint32_t w, h;
	int hasVideo, hasAudio;
} clackMFState;

// clackMFInfo is what a step made, or which step failed and how.
typedef struct {
	char adapter[256];
	uint32_t vendor, device, featureLevel;
	int nt;          // the ring shares NT handles (1) or legacy ones (0)
	uint32_t format; // the ring's DXGI format
	int32_t hr;      // the failing call's HRESULT
	char step[64];   // the failing call
} clackMFInfo;

// The counts of live objects (clack_mf_live), for tests: all back to 0
// once every player closed.
enum {
	CLACK_MF_LIVE_PLAYERS,
	CLACK_MF_LIVE_STREAMS,
	CLACK_MF_LIVE_NOTIFY,
	CLACK_MF_LIVE_ENGINES,
	CLACK_MF_LIVE_DEVICES,
	CLACK_MF_LIVE_TEXTURES,
	CLACK_MF_LIVE_READS,
	CLACK_MF_LIVE_N
};

// The controls (clack_mf_control).
enum { CLACK_MF_PLAY = 1, CLACK_MF_PAUSE, CLACK_MF_SEEK, CLACK_MF_VOLUME, CLACK_MF_MUTED, CLACK_MF_LOOP, CLACK_MF_RATE };

int32_t clack_mf_startup(void);
void clack_mf_thread_init(void);
void clack_mf_thread_done(void);
clackMF *clack_mf_new(uintptr_t h);
int32_t clack_mf_bind(clackMF *m, clackMFInfo *info);
int32_t clack_mf_open(clackMF *m, int64_t total, int seekable, const uint16_t *url, uint32_t format, clackMFInfo *info);
void clack_mf_close_engine(clackMF *m);
int32_t clack_mf_ring(clackMF *m, uint32_t w, uint32_t h, clackMFInfo *info);
int32_t clack_mf_control(clackMF *m, int op, double v);
void clack_mf_state(clackMF *m, clackMFState *st);
int clack_mf_tick(clackMF *m, int32_t *hr);
uintptr_t clack_mf_ask(clackMF *m, uintptr_t dev, uint64_t *key, int *status, clackMFInfo *info);
void clack_mf_counts(clackMF *m, int64_t *skips, int64_t *timeouts, int64_t *copies);
void clack_mf_close(clackMF *m);
void clack_mf_free(clackMF *m);
void clack_mf_read_done(void *req, int32_t n, int32_t hr);
void clack_mf_live(int32_t *out);
int32_t clack_mf_audio_endpoint(void);
int clack_mf_gio_format_ok(uintptr_t dev, uint32_t format);

#endif
