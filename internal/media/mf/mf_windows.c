//go:build windows && cgo

// Media Foundation's Media Engine (Windows' HTML5 media element) playing a
// file Go reads, in frame-server mode on a Direct3D 11 device of its own,
// created on the adapter Gio draws with (Microsoft's frame-server sample
// does the same "to avoid threading issues"). Gio's device keeps its flags
// and its single thread. Each new frame is transferred into one of three
// shared textures (keyed mutexes: 0 is free for the engine, 1 holds a
// frame for Gio); Gio's ask copies the newest into a texture of its own
// device, of an _SRGB format, which it draws (Gio patch 51). Neither side
// ever waits for the other: a busy texture skips a frame, a frame Gio
// can't take now is taken at its next ask.
//
// The engine never gets a URL: it reads through clackStream, an
// IMFByteStream whose reads Go serves from memory (media.RangeSource over
// slack.Client.DownloadRange).
//
// mingw-w64 11 (Ubuntu's, the release's cross compiler) has no
// mfmediaengine.h: the Media Engine's interfaces are declared here, in
// the order of mfmediaengine.idl, and every GUID is defined here so
// nothing depends on a uuid library's exports.

#define COBJMACROS
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0A00
#endif
#ifndef WINVER
#define WINVER 0x0A00
#endif
#include <windows.h>
#include <d3d11_1.h>
#include <dxgi1_2.h>
#include <mfapi.h>
#include <mferror.h>
#include <mfidl.h>
#include <mmdeviceapi.h>
#include <oleauto.h>
#include <propidl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

#include "_cgo_export.h"
#include "mf_windows.h"

// GUIDs (mfmediaengine.idl, d3d11.h, dxgi.h, dxgi1_2.h, d3d10.h, mfapi.h,
// mfobjects.h, mmdeviceapi.h).
static const GUID clackCLSID_MFMediaEngineClassFactory = {0xb44392da, 0x499b, 0x446b, {0xa4, 0xcb, 0x00, 0x5f, 0xea, 0xd0, 0xe6, 0xd5}};
static const GUID clackIID_IMFMediaEngineClassFactory = {0x4d645ace, 0x26aa, 0x4688, {0x9b, 0xe1, 0xdf, 0x35, 0x16, 0x99, 0x0b, 0x93}};
static const GUID clackIID_IMFMediaEngineEx = {0x83015ead, 0xb1e6, 0x40d0, {0xa9, 0x8a, 0x37, 0x14, 0x5f, 0xfe, 0x1a, 0xd1}};
static const GUID clackIID_IMFMediaEngineNotify = {0xfee7c112, 0xe776, 0x42b5, {0x9b, 0xbf, 0x00, 0x48, 0x52, 0x4e, 0x2b, 0xd5}};
static const GUID clackMF_MEDIA_ENGINE_CALLBACK = {0xc60381b8, 0x83a4, 0x41f8, {0xa3, 0xd0, 0xde, 0x05, 0x07, 0x68, 0x49, 0xa9}};
static const GUID clackMF_MEDIA_ENGINE_DXGI_MANAGER = {0x065702da, 0x1094, 0x486d, {0x86, 0x17, 0xee, 0x7c, 0xc4, 0xee, 0x46, 0x48}};
static const GUID clackMF_MEDIA_ENGINE_VIDEO_OUTPUT_FORMAT = {0x5066893c, 0x8cf9, 0x42bc, {0x8b, 0x8a, 0x47, 0x22, 0x12, 0xe5, 0x27, 0x26}};
static const GUID clackIID_IUnknown = {0x00000000, 0x0000, 0x0000, {0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}};
static const GUID clackIID_IMFByteStream = {0xad4c1b00, 0x4bf7, 0x422f, {0x91, 0x75, 0x75, 0x66, 0x93, 0xd9, 0x13, 0x0d}};
static const GUID clackIID_ID3D11Device1 = {0xa04bfb29, 0x08ef, 0x43d6, {0xa4, 0x9c, 0xa9, 0xbd, 0xbd, 0xcb, 0xe6, 0x86}};
static const GUID clackIID_IDXGIDevice = {0x54ec77fa, 0x1377, 0x44e6, {0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}};
static const GUID clackIID_IDXGIResource = {0x035f3ab4, 0x482e, 0x4e50, {0xb4, 0x1f, 0x8a, 0x7f, 0x8b, 0xd8, 0x96, 0x0b}};
static const GUID clackIID_IDXGIResource1 = {0x30961379, 0x4609, 0x4a41, {0x99, 0x8e, 0x54, 0xfe, 0x56, 0x7e, 0xe0, 0xc1}};
static const GUID clackIID_IDXGIKeyedMutex = {0x9d8e1289, 0xd7b3, 0x465f, {0x81, 0x26, 0x25, 0x0e, 0x34, 0x9a, 0xf8, 0x5d}};
static const GUID clackIID_ID3D11Texture2D = {0x6f15aaf2, 0xd208, 0x4e89, {0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}};
static const GUID clackIID_ID3D10Multithread = {0x9b7e4e00, 0x342c, 0x4106, {0xa1, 0x9f, 0x4f, 0x27, 0x04, 0xf6, 0x89, 0xf0}};
static const GUID clackIID_IMMDeviceEnumerator = {0xa95664d2, 0x9614, 0x4f35, {0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6}};
static const GUID clackCLSID_MMDeviceEnumerator = {0xbcde0395, 0xe52f, 0x467c, {0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e}};
static const GUID clackMF_MT_MAJOR_TYPE = {0x48eba18e, 0xf8c9, 0x4687, {0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}};
static const GUID clackMF_MT_FRAME_RATE = {0xc459a2e8, 0x3d2c, 0x4e44, {0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0}};
static const GUID clackMFMediaType_Video = {0x73646976, 0x0000, 0x0010, {0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}};

// The Media Engine's interfaces (mfmediaengine.idl), as far as Clack calls
// them: IMFMediaEngineEx's methods after GetStreamAttribute are left out.
typedef struct {
	float left, top, right, bottom;
} clackNormRect;
typedef struct {
	BYTE b, g, r, a;
} clackARGB;

typedef struct clackError clackError;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackError *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackError *);
	ULONG(STDMETHODCALLTYPE *Release)(clackError *);
	USHORT(STDMETHODCALLTYPE *GetErrorCode)(clackError *);
	HRESULT(STDMETHODCALLTYPE *GetExtendedErrorCode)(clackError *);
} clackErrorVtbl;
struct clackError {
	const clackErrorVtbl *lpVtbl;
};

typedef struct clackEngine clackEngine;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackEngine *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackEngine *);
	ULONG(STDMETHODCALLTYPE *Release)(clackEngine *);
	// IMFMediaEngine
	HRESULT(STDMETHODCALLTYPE *GetError)(clackEngine *, clackError **);
	HRESULT(STDMETHODCALLTYPE *SetErrorCode)(clackEngine *, int);
	HRESULT(STDMETHODCALLTYPE *SetSourceElements)(clackEngine *, void *);
	HRESULT(STDMETHODCALLTYPE *SetSrcURL)(clackEngine *, BSTR); // never called: no URL source
	HRESULT(STDMETHODCALLTYPE *GetCurrentSource)(clackEngine *, BSTR *);
	USHORT(STDMETHODCALLTYPE *GetNetworkState)(clackEngine *);
	int(STDMETHODCALLTYPE *GetPreload)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetPreload)(clackEngine *, int);
	HRESULT(STDMETHODCALLTYPE *GetBuffered)(clackEngine *, void **);
	HRESULT(STDMETHODCALLTYPE *Load)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *CanPlayType)(clackEngine *, BSTR, int *);
	USHORT(STDMETHODCALLTYPE *GetReadyState)(clackEngine *);
	BOOL(STDMETHODCALLTYPE *IsSeeking)(clackEngine *);
	double(STDMETHODCALLTYPE *CurTime)(clackEngine *); // GetCurrentTime (a windows.h macro)
	HRESULT(STDMETHODCALLTYPE *SetCurrentTime)(clackEngine *, double);
	double(STDMETHODCALLTYPE *GetStartTime)(clackEngine *);
	double(STDMETHODCALLTYPE *GetDuration)(clackEngine *);
	BOOL(STDMETHODCALLTYPE *IsPaused)(clackEngine *);
	double(STDMETHODCALLTYPE *GetDefaultPlaybackRate)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetDefaultPlaybackRate)(clackEngine *, double);
	double(STDMETHODCALLTYPE *GetPlaybackRate)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetPlaybackRate)(clackEngine *, double);
	HRESULT(STDMETHODCALLTYPE *GetPlayed)(clackEngine *, void **);
	HRESULT(STDMETHODCALLTYPE *GetSeekable)(clackEngine *, void **);
	BOOL(STDMETHODCALLTYPE *IsEnded)(clackEngine *);
	BOOL(STDMETHODCALLTYPE *GetAutoPlay)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetAutoPlay)(clackEngine *, BOOL);
	BOOL(STDMETHODCALLTYPE *GetLoop)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetLoop)(clackEngine *, BOOL);
	HRESULT(STDMETHODCALLTYPE *Play)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *Pause)(clackEngine *);
	BOOL(STDMETHODCALLTYPE *GetMuted)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetMuted)(clackEngine *, BOOL);
	double(STDMETHODCALLTYPE *GetVolume)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetVolume)(clackEngine *, double);
	BOOL(STDMETHODCALLTYPE *HasVideo)(clackEngine *);
	BOOL(STDMETHODCALLTYPE *HasAudio)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *GetNativeVideoSize)(clackEngine *, DWORD *, DWORD *);
	HRESULT(STDMETHODCALLTYPE *GetVideoAspectRatio)(clackEngine *, DWORD *, DWORD *);
	HRESULT(STDMETHODCALLTYPE *Shutdown)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *TransferVideoFrame)(clackEngine *, IUnknown *, const clackNormRect *, const RECT *, const clackARGB *);
	HRESULT(STDMETHODCALLTYPE *OnVideoStreamTick)(clackEngine *, LONGLONG *);
	// IMFMediaEngineEx
	HRESULT(STDMETHODCALLTYPE *SetSourceFromByteStream)(clackEngine *, IMFByteStream *, BSTR);
	HRESULT(STDMETHODCALLTYPE *GetStatistics)(clackEngine *, int, PROPVARIANT *);
	HRESULT(STDMETHODCALLTYPE *UpdateVideoStream)(clackEngine *, const clackNormRect *, const RECT *, const clackARGB *);
	double(STDMETHODCALLTYPE *GetBalance)(clackEngine *);
	HRESULT(STDMETHODCALLTYPE *SetBalance)(clackEngine *, double);
	BOOL(STDMETHODCALLTYPE *IsPlaybackRateSupported)(clackEngine *, double);
	HRESULT(STDMETHODCALLTYPE *FrameStep)(clackEngine *, BOOL);
	HRESULT(STDMETHODCALLTYPE *GetResourceCharacteristics)(clackEngine *, DWORD *);
	HRESULT(STDMETHODCALLTYPE *GetPresentationAttribute)(clackEngine *, REFGUID, PROPVARIANT *);
	HRESULT(STDMETHODCALLTYPE *GetNumberOfStreams)(clackEngine *, DWORD *);
	HRESULT(STDMETHODCALLTYPE *GetStreamAttribute)(clackEngine *, DWORD, REFGUID, PROPVARIANT *);
} clackEngineVtbl;
struct clackEngine {
	const clackEngineVtbl *lpVtbl;
};

typedef struct clackFactory clackFactory;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackFactory *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackFactory *);
	ULONG(STDMETHODCALLTYPE *Release)(clackFactory *);
	HRESULT(STDMETHODCALLTYPE *CreateInstance)(clackFactory *, DWORD, IMFAttributes *, clackEngine **);
} clackFactoryVtbl;
struct clackFactory {
	const clackFactoryVtbl *lpVtbl;
};

// ID3D10Multithread (d3d10.h, declared here rather than mixing d3d10.h's
// types into d3d11's).
typedef struct clackMT clackMT;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackMT *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackMT *);
	ULONG(STDMETHODCALLTYPE *Release)(clackMT *);
	void(STDMETHODCALLTYPE *Enter)(clackMT *);
	void(STDMETHODCALLTYPE *Leave)(clackMT *);
	BOOL(STDMETHODCALLTYPE *SetMultithreadProtected)(clackMT *, BOOL);
	BOOL(STDMETHODCALLTYPE *GetMultithreadProtected)(clackMT *);
} clackMTVtbl;
struct clackMT {
	const clackMTVtbl *lpVtbl;
};

// mfplat.dll, loaded once (it is missing from Windows N without the Media
// Feature Pack: then nothing plays, and the click opens the file's page).
typedef HRESULT(WINAPI *pMFStartup)(ULONG, DWORD);
typedef HRESULT(WINAPI *pMFCreateDXGIDeviceManager)(UINT *, IMFDXGIDeviceManager **);
typedef HRESULT(WINAPI *pMFCreateAttributes)(IMFAttributes **, UINT32);
typedef HRESULT(WINAPI *pMFCreateAsyncResult)(IUnknown *, IMFAsyncCallback *, IUnknown *, IMFAsyncResult **);
typedef HRESULT(WINAPI *pMFInvokeCallback)(IMFAsyncResult *);
static struct {
	pMFStartup Startup;
	pMFCreateDXGIDeviceManager CreateDXGIDeviceManager;
	pMFCreateAttributes CreateAttributes;
	pMFCreateAsyncResult CreateAsyncResult;
	pMFInvokeCallback InvokeCallback;
} mf;
static INIT_ONCE startOnce = INIT_ONCE_STATIC_INIT;
static HRESULT startHR = E_FAIL;

static volatile LONG live[CLACK_MF_LIVE_N];

static BOOL CALLBACK startup(PINIT_ONCE once, PVOID param, PVOID *ctx) {
	(void)once;
	(void)param;
	(void)ctx;
	HMODULE m = LoadLibraryW(L"mfplat.dll");
	if (m == NULL) {
		startHR = HRESULT_FROM_WIN32(GetLastError());
		return TRUE;
	}
	mf.Startup = (pMFStartup)(void *)GetProcAddress(m, "MFStartup");
	mf.CreateDXGIDeviceManager = (pMFCreateDXGIDeviceManager)(void *)GetProcAddress(m, "MFCreateDXGIDeviceManager");
	mf.CreateAttributes = (pMFCreateAttributes)(void *)GetProcAddress(m, "MFCreateAttributes");
	mf.CreateAsyncResult = (pMFCreateAsyncResult)(void *)GetProcAddress(m, "MFCreateAsyncResult");
	mf.InvokeCallback = (pMFInvokeCallback)(void *)GetProcAddress(m, "MFInvokeCallback");
	if (!mf.Startup || !mf.CreateDXGIDeviceManager || !mf.CreateAttributes || !mf.CreateAsyncResult || !mf.InvokeCallback) {
		startHR = E_NOINTERFACE;
		return TRUE;
	}
	startHR = mf.Startup(MF_VERSION, MFSTARTUP_LITE);
	return TRUE;
}

int32_t clack_me_startup(void) {
	InitOnceExecuteOnce(&startOnce, startup, NULL, NULL);
	return (int32_t)startHR;
}

void clack_me_thread_init(void) { CoInitializeEx(NULL, COINIT_MULTITHREADED); }
void clack_me_thread_done(void) { CoUninitialize(); }

void clack_me_live(int32_t *out) {
	for (int i = 0; i < CLACK_MF_LIVE_N; i++) {
		out[i] = (int32_t)InterlockedCompareExchange(&live[i], 0, 0);
	}
}

static void fail(clackMFInfo *info, const char *step, HRESULT hr) {
	if (info != NULL) {
		info->hr = (int32_t)hr;
		snprintf(info->step, sizeof info->step, "%s", step);
	}
}

#define RELEASE(T, p)                                                                                                                      \
	do {                                                                                                                                   \
		if ((p) != NULL) {                                                                                                                 \
			T##_Release(p);                                                                                                                \
			(p) = NULL;                                                                                                                    \
		}                                                                                                                                  \
	} while (0)

// ---- the byte stream ----

// clackDone is a finished read whose EndRead hasn't come yet.
typedef struct clackDone {
	IMFAsyncResult *res;
	ULONG n;
	struct clackDone *next;
} clackDone;

typedef struct clackStream {
	IMFByteStream iface;
	volatile LONG ref;
	uintptr_t h;
	CRITICAL_SECTION cs;
	QWORD pos, len;
	DWORD caps;
	clackDone *done;
} clackStream;

typedef struct clackRead {
	IMFAsyncResult *res;
	clackStream *s;
	QWORD start;
	ULONG cb;
} clackRead;

static clackStream *streamOf(IMFByteStream *b) { return (clackStream *)b; }

static HRESULT STDMETHODCALLTYPE sQI(IMFByteStream *b, REFIID iid, void **out) {
	if (out == NULL) {
		return E_POINTER;
	}
	if (IsEqualGUID(iid, &clackIID_IUnknown) || IsEqualGUID(iid, &clackIID_IMFByteStream)) {
		*out = b;
		InterlockedIncrement(&streamOf(b)->ref);
		return S_OK;
	}
	*out = NULL;
	return E_NOINTERFACE;
}

static ULONG STDMETHODCALLTYPE sAddRef(IMFByteStream *b) { return (ULONG)InterlockedIncrement(&streamOf(b)->ref); }

static ULONG STDMETHODCALLTYPE sRelease(IMFByteStream *b) {
	clackStream *s = streamOf(b);
	LONG n = InterlockedDecrement(&s->ref);
	if (n == 0) {
		for (clackDone *d = s->done; d != NULL;) {
			clackDone *next = d->next;
			IMFAsyncResult_Release(d->res);
			free(d);
			d = next;
		}
		DeleteCriticalSection(&s->cs);
		free(s);
		InterlockedDecrement(&live[CLACK_MF_LIVE_STREAMS]);
	}
	return (ULONG)n;
}

static HRESULT STDMETHODCALLTYPE sGetCapabilities(IMFByteStream *b, DWORD *caps) {
	*caps = streamOf(b)->caps;
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sGetLength(IMFByteStream *b, QWORD *n) {
	*n = streamOf(b)->len;
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sSetLength(IMFByteStream *b, QWORD n) {
	(void)b;
	(void)n;
	return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE sGetCurrentPosition(IMFByteStream *b, QWORD *pos) {
	clackStream *s = streamOf(b);
	EnterCriticalSection(&s->cs);
	*pos = s->pos;
	LeaveCriticalSection(&s->cs);
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sSetCurrentPosition(IMFByteStream *b, QWORD pos) {
	clackStream *s = streamOf(b);
	if (pos > s->len) {
		return E_INVALIDARG;
	}
	EnterCriticalSection(&s->cs);
	s->pos = pos;
	LeaveCriticalSection(&s->cs);
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sIsEndOfStream(IMFByteStream *b, BOOL *end) {
	clackStream *s = streamOf(b);
	EnterCriticalSection(&s->cs);
	*end = s->pos >= s->len;
	LeaveCriticalSection(&s->cs);
	return S_OK;
}

// Read blocks until Go has the bytes (a thread of Media Foundation's).
static HRESULT STDMETHODCALLTYPE sRead(IMFByteStream *b, BYTE *pb, ULONG cb, ULONG *got) {
	clackStream *s = streamOf(b);
	EnterCriticalSection(&s->cs);
	QWORD start = s->pos;
	LeaveCriticalSection(&s->cs);
	long long n = clackMFReadSync(s->h, pb, cb, (long long)start);
	if (n < 0) {
		*got = 0;
		return E_FAIL;
	}
	EnterCriticalSection(&s->cs);
	if (s->pos == start) {
		s->pos = start + (QWORD)n;
	}
	LeaveCriticalSection(&s->cs);
	*got = (ULONG)n;
	return S_OK;
}

// BeginRead hands the read to Go, which fills pb from memory (or once the
// GET brings it) and calls clack_me_read_done. The position moves on by
// cb now, so a read begun right after reads on from there; a short read
// moves it back to where it ended.
static HRESULT STDMETHODCALLTYPE sBeginRead(IMFByteStream *b, BYTE *pb, ULONG cb, IMFAsyncCallback *cbk, IUnknown *state) {
	clackStream *s = streamOf(b);
	if (cbk == NULL || pb == NULL) {
		return E_POINTER;
	}
	IMFAsyncResult *res = NULL;
	HRESULT hr = mf.CreateAsyncResult(NULL, cbk, state, &res);
	if (FAILED(hr)) {
		return hr;
	}
	clackRead *r = calloc(1, sizeof *r);
	if (r == NULL) {
		IMFAsyncResult_Release(res);
		return E_OUTOFMEMORY;
	}
	EnterCriticalSection(&s->cs);
	r->start = s->pos;
	QWORD end = s->pos + cb;
	s->pos = end > s->len ? s->len : end;
	LeaveCriticalSection(&s->cs);
	r->res = res;
	r->s = s;
	r->cb = cb;
	InterlockedIncrement(&s->ref);
	InterlockedIncrement(&live[CLACK_MF_LIVE_READS]);
	clackMFRead(s->h, r, pb, cb, (long long)r->start);
	return S_OK;
}

void clack_me_read_done(void *req, int32_t n, int32_t hr) {
	clackRead *r = req;
	clackStream *s = r->s;
	if (n < 0 || FAILED(hr)) {
		n = 0;
	}
	clackDone *d = calloc(1, sizeof *d);
	EnterCriticalSection(&s->cs);
	QWORD want = r->start + r->cb;
	if (want > s->len) {
		want = s->len;
	}
	if (s->pos == want) {
		s->pos = r->start + (QWORD)n;
	}
	if (d != NULL) {
		d->res = r->res;
		IMFAsyncResult_AddRef(d->res);
		d->n = (ULONG)n;
		d->next = s->done;
		s->done = d;
	}
	LeaveCriticalSection(&s->cs);
	IMFAsyncResult_SetStatus(r->res, (HRESULT)hr);
	mf.InvokeCallback(r->res);
	IMFAsyncResult_Release(r->res);
	sRelease(&s->iface);
	free(r);
	InterlockedDecrement(&live[CLACK_MF_LIVE_READS]);
}

static HRESULT STDMETHODCALLTYPE sEndRead(IMFByteStream *b, IMFAsyncResult *res, ULONG *got) {
	clackStream *s = streamOf(b);
	if (res == NULL || got == NULL) {
		return E_POINTER;
	}
	clackDone *found = NULL;
	EnterCriticalSection(&s->cs);
	for (clackDone **p = &s->done; *p != NULL; p = &(*p)->next) {
		if ((*p)->res == res) {
			found = *p;
			*p = found->next;
			break;
		}
	}
	LeaveCriticalSection(&s->cs);
	if (found == NULL) {
		*got = 0;
		return E_INVALIDARG;
	}
	*got = found->n;
	HRESULT hr = IMFAsyncResult_GetStatus(res);
	IMFAsyncResult_Release(found->res);
	free(found);
	return hr;
}

static HRESULT STDMETHODCALLTYPE sWrite(IMFByteStream *b, const BYTE *pb, ULONG cb, ULONG *n) {
	(void)b;
	(void)pb;
	(void)cb;
	(void)n;
	return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE sBeginWrite(IMFByteStream *b, const BYTE *pb, ULONG cb, IMFAsyncCallback *c, IUnknown *st) {
	(void)b;
	(void)pb;
	(void)cb;
	(void)c;
	(void)st;
	return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE sEndWrite(IMFByteStream *b, IMFAsyncResult *r, ULONG *n) {
	(void)b;
	(void)r;
	(void)n;
	return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE sSeek(IMFByteStream *b, MFBYTESTREAM_SEEK_ORIGIN origin, LONGLONG off, DWORD flags, QWORD *cur) {
	(void)flags;
	clackStream *s = streamOf(b);
	EnterCriticalSection(&s->cs);
	LONGLONG to = origin == msoBegin ? off : (LONGLONG)s->pos + off;
	if (to < 0) {
		to = 0;
	}
	if ((QWORD)to > s->len) {
		to = (LONGLONG)s->len;
	}
	s->pos = (QWORD)to;
	if (cur != NULL) {
		*cur = s->pos;
	}
	LeaveCriticalSection(&s->cs);
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sFlush(IMFByteStream *b) {
	(void)b;
	return S_OK;
}

static HRESULT STDMETHODCALLTYPE sClose(IMFByteStream *b) {
	(void)b;
	return S_OK;
}

static IMFByteStreamVtbl streamVtbl = {sQI,           sAddRef, sRelease,  sGetCapabilities, sGetLength, sSetLength,
                                       sGetCurrentPosition, sSetCurrentPosition, sIsEndOfStream, sRead, sBeginRead, sEndRead,
                                       sWrite,        sBeginWrite, sEndWrite, sSeek,            sFlush,     sClose};

static clackStream *newStream(uintptr_t h, QWORD len, int seekable) {
	clackStream *s = calloc(1, sizeof *s);
	if (s == NULL) {
		return NULL;
	}
	s->iface.lpVtbl = &streamVtbl;
	s->ref = 1;
	s->h = h;
	s->len = len;
	s->caps = MFBYTESTREAM_IS_READABLE | (seekable ? MFBYTESTREAM_IS_SEEKABLE : 0);
	InitializeCriticalSection(&s->cs);
	InterlockedIncrement(&live[CLACK_MF_LIVE_STREAMS]);
	return s;
}

// ---- the engine's events ----

typedef struct clackNotify clackNotify;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackNotify *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackNotify *);
	ULONG(STDMETHODCALLTYPE *Release)(clackNotify *);
	HRESULT(STDMETHODCALLTYPE *EventNotify)(clackNotify *, DWORD, DWORD_PTR, DWORD);
} clackNotifyVtbl;
struct clackNotify {
	const clackNotifyVtbl *lpVtbl;
	volatile LONG ref;
	uintptr_t h;
	uint32_t gen; // the engine's (Go's count of the player's engines)
};

static HRESULT STDMETHODCALLTYPE nQI(clackNotify *n, REFIID iid, void **out) {
	if (out == NULL) {
		return E_POINTER;
	}
	if (IsEqualGUID(iid, &clackIID_IUnknown) || IsEqualGUID(iid, &clackIID_IMFMediaEngineNotify)) {
		*out = n;
		InterlockedIncrement(&n->ref);
		return S_OK;
	}
	*out = NULL;
	return E_NOINTERFACE;
}

static ULONG STDMETHODCALLTYPE nAddRef(clackNotify *n) { return (ULONG)InterlockedIncrement(&n->ref); }

static ULONG STDMETHODCALLTYPE nRelease(clackNotify *n) {
	LONG r = InterlockedDecrement(&n->ref);
	if (r == 0) {
		free(n);
		InterlockedDecrement(&live[CLACK_MF_LIVE_NOTIFY]);
	}
	return (ULONG)r;
}

// EventNotify comes on a Media Foundation thread: Go only queues it,
// with the engine's generation (an engine made again for another format
// still has events queued from the one before it).
// MF_MEDIA_ENGINE_EVENT_TIMEUPDATE (18) isn't passed on: the manager
// times its own, as Chromium does.
static HRESULT STDMETHODCALLTYPE nEventNotify(clackNotify *n, DWORD ev, DWORD_PTR p1, DWORD p2) {
	if (ev != 18) {
		clackMFEvent(n->h, n->gen, (unsigned)ev, (uintptr_t)p1, (unsigned)p2);
	}
	return S_OK;
}

static const clackNotifyVtbl notifyVtbl = {nQI, nAddRef, nRelease, nEventNotify};

// ---- the player ----

#define SLOTS 3

struct clackMF {
	uintptr_t h;
	// the media thread's
	clackEngine *engine;
	clackNotify *notify;
	clackStream *stream;
	ID3D11Device *dev; // the engine's device
	IMFDXGIDeviceManager *mgr;
	UINT token;
	LUID adapter;
	LUID nextAdapter; // clack_me_bind's new device's, until clack_me_bind_set
	uint32_t format;
	struct {
		ID3D11Texture2D *tex;
		IDXGIKeyedMutex *mx;
		HANDLE handle;
	} slot[SLOTS];
	int nt; // the slots' handles are NT handles
	uint32_t w, h_;
	volatile LONG gen;
	int newest;
	LONG64 seq;
	volatile LONG64 published; // seq << 2 | slot; 0 none
	double fps;
	int fpsRead;
	// Gio's side (the ask), under the Go player's lock
	ID3D11Device *gioDev;
	ID3D11Device1 *gioDev1;
	ID3D11DeviceContext *gioCtx;
	LONG askGen;
	struct {
		ID3D11Texture2D *tex;
		IDXGIKeyedMutex *mx;
	} open[SLOTS];
	ID3D11Texture2D *local;
	ID3D11ShaderResourceView *srv;
	LONG64 copied;
	// counts
	volatile LONG64 skips, timeouts, copies;
};

clackMF *clack_me_new(uintptr_t h) {
	clackMF *m = calloc(1, sizeof *m);
	if (m == NULL) {
		return NULL;
	}
	m->h = h;
	m->newest = -1;
	m->askGen = -1;
	InterlockedIncrement(&live[CLACK_MF_LIVE_PLAYERS]);
	return m;
}

static void releaseGio(clackMF *m) {
	for (int i = 0; i < SLOTS; i++) {
		RELEASE(IDXGIKeyedMutex, m->open[i].mx);
		if (m->open[i].tex != NULL) {
			ID3D11Texture2D_Release(m->open[i].tex);
			m->open[i].tex = NULL;
			InterlockedDecrement(&live[CLACK_MF_LIVE_TEXTURES]);
		}
	}
	RELEASE(ID3D11ShaderResourceView, m->srv);
	if (m->local != NULL) {
		ID3D11Texture2D_Release(m->local);
		m->local = NULL;
		InterlockedDecrement(&live[CLACK_MF_LIVE_TEXTURES]);
	}
	m->askGen = -1;
	m->copied = 0;
}

static void releaseRing(clackMF *m) {
	for (int i = 0; i < SLOTS; i++) {
		RELEASE(IDXGIKeyedMutex, m->slot[i].mx);
		if (m->slot[i].tex != NULL) {
			ID3D11Texture2D_Release(m->slot[i].tex);
			m->slot[i].tex = NULL;
			InterlockedDecrement(&live[CLACK_MF_LIVE_TEXTURES]);
		}
		if (m->slot[i].handle != NULL && m->nt) {
			CloseHandle(m->slot[i].handle); // legacy shared handles aren't handles
		}
		m->slot[i].handle = NULL;
	}
	InterlockedExchange64(&m->published, 0);
	m->newest = -1;
	InterlockedIncrement(&m->gen);
}

// clack_me_gio_ref is the device Gio last asked with (the ask records
// it), with a reference the caller lets go of (clack_me_dev_release); 0
// before any ask. Under askMu.
uintptr_t clack_me_gio_ref(clackMF *m) {
	if (m->gioDev == NULL) {
		return 0;
	}
	ID3D11Device_AddRef(m->gioDev);
	return (uintptr_t)m->gioDev;
}

void clack_me_dev_release(uintptr_t devp) {
	if (devp != 0) {
		ID3D11Device_Release((ID3D11Device *)devp);
	}
}

// clack_me_bind makes the engine's device on the adapter of giop, the
// device Gio last asked with (clack_me_gio_ref), and moves the engine's
// manager to it. It touches nothing the ask reads, so it runs without
// askMu: D3D11CreateDevice and the manager take tens of milliseconds on a
// real GPU, which Gio's render thread would wait out. *newDev is the new
// device for clack_me_bind_set, or 0 when the engine's device is already
// on that adapter.
int32_t clack_me_bind(clackMF *m, uintptr_t giop, clackMFInfo *info, uintptr_t *newDev) {
	*newDev = 0;
	if (giop == 0) {
		fail(info, "no device asked yet", E_UNEXPECTED);
		return E_UNEXPECTED;
	}
	IDXGIDevice *xd = NULL;
	HRESULT hr = ID3D11Device_QueryInterface((ID3D11Device *)giop, &clackIID_IDXGIDevice, (void **)&xd);
	if (FAILED(hr)) {
		fail(info, "IDXGIDevice", hr);
		return hr;
	}
	IDXGIAdapter *ad = NULL;
	hr = IDXGIDevice_GetAdapter(xd, &ad);
	IDXGIDevice_Release(xd);
	if (FAILED(hr)) {
		fail(info, "GetAdapter", hr);
		return hr;
	}
	DXGI_ADAPTER_DESC desc;
	memset(&desc, 0, sizeof desc);
	IDXGIAdapter_GetDesc(ad, &desc);
	if (info != NULL) {
		WideCharToMultiByte(CP_UTF8, 0, desc.Description, -1, info->adapter, sizeof info->adapter - 1, NULL, NULL);
		info->vendor = desc.VendorId;
		info->device = desc.DeviceId;
	}
	if (m->dev != NULL && memcmp(&m->adapter, &desc.AdapterLuid, sizeof m->adapter) == 0) {
		IDXGIAdapter_Release(ad);
		if (info != NULL) {
			info->featureLevel = (uint32_t)ID3D11Device_GetFeatureLevel(m->dev);
		}
		return S_OK; // the same adapter: nothing to do
	}
	D3D_FEATURE_LEVEL levels[] = {D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0, D3D_FEATURE_LEVEL_10_1, D3D_FEATURE_LEVEL_10_0};
	D3D_FEATURE_LEVEL got = 0;
	ID3D11Device *dev = NULL;
	// video support first (hardware decoding); an adapter refusing it (a
	// runner's Basic Render Driver answers DXGI_ERROR_UNSUPPORTED, whatever
	// the docs say) gets a device without it, and Media Foundation decodes
	// in software on it
	UINT tries[] = {D3D11_CREATE_DEVICE_VIDEO_SUPPORT | D3D11_CREATE_DEVICE_BGRA_SUPPORT, D3D11_CREATE_DEVICE_BGRA_SUPPORT};
	UINT flags = 0;
	for (int i = 0; i < 2; i++) {
		flags = tries[i];
		hr = D3D11CreateDevice(ad, D3D_DRIVER_TYPE_UNKNOWN, NULL, flags, levels, 4, D3D11_SDK_VERSION, &dev, &got, NULL);
		if (hr == E_INVALIDARG) { // a runtime without 11.1
			hr = D3D11CreateDevice(ad, D3D_DRIVER_TYPE_UNKNOWN, NULL, flags, levels + 1, 3, D3D11_SDK_VERSION, &dev, &got, NULL);
		}
		if (SUCCEEDED(hr)) {
			break;
		}
		if (info != NULL && i == 0) {
			info->videoHR = (int32_t)hr;
		}
	}
	IDXGIAdapter_Release(ad);
	if (FAILED(hr)) {
		fail(info, "D3D11CreateDevice", hr);
		return hr;
	}
	InterlockedIncrement(&live[CLACK_MF_LIVE_DEVICES]);
	clackMT *mt = NULL;
	if (SUCCEEDED(ID3D11Device_QueryInterface(dev, &clackIID_ID3D10Multithread, (void **)&mt))) {
		mt->lpVtbl->SetMultithreadProtected(mt, TRUE);
		mt->lpVtbl->Release(mt);
	}
	if (m->mgr == NULL) {
		hr = mf.CreateDXGIDeviceManager(&m->token, &m->mgr);
		if (FAILED(hr)) {
			ID3D11Device_Release(dev);
			InterlockedDecrement(&live[CLACK_MF_LIVE_DEVICES]);
			fail(info, "MFCreateDXGIDeviceManager", hr);
			return hr;
		}
	}
	hr = IMFDXGIDeviceManager_ResetDevice(m->mgr, (IUnknown *)dev, m->token);
	if (FAILED(hr)) {
		ID3D11Device_Release(dev);
		InterlockedDecrement(&live[CLACK_MF_LIVE_DEVICES]);
		fail(info, "ResetDevice", hr);
		return hr;
	}
	m->nextAdapter = desc.AdapterLuid;
	*newDev = (uintptr_t)dev;
	if (info != NULL) {
		info->featureLevel = (uint32_t)got;
		info->flags = flags;
	}
	return S_OK;
}

// clack_me_bind_set makes devp (clack_me_bind's) the engine's device, under
// askMu: the old device's ring, which the ask reads, goes with it.
void clack_me_bind_set(clackMF *m, uintptr_t devp) {
	if (m->dev != NULL) {
		releaseRing(m);
		ID3D11Device_Release(m->dev);
		InterlockedDecrement(&live[CLACK_MF_LIVE_DEVICES]);
	}
	m->dev = (ID3D11Device *)devp;
	m->adapter = m->nextAdapter;
}

// clack_me_open makes the engine, reading the file through a byte stream
// of total bytes (seekable: the server answers ranges), and gives it its
// source. url is only the demuxer's hint (clack-media://<id>.<ext>).
int32_t clack_me_open(clackMF *m, int64_t total, int seekable, const uint16_t *url, uint32_t format, uint32_t flags, uint32_t gen, clackMFInfo *info) {
	if (m->mgr == NULL) {
		fail(info, "no device", E_UNEXPECTED);
		return E_UNEXPECTED;
	}
	if (wcsncmp((const wchar_t *)url, L"clack-media://", 14) != 0) {
		fail(info, "the source's URL isn't clack-media://", E_INVALIDARG);
		return E_INVALIDARG; // the engine is never given a URL it would fetch itself
	}
	clackNotify *n = calloc(1, sizeof *n);
	if (n == NULL) {
		return E_OUTOFMEMORY;
	}
	n->lpVtbl = &notifyVtbl;
	n->ref = 1;
	n->h = m->h;
	n->gen = gen;
	InterlockedIncrement(&live[CLACK_MF_LIVE_NOTIFY]);
	clackStream *s = newStream(m->h, (QWORD)total, seekable);
	if (s == NULL) {
		nRelease(n);
		return E_OUTOFMEMORY;
	}
	IMFAttributes *a = NULL;
	clackFactory *f = NULL;
	clackEngine *base = NULL, *e = NULL;
	BSTR b = NULL;
	HRESULT hr = mf.CreateAttributes(&a, 3);
	if (FAILED(hr)) {
		fail(info, "MFCreateAttributes", hr);
		goto out;
	}
	IMFAttributes_SetUnknown(a, &clackMF_MEDIA_ENGINE_CALLBACK, (IUnknown *)n);
	IMFAttributes_SetUnknown(a, &clackMF_MEDIA_ENGINE_DXGI_MANAGER, (IUnknown *)m->mgr);
	IMFAttributes_SetUINT32(a, &clackMF_MEDIA_ENGINE_VIDEO_OUTPUT_FORMAT, format);
	hr = CoCreateInstance(&clackCLSID_MFMediaEngineClassFactory, NULL, CLSCTX_INPROC_SERVER, &clackIID_IMFMediaEngineClassFactory, (void **)&f);
	if (FAILED(hr)) {
		fail(info, "CoCreateInstance(MFMediaEngineClassFactory)", hr);
		goto out;
	}
	hr = f->lpVtbl->CreateInstance(f, flags & CLACK_MF_FORCEMUTE, a, &base);
	if (FAILED(hr)) {
		fail(info, "IMFMediaEngineClassFactory::CreateInstance", hr);
		goto out;
	}
	hr = base->lpVtbl->QueryInterface(base, &clackIID_IMFMediaEngineEx, (void **)&e);
	base->lpVtbl->Release(base);
	if (FAILED(hr)) {
		fail(info, "IMFMediaEngineEx", hr);
		goto out;
	}
	InterlockedIncrement(&live[CLACK_MF_LIVE_ENGINES]);
	b = SysAllocString((const OLECHAR *)url);
	hr = e->lpVtbl->SetSourceFromByteStream(e, &s->iface, b);
	if (FAILED(hr)) {
		fail(info, "SetSourceFromByteStream", hr);
		e->lpVtbl->Shutdown(e);
		e->lpVtbl->Release(e);
		e = NULL;
		InterlockedDecrement(&live[CLACK_MF_LIVE_ENGINES]);
		goto out;
	}
	m->engine = e;
	m->notify = n;
	m->stream = s;
	m->format = format;
	m->fpsRead = 0;
	n = NULL;
	s = NULL;
	e = NULL;
out:
	if (b != NULL) {
		SysFreeString(b);
	}
	if (f != NULL) {
		f->lpVtbl->Release(f);
	}
	RELEASE(IMFAttributes, a);
	if (s != NULL) {
		sRelease(&s->iface);
	}
	if (n != NULL) {
		nRelease(n);
	}
	return hr;
}

// clack_me_close_engine shuts the engine down and lets go of its source,
// keeping the device (a new engine follows, or Close).
void clack_me_close_engine(clackMF *m) {
	if (m->engine != NULL) {
		m->engine->lpVtbl->Shutdown(m->engine);
		m->engine->lpVtbl->Release(m->engine);
		m->engine = NULL;
		InterlockedDecrement(&live[CLACK_MF_LIVE_ENGINES]);
	}
	if (m->notify != NULL) {
		nRelease(m->notify);
		m->notify = NULL;
	}
	if (m->stream != NULL) {
		sRelease(&m->stream->iface);
		m->stream = NULL;
	}
}

// clack_me_ring makes the three shared textures, w×h, of the engine's
// format: NT handles first, the legacy shared handles where those fail.
int32_t clack_me_ring(clackMF *m, uint32_t w, uint32_t h, clackMFInfo *info) {
	if (m->dev == NULL) {
		fail(info, "no device", E_UNEXPECTED);
		return E_UNEXPECTED;
	}
	releaseRing(m);
	HRESULT hr = S_OK;
	for (int legacy = 0; legacy < 2; legacy++) {
		hr = S_OK;
		for (int i = 0; i < SLOTS && SUCCEEDED(hr); i++) {
			D3D11_TEXTURE2D_DESC d;
			memset(&d, 0, sizeof d);
			d.Width = w;
			d.Height = h;
			d.MipLevels = 1;
			d.ArraySize = 1;
			d.Format = (DXGI_FORMAT)m->format;
			d.SampleDesc.Count = 1;
			d.Usage = D3D11_USAGE_DEFAULT;
			d.BindFlags = D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE;
			d.MiscFlags = D3D11_RESOURCE_MISC_SHARED_KEYEDMUTEX | (legacy ? 0 : D3D11_RESOURCE_MISC_SHARED_NTHANDLE);
			hr = ID3D11Device_CreateTexture2D(m->dev, &d, NULL, &m->slot[i].tex);
			if (FAILED(hr)) {
				fail(info, legacy ? "CreateTexture2D (shared)" : "CreateTexture2D (NT handle)", hr);
				break;
			}
			InterlockedIncrement(&live[CLACK_MF_LIVE_TEXTURES]);
			hr = ID3D11Texture2D_QueryInterface(m->slot[i].tex, &clackIID_IDXGIKeyedMutex, (void **)&m->slot[i].mx);
			if (FAILED(hr)) {
				fail(info, "IDXGIKeyedMutex", hr);
				break;
			}
			if (!legacy) {
				IDXGIResource1 *r1 = NULL;
				hr = ID3D11Texture2D_QueryInterface(m->slot[i].tex, &clackIID_IDXGIResource1, (void **)&r1);
				if (SUCCEEDED(hr)) {
					hr = IDXGIResource1_CreateSharedHandle(r1, NULL, DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &m->slot[i].handle);
					IDXGIResource1_Release(r1);
				}
				if (FAILED(hr)) {
					fail(info, "CreateSharedHandle", hr);
				}
			} else {
				IDXGIResource *r = NULL;
				hr = ID3D11Texture2D_QueryInterface(m->slot[i].tex, &clackIID_IDXGIResource, (void **)&r);
				if (SUCCEEDED(hr)) {
					hr = IDXGIResource_GetSharedHandle(r, &m->slot[i].handle);
					IDXGIResource_Release(r);
				}
				if (FAILED(hr)) {
					fail(info, "GetSharedHandle", hr);
				}
			}
		}
		m->nt = !legacy;
		if (SUCCEEDED(hr)) {
			break;
		}
		releaseRing(m);
	}
	if (FAILED(hr)) {
		return hr;
	}
	m->w = w;
	m->h_ = h;
	InterlockedIncrement(&m->gen);
	if (info != NULL) {
		info->nt = m->nt;
		info->format = m->format;
	}
	return S_OK;
}

int32_t clack_me_control(clackMF *m, int op, double v) {
	clackEngine *e = m->engine;
	if (e == NULL) {
		return E_UNEXPECTED;
	}
	switch (op) {
	case CLACK_MF_PLAY:
		return e->lpVtbl->Play(e);
	case CLACK_MF_PAUSE:
		return e->lpVtbl->Pause(e);
	case CLACK_MF_SEEK:
		return e->lpVtbl->SetCurrentTime(e, v);
	case CLACK_MF_VOLUME:
		return e->lpVtbl->SetVolume(e, v);
	case CLACK_MF_MUTED:
		return e->lpVtbl->SetMuted(e, v != 0);
	case CLACK_MF_LOOP:
		return e->lpVtbl->SetLoop(e, v != 0);
	case CLACK_MF_RATE:
		e->lpVtbl->SetDefaultPlaybackRate(e, v);
		return e->lpVtbl->SetPlaybackRate(e, v);
	}
	return E_INVALIDARG;
}

// readFPS is the video stream's frame rate (MF_MT_FRAME_RATE), 0 unknown.
static double readFPS(clackEngine *e) {
	DWORD n = 0;
	if (FAILED(e->lpVtbl->GetNumberOfStreams(e, &n))) {
		return 0;
	}
	for (DWORD i = 0; i < n; i++) {
		PROPVARIANT v;
		PropVariantInit(&v);
		int video = 0;
		if (SUCCEEDED(e->lpVtbl->GetStreamAttribute(e, i, &clackMF_MT_MAJOR_TYPE, &v))) {
			video = v.vt == VT_CLSID && v.puuid != NULL && IsEqualGUID(v.puuid, &clackMFMediaType_Video);
		}
		PropVariantClear(&v);
		if (!video) {
			continue;
		}
		double fps = 0;
		if (SUCCEEDED(e->lpVtbl->GetStreamAttribute(e, i, &clackMF_MT_FRAME_RATE, &v)) && v.vt == VT_UI8) {
			UINT32 num = (UINT32)(v.uhVal.QuadPart >> 32), den = (UINT32)v.uhVal.QuadPart;
			if (den != 0) {
				fps = (double)num / (double)den;
			}
		}
		PropVariantClear(&v);
		return fps;
	}
	return 0;
}

void clack_me_state(clackMF *m, clackMFState *st) {
	memset(st, 0, sizeof *st);
	clackEngine *e = m->engine;
	if (e == NULL) {
		st->paused = 1;
		return;
	}
	st->pos = e->lpVtbl->CurTime(e);
	st->dur = e->lpVtbl->GetDuration(e);
	st->paused = e->lpVtbl->IsPaused(e) != 0;
	st->ended = e->lpVtbl->IsEnded(e) != 0;
	st->seeking = e->lpVtbl->IsSeeking(e) != 0;
	st->ready = e->lpVtbl->GetReadyState(e);
	clackError *err = NULL;
	if (SUCCEEDED(e->lpVtbl->GetError(e, &err)) && err != NULL) {
		st->err = err->lpVtbl->GetErrorCode(err);
		st->errExt = (int32_t)err->lpVtbl->GetExtendedErrorCode(err);
		err->lpVtbl->Release(err);
	}
	DWORD w = 0, h = 0;
	if (SUCCEEDED(e->lpVtbl->GetNativeVideoSize(e, &w, &h))) {
		st->w = w;
		st->h = h;
	}
	st->hasVideo = e->lpVtbl->HasVideo(e) != 0;
	st->hasAudio = e->lpVtbl->HasAudio(e) != 0;
	if (!m->fpsRead && st->ready >= 1) {
		m->fps = readFPS(e);
		m->fpsRead = 1;
	}
	st->fps = m->fps;
}

// clack_me_tick is the media thread's poll: when the engine has a new
// frame, it goes into a texture of the ring that isn't the newest (a free
// one, else a frame Gio never took), which is then the newest. 1: a new
// frame, *ppts its presentation time (100 ns units); 0: none (or no
// texture free: a skip); -1: the transfer failed. force transfers the
// frame the engine presented even when it has no new one (a new ring,
// paused): *ppts is -1 then, and a failure is a 0 with its *phr.
int clack_me_tick(clackMF *m, int force, int32_t *phr, int64_t *ppts) {
	*phr = S_OK;
	*ppts = -1;
	clackEngine *e = m->engine;
	if (e == NULL || m->slot[0].tex == NULL) {
		return 0;
	}
	LONGLONG pts = 0;
	HRESULT hr = e->lpVtbl->OnVideoStreamTick(e, &pts);
	int fresh = hr == S_OK;
	if (!fresh && !force) {
		*phr = hr;
		return 0;
	}
	int got = -1;
	for (int k = 0; k < SLOTS && got < 0; k++) {
		int i = (m->newest + 1 + k) % SLOTS;
		if (i == m->newest) {
			continue;
		}
		if (IDXGIKeyedMutex_AcquireSync(m->slot[i].mx, 0, 0) == S_OK || IDXGIKeyedMutex_AcquireSync(m->slot[i].mx, 1, 0) == S_OK) {
			got = i;
		}
	}
	if (got < 0) {
		InterlockedIncrement64(&m->skips);
		*phr = WAIT_TIMEOUT;
		return 0;
	}
	RECT dst = {0, 0, (LONG)m->w, (LONG)m->h_};
	clackARGB black = {0, 0, 0, 255};
	hr = e->lpVtbl->TransferVideoFrame(e, (IUnknown *)m->slot[got].tex, NULL, &dst, &black);
	IDXGIKeyedMutex_ReleaseSync(m->slot[got].mx, FAILED(hr) ? 0 : 1);
	if (FAILED(hr)) {
		*phr = hr;
		return fresh ? -1 : 0;
	}
	m->seq++;
	m->newest = got;
	InterlockedExchange64(&m->published, (m->seq << 2) | got);
	*ppts = fresh ? pts : -1;
	return 1;
}

// clack_me_ask is Gio's ask, on its render thread: the newest frame
// copied into Gio's own texture (an _SRGB view of the same bytes), with a
// reference Gio takes over, and its key. status, bits: 1, Gio asked with a device not seen before (bind to
// it); 2, opening the ring on Gio's device failed (info says how).
uintptr_t clack_me_ask(clackMF *m, uintptr_t devp, uint64_t *key, int *status, clackMFInfo *info) {
	ID3D11Device *dev = (ID3D11Device *)devp;
	*status = 0;
	*key = 0;
	if (dev != m->gioDev) {
		releaseGio(m);
		RELEASE(ID3D11DeviceContext, m->gioCtx);
		RELEASE(ID3D11Device1, m->gioDev1);
		RELEASE(ID3D11Device, m->gioDev);
		m->gioDev = dev;
		ID3D11Device_AddRef(dev);
		if (FAILED(ID3D11Device_QueryInterface(dev, &clackIID_ID3D11Device1, (void **)&m->gioDev1))) {
			m->gioDev1 = NULL;
		}
		ID3D11Device_GetImmediateContext(dev, &m->gioCtx);
		*status = 1;
	}
	LONG gen = InterlockedCompareExchange(&m->gen, 0, 0);
	if (m->askGen != gen) {
		releaseGio(m);
		if (m->slot[0].tex == NULL || m->slot[0].handle == NULL) {
			return 0; // no ring yet
		}
		HRESULT hr = S_OK;
		for (int i = 0; i < SLOTS && SUCCEEDED(hr); i++) {
			if (m->nt && m->gioDev1 != NULL) {
				hr = ID3D11Device1_OpenSharedResource1(m->gioDev1, m->slot[i].handle, &clackIID_ID3D11Texture2D, (void **)&m->open[i].tex);
			} else {
				hr = ID3D11Device_OpenSharedResource(dev, m->slot[i].handle, &clackIID_ID3D11Texture2D, (void **)&m->open[i].tex);
			}
			if (FAILED(hr)) {
				fail(info, m->nt ? "OpenSharedResource1" : "OpenSharedResource", hr);
				m->open[i].tex = NULL;
				break;
			}
			InterlockedIncrement(&live[CLACK_MF_LIVE_TEXTURES]);
			hr = ID3D11Texture2D_QueryInterface(m->open[i].tex, &clackIID_IDXGIKeyedMutex, (void **)&m->open[i].mx);
			if (FAILED(hr)) {
				fail(info, "IDXGIKeyedMutex (Gio's)", hr);
			}
		}
		DXGI_FORMAT srgb = m->format == DXGI_FORMAT_B8G8R8A8_UNORM ? DXGI_FORMAT_B8G8R8A8_UNORM_SRGB : DXGI_FORMAT_R8G8B8A8_UNORM_SRGB;
		if (SUCCEEDED(hr)) {
			D3D11_TEXTURE2D_DESC d;
			memset(&d, 0, sizeof d);
			d.Width = m->w;
			d.Height = m->h_;
			d.MipLevels = 1;
			d.ArraySize = 1;
			d.Format = srgb;
			d.SampleDesc.Count = 1;
			d.Usage = D3D11_USAGE_DEFAULT;
			d.BindFlags = D3D11_BIND_SHADER_RESOURCE;
			hr = ID3D11Device_CreateTexture2D(dev, &d, NULL, &m->local);
			if (FAILED(hr)) {
				fail(info, "CreateTexture2D (Gio's)", hr);
				m->local = NULL;
			} else {
				InterlockedIncrement(&live[CLACK_MF_LIVE_TEXTURES]);
			}
		}
		if (SUCCEEDED(hr)) {
			D3D11_SHADER_RESOURCE_VIEW_DESC v;
			memset(&v, 0, sizeof v);
			v.Format = srgb;
			v.ViewDimension = D3D11_SRV_DIMENSION_TEXTURE2D;
			v.Texture2D.MipLevels = 1;
			hr = ID3D11Device_CreateShaderResourceView(dev, (ID3D11Resource *)m->local, &v, &m->srv);
			if (FAILED(hr)) {
				fail(info, "CreateShaderResourceView", hr);
				m->srv = NULL;
			}
		}
		if (FAILED(hr)) {
			releaseGio(m);
			*status |= 2;
			return 0;
		}
		m->askGen = gen;
	}
	LONG64 pub = InterlockedCompareExchange64(&m->published, 0, 0);
	if (pub != 0 && (pub >> 2) != m->copied) {
		int i = (int)(pub & 3);
		if (IDXGIKeyedMutex_AcquireSync(m->open[i].mx, 1, 0) == S_OK) {
			ID3D11DeviceContext_CopyResource(m->gioCtx, (ID3D11Resource *)m->local, (ID3D11Resource *)m->open[i].tex);
			IDXGIKeyedMutex_ReleaseSync(m->open[i].mx, 0);
			m->copied = pub >> 2;
			InterlockedIncrement64(&m->copies);
		} else {
			InterlockedIncrement64(&m->timeouts);
		}
	}
	if (m->copied == 0) {
		return 0;
	}
	*key = (uint64_t)m->copied;
	ID3D11ShaderResourceView_AddRef(m->srv); // Gio takes this reference over
	return (uintptr_t)m->srv;
}

// clack_me_snapshot copies the ring's newest frame, w×h (the ring's
// size), into dst as RGBA, opaque: 1, or 0 with no frame or on a failure.
// The slot is read under its keyed mutex as the ring left it (published,
// key 1, or taken back by Gio's ask, 0), and released the same. On the
// media thread.
int clack_me_snapshot(clackMF *m, uint8_t *dst, size_t cap, uint32_t w, uint32_t h) {
	int i = m->newest;
	if (m->dev == NULL || i < 0 || m->slot[i].tex == NULL || m->slot[i].mx == NULL || w != m->w || h != m->h_ || (size_t)w * h * 4 > cap) {
		return 0;
	}
	D3D11_TEXTURE2D_DESC d;
	ID3D11Texture2D_GetDesc(m->slot[i].tex, &d);
	d.Usage = D3D11_USAGE_STAGING;
	d.BindFlags = 0;
	d.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
	d.MiscFlags = 0;
	ID3D11Texture2D *st = NULL;
	if (FAILED(ID3D11Device_CreateTexture2D(m->dev, &d, NULL, &st))) {
		return 0;
	}
	ID3D11DeviceContext *ctx = NULL;
	ID3D11Device_GetImmediateContext(m->dev, &ctx);
	UINT64 k = 1;
	HRESULT hr = IDXGIKeyedMutex_AcquireSync(m->slot[i].mx, 1, 100);
	if (hr != S_OK) {
		k = 0;
		hr = IDXGIKeyedMutex_AcquireSync(m->slot[i].mx, 0, 100);
	}
	int ok = 0;
	if (hr == S_OK) {
		ID3D11DeviceContext_CopyResource(ctx, (ID3D11Resource *)st, (ID3D11Resource *)m->slot[i].tex);
		IDXGIKeyedMutex_ReleaseSync(m->slot[i].mx, k);
		D3D11_MAPPED_SUBRESOURCE map;
		if (SUCCEEDED(ID3D11DeviceContext_Map(ctx, (ID3D11Resource *)st, 0, D3D11_MAP_READ, 0, &map))) {
			int bgra = m->format == DXGI_FORMAT_B8G8R8A8_UNORM;
			for (uint32_t y = 0; y < h; y++) {
				const uint8_t *s = (const uint8_t *)map.pData + (size_t)y * map.RowPitch;
				uint8_t *o = dst + (size_t)y * w * 4;
				for (uint32_t x = 0; x < w; x++, s += 4, o += 4) {
					o[0] = bgra ? s[2] : s[0];
					o[1] = s[1];
					o[2] = bgra ? s[0] : s[2];
					o[3] = 255;
				}
			}
			ID3D11DeviceContext_Unmap(ctx, (ID3D11Resource *)st, 0);
			ok = 1;
		}
	}
	ID3D11DeviceContext_Release(ctx);
	ID3D11Texture2D_Release(st);
	return ok;
}

void clack_me_counts(clackMF *m, int64_t *skips, int64_t *timeouts, int64_t *copies) {
	*skips = InterlockedCompareExchange64(&m->skips, 0, 0);
	*timeouts = InterlockedCompareExchange64(&m->timeouts, 0, 0);
	*copies = InterlockedCompareExchange64(&m->copies, 0, 0);
}

// clack_me_close lets go of everything: the engine, its device, the ring,
// and the textures on Gio's device (Gio's wrappers hold the view a frame
// longer, until its texture cache drops them).
void clack_me_close(clackMF *m) {
	clack_me_close_engine(m);
	releaseRing(m);
	RELEASE(IMFDXGIDeviceManager, m->mgr);
	if (m->dev != NULL) {
		ID3D11Device_Release(m->dev);
		m->dev = NULL;
		InterlockedDecrement(&live[CLACK_MF_LIVE_DEVICES]);
	}
	releaseGio(m);
	RELEASE(ID3D11DeviceContext, m->gioCtx);
	RELEASE(ID3D11Device1, m->gioDev1);
	RELEASE(ID3D11Device, m->gioDev);
}

void clack_me_free(clackMF *m) {
	free(m);
	InterlockedDecrement(&live[CLACK_MF_LIVE_PLAYERS]);
}

// clack_me_audio_endpoint is whether Windows has a default audio output
// (S_OK). A runner has none (E_NOTFOUND), and the player then makes the
// engine with MF_MEDIA_ENGINE_FORCEMUTE: the video plays silently there
// anyway. (On a runner the first engines of one process, made without
// it, never got to their metadata, and none has since; a file with an
// audio track takes 2.2 s to its metadata there either way, the silent
// fixture 0.02 s.)
int32_t clack_me_audio_endpoint(void) {
	IMMDeviceEnumerator *e = NULL;
	HRESULT hr = CoCreateInstance(&clackCLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL, &clackIID_IMMDeviceEnumerator, (void **)&e);
	if (FAILED(hr)) {
		return hr;
	}
	IMMDevice *d = NULL;
	hr = IMMDeviceEnumerator_GetDefaultAudioEndpoint(e, eRender, eConsole, &d);
	if (d != NULL) {
		IMMDevice_Release(d);
	}
	IMMDeviceEnumerator_Release(e);
	return hr;
}

// clack_me_gio_format_ok is whether Gio's device samples format as a 2D
// texture.
int clack_me_gio_format_ok(uintptr_t devp, uint32_t format) {
	UINT support = 0;
	if (FAILED(ID3D11Device_CheckFormatSupport((ID3D11Device *)devp, (DXGI_FORMAT)format, &support))) {
		return 0;
	}
	UINT need = D3D11_FORMAT_SUPPORT_TEXTURE2D | D3D11_FORMAT_SUPPORT_SHADER_SAMPLE;
	return (support & need) == need;
}
