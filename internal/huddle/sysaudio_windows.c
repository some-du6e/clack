//go:build windows && cgo

// "Share system audio" on Windows: WASAPI process loopback of everything
// except Clack's own process tree (AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_
// LOOPBACK, PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE), so the
// huddle's own sound doesn't go back out as an echo. Windows 10 build
// 20348 and later; older builds fail the activation and the picker offers
// no checkbox. 48 kHz stereo 16-bit, delivered from a capture thread.

#define COBJMACROS
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0A00
#endif
#include <windows.h>
#include <audioclient.h>
#ifndef AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM
#define AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM 0x80000000
#endif
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void clackLoopbackPCM(uintptr_t h, int16_t *pcm, int frames);

static const GUID clackIIDAudioClient = {0x1CB9AD4C, 0xDBFA, 0x4c32, {0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}};
static const GUID clackIIDCaptureClient = {0xC8ADBD64, 0xE71E, 0x48a0, {0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}};
static const GUID clackIIDCompletion = {0x41D949AB, 0x9862, 0x444A, {0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}};
static const GUID clackIIDAgile = {0x94ea2b94, 0xe9cc, 0x49e0, {0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}};
static const GUID clackIIDUnknown = {0x00000000, 0x0000, 0x0000, {0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}};

// audioclientactivationparams.h
typedef struct {
	DWORD TargetProcessId;
	int ProcessLoopbackMode; // PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE 1
} clackLoopbackParams;
typedef struct {
	int ActivationType; // AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK 1
	clackLoopbackParams ProcessLoopbackParams;
} clackActivationParams;

// IActivateAudioInterfaceAsyncOperation and the completion handler
// (mmdeviceapi.h), as vtables.
typedef struct clackAsyncOp clackAsyncOp;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackAsyncOp *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackAsyncOp *);
	ULONG(STDMETHODCALLTYPE *Release)(clackAsyncOp *);
	HRESULT(STDMETHODCALLTYPE *GetActivateResult)(clackAsyncOp *, HRESULT *, IUnknown **);
} clackAsyncOpVtbl;
struct clackAsyncOp {
	const clackAsyncOpVtbl *lpVtbl;
};

typedef struct clackHandler clackHandler;
typedef struct {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(clackHandler *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(clackHandler *);
	ULONG(STDMETHODCALLTYPE *Release)(clackHandler *);
	HRESULT(STDMETHODCALLTYPE *ActivateCompleted)(clackHandler *, clackAsyncOp *);
} clackHandlerVtbl;
struct clackHandler {
	const clackHandlerVtbl *lpVtbl;
	LONG ref;
	HANDLE done;
	HRESULT hr;
	IAudioClient *client;
};

static HRESULT STDMETHODCALLTYPE hQI(clackHandler *h, REFIID riid, void **out) {
	if (IsEqualIID(riid, &clackIIDUnknown) || IsEqualIID(riid, &clackIIDCompletion) || IsEqualIID(riid, &clackIIDAgile)) {
		*out = h;
		InterlockedIncrement(&h->ref);
		return S_OK;
	}
	*out = NULL;
	return E_NOINTERFACE;
}
static ULONG STDMETHODCALLTYPE hAddRef(clackHandler *h) { return InterlockedIncrement(&h->ref); }
static ULONG STDMETHODCALLTYPE hRelease(clackHandler *h) {
	LONG n = InterlockedDecrement(&h->ref);
	if (n == 0) {
		CloseHandle(h->done);
		free(h);
	}
	return n;
}
static HRESULT STDMETHODCALLTYPE hCompleted(clackHandler *h, clackAsyncOp *op) {
	HRESULT hr = E_FAIL;
	IUnknown *unk = NULL;
	if (SUCCEEDED(op->lpVtbl->GetActivateResult(op, &hr, &unk)) && SUCCEEDED(hr) && unk != NULL) {
		hr = IUnknown_QueryInterface(unk, &clackIIDAudioClient, (void **)&h->client);
	}
	if (unk != NULL) {
		IUnknown_Release(unk);
	}
	h->hr = hr;
	SetEvent(h->done);
	return S_OK;
}
static const clackHandlerVtbl handlerVtbl = {hQI, hAddRef, hRelease, hCompleted};

typedef HRESULT(WINAPI *activateFn)(LPCWSTR, REFIID, PROPVARIANT *, clackHandler *, clackAsyncOp **);

// activate gets an IAudioClient for process loopback that leaves our
// process tree out.
static IAudioClient *activate(HRESULT *status) {
	static activateFn fn;
	static int looked;
	if (!looked) {
		looked = 1;
		HMODULE m = LoadLibraryW(L"Mmdevapi.dll");
		if (m != NULL) {
			fn = (activateFn)(void *)GetProcAddress(m, "ActivateAudioInterfaceAsync");
		}
	}
	if (fn == NULL) {
		*status = E_NOTIMPL;
		return NULL;
	}
	CoInitializeEx(NULL, COINIT_MULTITHREADED);
	clackActivationParams p = {1, {GetCurrentProcessId(), 1}};
	PROPVARIANT pv;
	memset(&pv, 0, sizeof pv);
	pv.vt = VT_BLOB;
	pv.blob.cbSize = sizeof p;
	pv.blob.pBlobData = (BYTE *)&p;
	clackHandler *h = calloc(1, sizeof *h);
	h->lpVtbl = &handlerVtbl;
	h->ref = 1;
	h->done = CreateEventW(NULL, TRUE, FALSE, NULL);
	clackAsyncOp *op = NULL;
	HRESULT hr = fn(L"VAD\\Process_Loopback", &clackIIDAudioClient, &pv, h, &op);
	IAudioClient *c = NULL;
	if (SUCCEEDED(hr)) {
		if (WaitForSingleObject(h->done, 5000) == WAIT_OBJECT_0) {
			hr = h->hr;
			c = h->client;
		} else {
			hr = HRESULT_FROM_WIN32(ERROR_TIMEOUT);
		}
	}
	if (op != NULL) {
		op->lpVtbl->Release(op);
	}
	hRelease(h);
	*status = hr;
	return SUCCEEDED(hr) ? c : NULL;
}

typedef struct {
	IAudioClient *client;
	IAudioCaptureClient *cap;
	HANDLE ev, thread;
	volatile LONG stop;
	uintptr_t handle;
} clackLoopback;

static DWORD WINAPI loop(LPVOID arg) {
	clackLoopback *l = arg;
	CoInitializeEx(NULL, COINIT_MULTITHREADED);
	int16_t *silence = NULL;
	UINT32 silenceN = 0;
	while (!l->stop) {
		if (WaitForSingleObject(l->ev, 100) != WAIT_OBJECT_0) {
			continue;
		}
		UINT32 n = 0;
		while (!l->stop && SUCCEEDED(IAudioCaptureClient_GetNextPacketSize(l->cap, &n)) && n > 0) {
			BYTE *data = NULL;
			UINT32 frames = 0;
			DWORD flags = 0;
			if (FAILED(IAudioCaptureClient_GetBuffer(l->cap, &data, &frames, &flags, NULL, NULL))) {
				break;
			}
			if (frames > 0) {
				if (flags & AUDCLNT_BUFFERFLAGS_SILENT || data == NULL) {
					if (silenceN < frames) {
						free(silence);
						silence = calloc(frames * 2, sizeof *silence);
						silenceN = frames;
					}
					clackLoopbackPCM(l->handle, silence, (int)frames);
				} else {
					clackLoopbackPCM(l->handle, (int16_t *)data, (int)frames);
				}
			}
			IAudioCaptureClient_ReleaseBuffer(l->cap, frames);
		}
	}
	free(silence);
	return 0;
}

// clack_loopback_probe reports whether process loopback works here.
int clack_loopback_probe(void) {
	HRESULT hr;
	IAudioClient *c = activate(&hr);
	if (c == NULL) {
		return 0;
	}
	IAudioClient_Release(c);
	return 1;
}

void *clack_loopback_start(uintptr_t handle, int *status) {
	HRESULT hr;
	IAudioClient *c = activate(&hr);
	if (c == NULL) {
		*status = (int)hr;
		return NULL;
	}
	WAVEFORMATEX wfx;
	memset(&wfx, 0, sizeof wfx);
	wfx.wFormatTag = WAVE_FORMAT_PCM;
	wfx.nChannels = 2;
	wfx.nSamplesPerSec = 48000;
	wfx.wBitsPerSample = 16;
	wfx.nBlockAlign = 4;
	wfx.nAvgBytesPerSec = 48000 * 4;
	hr = IAudioClient_Initialize(c, AUDCLNT_SHAREMODE_SHARED,
	                             AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM,
	                             2000000, 0, &wfx, NULL);
	if (FAILED(hr)) {
		IAudioClient_Release(c);
		*status = (int)hr;
		return NULL;
	}
	clackLoopback *l = calloc(1, sizeof *l);
	l->client = c;
	l->handle = handle;
	l->ev = CreateEventW(NULL, FALSE, FALSE, NULL);
	hr = IAudioClient_GetService(c, &clackIIDCaptureClient, (void **)&l->cap);
	if (SUCCEEDED(hr)) {
		hr = IAudioClient_SetEventHandle(c, l->ev);
	}
	if (SUCCEEDED(hr)) {
		hr = IAudioClient_Start(c);
	}
	if (FAILED(hr)) {
		if (l->cap != NULL) {
			IAudioCaptureClient_Release(l->cap);
		}
		IAudioClient_Release(c);
		CloseHandle(l->ev);
		free(l);
		*status = (int)hr;
		return NULL;
	}
	l->thread = CreateThread(NULL, 0, loop, l, 0, NULL);
	*status = 0;
	return l;
}

void clack_loopback_stop(void *p) {
	clackLoopback *l = p;
	InterlockedExchange(&l->stop, 1);
	SetEvent(l->ev);
	if (l->thread != NULL) {
		WaitForSingleObject(l->thread, 2000);
		CloseHandle(l->thread);
	}
	IAudioClient_Stop(l->client);
	IAudioCaptureClient_Release(l->cap);
	IAudioClient_Release(l->client);
	CloseHandle(l->ev);
	free(l);
}
