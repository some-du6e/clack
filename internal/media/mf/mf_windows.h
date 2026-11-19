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
	uint32_t flags;  // the engine's device's creation flags
	int32_t videoHR; // why a device with video support wasn't made
} clackMFInfo;

// The counts of live objects (clack_me_live), for tests: all back to 0
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

// clack_me_open's flags: MF_MEDIA_ENGINE_FORCEMUTE, an engine with no
// audio renderer (no audio output to render to).
enum { CLACK_MF_FORCEMUTE = 0x4 };

// The controls (clack_me_control).
enum { CLACK_MF_PLAY = 1, CLACK_MF_PAUSE, CLACK_MF_SEEK, CLACK_MF_VOLUME, CLACK_MF_MUTED, CLACK_MF_LOOP, CLACK_MF_RATE };

int32_t clack_me_startup(void);
void clack_me_thread_init(void);
void clack_me_thread_done(void);
clackMF *clack_me_new(uintptr_t h);
uintptr_t clack_me_gio_ref(clackMF *m);
void clack_me_dev_release(uintptr_t dev);
int32_t clack_me_bind(clackMF *m, uintptr_t gio, clackMFInfo *info, uintptr_t *newDev);
void clack_me_bind_set(clackMF *m, uintptr_t dev);
int clack_me_snapshot(clackMF *m, uint8_t *dst, size_t cap, uint32_t w, uint32_t h);
int32_t clack_me_open(clackMF *m, int64_t total, int seekable, const uint16_t *url, uint32_t format, uint32_t flags, uint32_t gen, clackMFInfo *info);
void clack_me_close_engine(clackMF *m);
int32_t clack_me_ring(clackMF *m, uint32_t w, uint32_t h, clackMFInfo *info);
int32_t clack_me_control(clackMF *m, int op, double v);
void clack_me_state(clackMF *m, clackMFState *st);
int clack_me_tick(clackMF *m, int force, int32_t *hr, int64_t *pts);
uintptr_t clack_me_ask(clackMF *m, uintptr_t dev, uint64_t *key, int *status, clackMFInfo *info);
void clack_me_counts(clackMF *m, int64_t *skips, int64_t *timeouts, int64_t *copies);
void clack_me_close(clackMF *m);
void clack_me_free(clackMF *m);
void clack_me_read_done(void *req, int32_t n, int32_t hr);
void clack_me_live(int32_t *out);
int32_t clack_me_audio_endpoint(void);
int clack_me_gio_format_ok(uintptr_t dev, uint32_t format);

#endif
