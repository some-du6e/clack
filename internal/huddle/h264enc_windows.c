//go:build windows && cgo

// Windows' own H.264 encoder: the Media Foundation encoder MFT (the
// synchronous one, Microsoft's software H.264 encoder, which every
// desktop Windows 10/11 has). NV12 in, Annex B out, constrained
// baseline, low-latency mode, keyframes when asked for
// (CODECAPI_AVEncVideoForceKeyFrame).

#define COBJMACROS
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0A00
#endif
#ifndef WINVER
#define WINVER 0x0A00
#endif
#include <windows.h>
#include <mfapi.h>
#include <mferror.h>
#include <mfidl.h>
#include <mftransform.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <strmif.h>

// The codec API's GUIDs (codecapi.h), defined here so nothing depends on
// how a mingw version exports them.
static const GUID clackIIDCodecAPI = {0x901db4c7, 0x31ce, 0x41a2, {0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda}};
static const GUID clackRateControl = {0x1c0608e9, 0x370c, 0x4710, {0x8a, 0x58, 0xcb, 0x61, 0x81, 0xc4, 0x24, 0x23}};
static const GUID clackMeanBitRate = {0xf7222374, 0x2144, 0x4815, {0xb5, 0x50, 0xa3, 0x7f, 0x8e, 0x12, 0xee, 0x52}};
static const GUID clackLowLatency = {0x9c27891a, 0xed7a, 0x40e1, {0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee}};
static const GUID clackForceKey = {0x398c1b98, 0x8353, 0x475a, {0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45}};
static const GUID clackGOPSize = {0x95f31b26, 0x95a4, 0x41aa, {0x93, 0x03, 0x24, 0x6a, 0x7f, 0xc6, 0xee, 0xf1}};

typedef struct {
	IMFTransform *t;
	ICodecAPI *api;
	int w, h, fps;
	int provides; // the MFT makes its own output samples
	DWORD outSize;
	uint8_t *out;
	size_t cap;
	uint8_t seq[512]; // MF_MT_MPEG_SEQUENCE_HEADER: SPS and PPS
	int seqLen;
} clackMF;

static int started;

static HRESULT setSize(IMFMediaType *t, REFGUID key, UINT32 a, UINT32 b) { return IMFMediaType_SetUINT64(t, key, ((UINT64)a << 32) | b); }

static void setU4(ICodecAPI *api, const GUID *key, ULONG v) {
	VARIANT x;
	VariantInit(&x);
	x.vt = VT_UI4;
	x.ulVal = v;
	ICodecAPI_SetValue(api, key, &x);
}

static void readSeq(clackMF *c) {
	IMFMediaType *ot = NULL;
	if (SUCCEEDED(IMFTransform_GetOutputCurrentType(c->t, 0, &ot)) && ot != NULL) {
		UINT32 n = 0;
		if (SUCCEEDED(IMFMediaType_GetBlobSize(ot, &MF_MT_MPEG_SEQUENCE_HEADER, &n)) && n > 0 && n <= sizeof c->seq) {
			if (SUCCEEDED(IMFMediaType_GetBlob(ot, &MF_MT_MPEG_SEQUENCE_HEADER, c->seq, n, &n))) {
				c->seqLen = (int)n;
			}
		}
		IMFMediaType_Release(ot);
	}
}

void clack_mf_free(void *p);

void *clack_mf_new(int w, int h, int fps, int bps, int *status) {
	HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED); // keeps the MTA alive; RPC_E_CHANGED_MODE is fine
	(void)hr;
	if (!started) {
		if (FAILED(hr = MFStartup(MF_VERSION, MFSTARTUP_LITE))) {
			*status = (int)hr;
			return NULL;
		}
		started = 1;
	}
	MFT_REGISTER_TYPE_INFO in = {MFMediaType_Video, MFVideoFormat_NV12}, out = {MFMediaType_Video, MFVideoFormat_H264};
	IMFActivate **acts = NULL;
	UINT32 n = 0;
	hr = MFTEnumEx(MFT_CATEGORY_VIDEO_ENCODER, MFT_ENUM_FLAG_SYNCMFT | MFT_ENUM_FLAG_LOCALMFT | MFT_ENUM_FLAG_SORTANDFILTER, &in, &out,
	               &acts, &n);
	if (FAILED(hr) || n == 0) {
		*status = FAILED(hr) ? (int)hr : -1;
		return NULL;
	}
	clackMF *c = calloc(1, sizeof *c);
	c->w = w;
	c->h = h;
	c->fps = fps;
	hr = IMFActivate_ActivateObject(acts[0], &IID_IMFTransform, (void **)&c->t);
	for (UINT32 i = 0; i < n; i++) {
		IMFActivate_Release(acts[i]);
	}
	CoTaskMemFree(acts);
	if (FAILED(hr) || c->t == NULL) {
		*status = (int)hr;
		free(c);
		return NULL;
	}
	IMFMediaType *ot = NULL, *it = NULL;
	MFCreateMediaType(&ot);
	IMFMediaType_SetGUID(ot, &MF_MT_MAJOR_TYPE, &MFMediaType_Video);
	IMFMediaType_SetGUID(ot, &MF_MT_SUBTYPE, &MFVideoFormat_H264);
	IMFMediaType_SetUINT32(ot, &MF_MT_AVG_BITRATE, bps);
	setSize(ot, &MF_MT_FRAME_SIZE, w, h);
	setSize(ot, &MF_MT_FRAME_RATE, fps, 1);
	setSize(ot, &MF_MT_PIXEL_ASPECT_RATIO, 1, 1);
	IMFMediaType_SetUINT32(ot, &MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive);
	IMFMediaType_SetUINT32(ot, &MF_MT_MPEG2_PROFILE, 66); // eAVEncH264VProfile_Base
	hr = IMFTransform_SetOutputType(c->t, 0, ot, 0);
	IMFMediaType_Release(ot);
	if (FAILED(hr)) {
		*status = (int)hr;
		clack_mf_free(c);
		return NULL;
	}
	MFCreateMediaType(&it);
	IMFMediaType_SetGUID(it, &MF_MT_MAJOR_TYPE, &MFMediaType_Video);
	IMFMediaType_SetGUID(it, &MF_MT_SUBTYPE, &MFVideoFormat_NV12);
	setSize(it, &MF_MT_FRAME_SIZE, w, h);
	setSize(it, &MF_MT_FRAME_RATE, fps, 1);
	setSize(it, &MF_MT_PIXEL_ASPECT_RATIO, 1, 1);
	IMFMediaType_SetUINT32(it, &MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive);
	hr = IMFTransform_SetInputType(c->t, 0, it, 0);
	IMFMediaType_Release(it);
	if (FAILED(hr)) {
		*status = (int)hr;
		clack_mf_free(c);
		return NULL;
	}
	if (SUCCEEDED(IMFTransform_QueryInterface(c->t, &clackIIDCodecAPI, (void **)&c->api)) && c->api != NULL) {
		setU4(c->api, &clackRateControl, 0); // eAVEncCommonRateControlMode_CBR
		setU4(c->api, &clackMeanBitRate, bps);
		setU4(c->api, &clackGOPSize, fps * 20); // Chime asks (PLI) when it needs a keyframe
		VARIANT x;
		VariantInit(&x);
		x.vt = VT_BOOL;
		x.boolVal = VARIANT_TRUE;
		ICodecAPI_SetValue(c->api, &clackLowLatency, &x);
	}
	MFT_OUTPUT_STREAM_INFO osi;
	memset(&osi, 0, sizeof osi);
	IMFTransform_GetOutputStreamInfo(c->t, 0, &osi);
	c->provides = (osi.dwFlags & (MFT_OUTPUT_STREAM_PROVIDES_SAMPLES | MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES)) != 0;
	c->outSize = osi.cbSize > 0 ? osi.cbSize : (DWORD)(w * h * 2);
	IMFTransform_ProcessMessage(c->t, MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0);
	IMFTransform_ProcessMessage(c->t, MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0);
	readSeq(c);
	*status = 0;
	return c;
}

static void put(clackMF *c, size_t *len, const uint8_t *p, size_t n) {
	if (*len + n > c->cap) {
		size_t nc = c->cap ? c->cap : 65536;
		while (nc < *len + n) {
			nc *= 2;
		}
		c->out = realloc(c->out, nc);
		c->cap = nc;
	}
	memcpy(c->out + *len, p, n);
	*len += n;
}

// clack_mf_encode encodes one NV12 picture; *out/*n is what the MFT gave
// back (Annex B; *n 0 when it gave nothing yet), *seq/*seqLen the
// sequence header to put before a keyframe that lacks it.
int clack_mf_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, long long t100ns, int key, uint8_t **out, int *n,
                    uint8_t **seq, int *seqLen) {
	clackMF *c = p;
	int w = c->w, h = c->h;
	DWORD size = (DWORD)(w * h * 3 / 2);
	IMFMediaBuffer *buf = NULL;
	HRESULT hr = MFCreateMemoryBuffer(size, &buf);
	if (FAILED(hr)) {
		return (int)hr;
	}
	BYTE *dst = NULL;
	IMFMediaBuffer_Lock(buf, &dst, NULL, NULL);
	for (int r = 0; r < h; r++) {
		memcpy(dst + (size_t)r * w, y + (size_t)r * ys, w);
	}
	for (int r = 0; r < h / 2; r++) {
		memcpy(dst + (size_t)w * h + (size_t)r * w, uv + (size_t)r * uvs, w);
	}
	IMFMediaBuffer_Unlock(buf);
	IMFMediaBuffer_SetCurrentLength(buf, size);
	IMFSample *s = NULL;
	MFCreateSample(&s);
	IMFSample_AddBuffer(s, buf);
	IMFMediaBuffer_Release(buf);
	IMFSample_SetSampleTime(s, t100ns);
	IMFSample_SetSampleDuration(s, 10000000LL / c->fps);
	if (key && c->api != NULL) {
		setU4(c->api, &clackForceKey, 1);
	}
	hr = IMFTransform_ProcessInput(c->t, 0, s, 0);
	IMFSample_Release(s);
	if (FAILED(hr) && hr != MF_E_NOTACCEPTING) {
		return (int)hr;
	}
	size_t len = 0;
	for (int i = 0; i < 8; i++) {
		MFT_OUTPUT_DATA_BUFFER ob;
		memset(&ob, 0, sizeof ob);
		IMFSample *os = NULL;
		if (!c->provides) {
			IMFMediaBuffer *obuf = NULL;
			MFCreateMemoryBuffer(c->outSize, &obuf);
			MFCreateSample(&os);
			IMFSample_AddBuffer(os, obuf);
			IMFMediaBuffer_Release(obuf);
			ob.pSample = os;
		}
		DWORD st = 0;
		hr = IMFTransform_ProcessOutput(c->t, 0, 1, &ob, &st);
		if (ob.pEvents != NULL) {
			IMFCollection_Release(ob.pEvents);
		}
		if (hr == MF_E_TRANSFORM_STREAM_CHANGE) { // take the type it offers now
			IMFMediaType *nt = NULL;
			if (SUCCEEDED(IMFTransform_GetOutputAvailableType(c->t, 0, 0, &nt))) {
				IMFTransform_SetOutputType(c->t, 0, nt, 0);
				IMFMediaType_Release(nt);
				readSeq(c);
			}
			if (os != NULL) {
				IMFSample_Release(os);
			}
			continue;
		}
		if (FAILED(hr)) {
			if (os != NULL) {
				IMFSample_Release(os);
			}
			break; // MF_E_TRANSFORM_NEED_MORE_INPUT: done for this frame
		}
		IMFSample *got = ob.pSample;
		IMFMediaBuffer *cb = NULL;
		if (got != NULL && SUCCEEDED(IMFSample_ConvertToContiguousBuffer(got, &cb))) {
			BYTE *d = NULL;
			DWORD dl = 0;
			if (SUCCEEDED(IMFMediaBuffer_Lock(cb, &d, NULL, &dl))) {
				put(c, &len, d, dl);
				IMFMediaBuffer_Unlock(cb);
			}
			IMFMediaBuffer_Release(cb);
		}
		if (got != NULL) {
			IMFSample_Release(got);
		}
	}
	*out = c->out;
	*n = (int)len;
	*seq = c->seq;
	*seqLen = c->seqLen;
	return 0;
}

void clack_mf_bitrate(void *p, int bps) {
	clackMF *c = p;
	if (c->api != NULL) {
		setU4(c->api, &clackMeanBitRate, bps);
	}
}

void clack_mf_free(void *p) {
	clackMF *c = p;
	if (c->t != NULL) {
		IMFTransform_ProcessMessage(c->t, MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0);
		IMFTransform_ProcessMessage(c->t, MFT_MESSAGE_NOTIFY_END_STREAMING, 0);
	}
	if (c->api != NULL) {
		ICodecAPI_Release(c->api);
	}
	if (c->t != NULL) {
		IMFTransform_Release(c->t);
	}
	free(c->out);
	free(c);
}
