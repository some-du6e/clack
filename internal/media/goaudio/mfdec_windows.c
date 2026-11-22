//go:build windows && cgo

// Media Foundation's Source Reader decoding what Go audio can't (AAC:
// Slack's aac renditions and most m4a uploads): the whole file from
// memory (an HGLOBAL stream), its first audio stream to 32-bit float PCM
// at its own rate and channels. Go audio plays that PCM out of its own
// sink (the speakers, or its clock where there are none), so a machine
// with no audio output plays it in time, silently, as Chromium's fake
// audio sink does; the Media Engine fails there, its audio renderer
// being all an audio file has. mfplat.dll and mfreadwrite.dll are loaded
// at the first file (Windows N without the Media Feature Pack has
// neither: AAC doesn't play there, as on Linux).
//
// Every call is on the decoder's own thread (mfdec_windows.go), in COM's
// multithreaded apartment; every GUID is defined here, so nothing needs
// a uuid library.

#define COBJMACROS
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0A00
#endif
#include <windows.h>
#include <mfapi.h>
#include <mferror.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <propidl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "mfdec_windows.h"

static const GUID decMF_MT_MAJOR_TYPE = {0x48eba18e, 0xf8c9, 0x4687, {0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}};
static const GUID decMF_MT_SUBTYPE = {0xf7e34c9a, 0x42e8, 0x4714, {0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}};
static const GUID decMFMediaType_Audio = {0x73647561, 0x0000, 0x0010, {0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}};
static const GUID decMFAudioFormat_Float = {0x00000003, 0x0000, 0x0010, {0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}};
static const GUID decMF_MT_AUDIO_SAMPLES_PER_SECOND = {0x5faeeae7, 0x0290, 0x4c31, {0x9e, 0x8a, 0xc5, 0x34, 0xf6, 0x8d, 0x9d, 0xba}};
static const GUID decMF_MT_AUDIO_NUM_CHANNELS = {0x37e48bf5, 0x645e, 0x4c5b, {0x89, 0xde, 0xad, 0xa9, 0xe2, 0x9b, 0x69, 0x6a}};
static const GUID decMF_PD_DURATION = {0x6c990d33, 0xbb8e, 0x477a, {0x85, 0x98, 0x0d, 0x5d, 0x96, 0xfc, 0xd8, 0x8a}};
static const GUID decMF_BYTESTREAM_CONTENT_TYPE = {0xfc358289, 0x3cb6, 0x460c, {0xa4, 0x24, 0xb6, 0x68, 0x12, 0x60, 0x37, 0x5a}};
static const GUID decIID_IMFAttributes = {0x2cd2d921, 0xc447, 0x44a7, {0xa1, 0x3c, 0x4a, 0xda, 0xbf, 0xc2, 0x47, 0xe3}};
static const GUID decGUID_NULL = {0x00000000, 0x0000, 0x0000, {0, 0, 0, 0, 0, 0, 0, 0}};

typedef HRESULT(WINAPI *pMFStartup)(ULONG, DWORD);
typedef HRESULT(WINAPI *pMFCreateMediaType)(IMFMediaType **);
typedef HRESULT(WINAPI *pMFCreateMFByteStreamOnStream)(IStream *, IMFByteStream **);
typedef HRESULT(WINAPI *pMFCreateSourceReaderFromByteStream)(IMFByteStream *, IMFAttributes *, IMFSourceReader **);
static struct {
	pMFStartup Startup;
	pMFCreateMediaType CreateMediaType;
	pMFCreateMFByteStreamOnStream CreateByteStreamOnStream;
	pMFCreateSourceReaderFromByteStream CreateSourceReader;
} fn;
static INIT_ONCE startOnce = INIT_ONCE_STATIC_INIT;
static HRESULT startHR = E_FAIL;

static BOOL CALLBACK startup(PINIT_ONCE once, PVOID param, PVOID *ctx) {
	(void)once;
	(void)param;
	(void)ctx;
	HMODULE plat = LoadLibraryW(L"mfplat.dll");
	HMODULE rw = LoadLibraryW(L"mfreadwrite.dll");
	if (plat == NULL || rw == NULL) {
		startHR = HRESULT_FROM_WIN32(GetLastError());
		return TRUE;
	}
	fn.Startup = (pMFStartup)(void *)GetProcAddress(plat, "MFStartup");
	fn.CreateMediaType = (pMFCreateMediaType)(void *)GetProcAddress(plat, "MFCreateMediaType");
	fn.CreateByteStreamOnStream = (pMFCreateMFByteStreamOnStream)(void *)GetProcAddress(plat, "MFCreateMFByteStreamOnStream");
	fn.CreateSourceReader = (pMFCreateSourceReaderFromByteStream)(void *)GetProcAddress(rw, "MFCreateSourceReaderFromByteStream");
	if (!fn.Startup || !fn.CreateMediaType || !fn.CreateByteStreamOnStream || !fn.CreateSourceReader) {
		startHR = E_NOINTERFACE;
		return TRUE;
	}
	startHR = fn.Startup(MF_VERSION, MFSTARTUP_LITE);
	return TRUE;
}

void clack_dec_thread_init(void) { CoInitializeEx(NULL, COINIT_MULTITHREADED); }
void clack_dec_thread_done(void) { CoUninitialize(); }

struct clackDec {
	IMFSourceReader *r;
	uint32_t rate, ch;
	// the sample being read: its buffer locked, at off of len bytes
	IMFMediaBuffer *buf;
	BYTE *p;
	DWORD len, off;
	int64_t ts;
};

#define AUDIO ((DWORD)MF_SOURCE_READER_FIRST_AUDIO_STREAM)

int32_t clack_dec_open(const void *data, uint32_t n, const uint16_t *mime, clackDec **out, uint32_t *rate, uint32_t *channels, int64_t *dur) {
	*out = NULL;
	*rate = 0;
	*channels = 0;
	*dur = -1;
	InitOnceExecuteOnce(&startOnce, startup, NULL, NULL);
	if (FAILED(startHR)) {
		return startHR;
	}
	IStream *stm = NULL;
	IMFByteStream *bs = NULL;
	IMFSourceReader *r = NULL;
	IMFMediaType *mt = NULL, *cur = NULL;
	HRESULT hr = CreateStreamOnHGlobal(NULL, TRUE, &stm);
	if (FAILED(hr)) {
		return hr;
	}
	ULONG wrote = 0;
	hr = IStream_Write(stm, data, n, &wrote);
	if (SUCCEEDED(hr) && wrote != n) {
		hr = E_OUTOFMEMORY;
	}
	if (SUCCEEDED(hr)) {
		LARGE_INTEGER zero;
		zero.QuadPart = 0;
		hr = IStream_Seek(stm, zero, STREAM_SEEK_SET, NULL);
	}
	if (SUCCEEDED(hr)) {
		hr = fn.CreateByteStreamOnStream(stm, &bs);
	}
	IStream_Release(stm);
	if (FAILED(hr)) {
		return hr;
	}
	if (mime != NULL) { // the source resolver's hint
		IMFAttributes *a = NULL;
		if (SUCCEEDED(IMFByteStream_QueryInterface(bs, &decIID_IMFAttributes, (void **)&a)) && a != NULL) {
			IMFAttributes_SetString(a, &decMF_BYTESTREAM_CONTENT_TYPE, (LPCWSTR)mime);
			IMFAttributes_Release(a);
		}
	}
	hr = fn.CreateSourceReader(bs, NULL, &r);
	IMFByteStream_Release(bs);
	if (FAILED(hr)) {
		return hr;
	}
	IMFSourceReader_SetStreamSelection(r, (DWORD)MF_SOURCE_READER_ALL_STREAMS, FALSE);
	hr = IMFSourceReader_SetStreamSelection(r, AUDIO, TRUE);
	if (SUCCEEDED(hr)) {
		hr = fn.CreateMediaType(&mt);
	}
	if (SUCCEEDED(hr)) {
		IMFMediaType_SetGUID(mt, &decMF_MT_MAJOR_TYPE, &decMFMediaType_Audio);
		IMFMediaType_SetGUID(mt, &decMF_MT_SUBTYPE, &decMFAudioFormat_Float);
		hr = IMFSourceReader_SetCurrentMediaType(r, AUDIO, NULL, mt);
		IMFMediaType_Release(mt);
	}
	if (SUCCEEDED(hr)) {
		hr = IMFSourceReader_GetCurrentMediaType(r, AUDIO, &cur);
	}
	UINT32 sr = 0, ch = 0;
	if (SUCCEEDED(hr)) {
		IMFMediaType_GetUINT32(cur, &decMF_MT_AUDIO_SAMPLES_PER_SECOND, &sr);
		IMFMediaType_GetUINT32(cur, &decMF_MT_AUDIO_NUM_CHANNELS, &ch);
		IMFMediaType_Release(cur);
		if (sr == 0 || ch == 0) {
			hr = MF_E_INVALIDMEDIATYPE;
		}
	}
	if (FAILED(hr)) {
		IMFSourceReader_Release(r);
		return hr;
	}
	PROPVARIANT v;
	PropVariantInit(&v);
	if (SUCCEEDED(IMFSourceReader_GetPresentationAttribute(r, (DWORD)MF_SOURCE_READER_MEDIASOURCE, &decMF_PD_DURATION, &v)) && v.vt == VT_UI8) {
		*dur = (int64_t)v.uhVal.QuadPart;
	}
	PropVariantClear(&v);
	clackDec *d = calloc(1, sizeof *d);
	if (d == NULL) {
		IMFSourceReader_Release(r);
		return E_OUTOFMEMORY;
	}
	d->r = r;
	d->rate = sr;
	d->ch = ch;
	d->ts = -1;
	*out = d;
	*rate = sr;
	*channels = ch;
	return S_OK;
}

static void dropBuffer(clackDec *d) {
	if (d->buf != NULL) {
		IMFMediaBuffer_Unlock(d->buf);
		IMFMediaBuffer_Release(d->buf);
		d->buf = NULL;
	}
	d->p = NULL;
	d->len = d->off = 0;
}

int32_t clack_dec_read(clackDec *d, float *dst, uint32_t cap, uint32_t *frames, int64_t *ts, int *eof) {
	*frames = 0;
	*ts = -1;
	*eof = 0;
	uint32_t fb = d->ch * 4; // a frame's bytes
	// the next sample with data (a gap or a stream tick has none)
	for (int i = 0; d->buf == NULL; i++) {
		if (i == 1000) {
			return S_OK; // none this time: the caller reads again
		}
		DWORD got = 0, flags = 0;
		LONGLONG t = 0;
		IMFSample *s = NULL;
		HRESULT hr = IMFSourceReader_ReadSample(d->r, AUDIO, 0, &got, &flags, &t, &s);
		if (FAILED(hr)) {
			return hr;
		}
		if (flags & MF_SOURCE_READERF_ENDOFSTREAM) {
			if (s != NULL) {
				IMFSample_Release(s);
			}
			*eof = 1;
			return S_OK;
		}
		if (s == NULL) {
			continue;
		}
		IMFMediaBuffer *b = NULL;
		hr = IMFSample_ConvertToContiguousBuffer(s, &b);
		IMFSample_Release(s);
		if (FAILED(hr)) {
			return hr;
		}
		BYTE *p = NULL;
		DWORD len = 0;
		hr = IMFMediaBuffer_Lock(b, &p, NULL, &len);
		if (FAILED(hr)) {
			IMFMediaBuffer_Release(b);
			return hr;
		}
		if (len < fb) {
			IMFMediaBuffer_Unlock(b);
			IMFMediaBuffer_Release(b);
			continue;
		}
		d->buf = b;
		d->p = p;
		d->len = len;
		d->off = 0;
		d->ts = (int64_t)t;
	}
	uint32_t avail = (d->len - d->off) / fb;
	uint32_t n = avail < cap ? avail : cap;
	memcpy(dst, d->p + d->off, (size_t)n * fb);
	*ts = d->ts + (int64_t)(d->off / fb) * 10000000 / (int64_t)d->rate;
	*frames = n;
	d->off += n * fb;
	if (d->len - d->off < fb) {
		dropBuffer(d);
	}
	return S_OK;
}

int32_t clack_dec_seek(clackDec *d, int64_t at) {
	dropBuffer(d);
	PROPVARIANT v;
	PropVariantInit(&v);
	v.vt = VT_I8;
	v.hVal.QuadPart = at;
	HRESULT hr = IMFSourceReader_SetCurrentPosition(d->r, &decGUID_NULL, &v);
	PropVariantClear(&v);
	return hr;
}

void clack_dec_close(clackDec *d) {
	if (d == NULL) {
		return;
	}
	dropBuffer(d);
	if (d->r != NULL) {
		IMFSourceReader_Release(d->r);
	}
	free(d);
}
