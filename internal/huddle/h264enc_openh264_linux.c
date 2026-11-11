//go:build linux && cgo

// OpenH264 (Cisco's BSD-licensed encoder) loaded at run time: Linux has
// no OS H.264 encoder, and distributions package this one (libopenh264),
// so Clack uses it when it's installed and builds without it. The types
// are codec/api/wels' C interface, declared here; 2.6 (libopenh264.so.8)
// added PSNR fields to SLayerBSInfo and SSourcePicture, so the frame
// layout is picked from the loaded library's version.

#include <dlfcn.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	int iUsageType; // EUsageType: CAMERA_VIDEO_REAL_TIME 0, SCREEN_CONTENT_REAL_TIME 1
	int iPicWidth;
	int iPicHeight;
	int iTargetBitrate;
	int iRCMode; // RC_BITRATE_MODE 1
	float fMaxFrameRate;
} ohParamBase; // SEncParamBase

typedef struct {
	int iColorFormat; // videoFormatI420 23
	int iStride[4];
	unsigned char *pData[4];
	int iPicWidth;
	int iPicHeight;
	long long uiTimeStamp;
	bool bPsnrY, bPsnrU, bPsnrV; // 2.6+; older versions stop before these
} ohSource; // SSourcePicture

#define OH_LAYER_FIELDS                                                                                                    \
	unsigned char uiTemporalId, uiSpatialId, uiQualityId;                                                              \
	int eFrameType;                                                                                                    \
	unsigned char uiLayerType;                                                                                         \
	int iSubSeqId;                                                                                                     \
	int iNalCount;                                                                                                     \
	int *pNalLengthInByte;                                                                                             \
	unsigned char *pBsBuf;

typedef struct {
	OH_LAYER_FIELDS
} ohLayer5; // SLayerBSInfo before 2.6
typedef struct {
	OH_LAYER_FIELDS
	float rPsnr[3];
} ohLayer6; // SLayerBSInfo since 2.6

typedef struct {
	int iLayerNum;
	ohLayer5 sLayerInfo[128];
	int eFrameType;
	int iFrameSizeInBytes;
	long long uiTimeStamp;
} ohFrame5;
typedef struct {
	int iLayerNum;
	ohLayer6 sLayerInfo[128];
	int eFrameType;
	int iFrameSizeInBytes;
	long long uiTimeStamp;
} ohFrame6;

typedef struct ohVtbl ohVtbl;
typedef const ohVtbl *ohEncoder; // ISVCEncoder
struct ohVtbl {
	int (*Initialize)(ohEncoder *, const ohParamBase *);
	int (*InitializeExt)(ohEncoder *, const void *);
	int (*GetDefaultParams)(ohEncoder *, void *);
	int (*Uninitialize)(ohEncoder *);
	int (*EncodeFrame)(ohEncoder *, const ohSource *, void *);
	int (*EncodeParameterSets)(ohEncoder *, void *);
	int (*ForceIntraFrame)(ohEncoder *, bool);
	int (*SetOption)(ohEncoder *, int, void *);
	int (*GetOption)(ohEncoder *, int, void *);
};

enum { optDataFormat = 0, optIDRInterval = 1, optBitrate = 5, optTraceLevel = 25 };

static int (*createEnc)(ohEncoder **);
static void (*destroyEnc)(ohEncoder *);
static int layout6;
static unsigned int version[4];

int clack_oh_load(char *err, int n) {
	static const char *names[] = {"libopenh264.so.8", "libopenh264.so.7", "libopenh264.so.6", "libopenh264.so.5",
	                              "libopenh264.so.4", "libopenh264.so", NULL};
	void *lib = NULL;
	for (int i = 0; names[i] != NULL && lib == NULL; i++) {
		lib = dlopen(names[i], RTLD_NOW | RTLD_LOCAL);
	}
	if (lib == NULL) {
		snprintf(err, n, "OpenH264 isn't installed (libopenh264)");
		return -1;
	}
	createEnc = dlsym(lib, "WelsCreateSVCEncoder");
	destroyEnc = dlsym(lib, "WelsDestroySVCEncoder");
	void (*versionEx)(unsigned int *) = dlsym(lib, "WelsGetCodecVersionEx");
	if (createEnc == NULL || destroyEnc == NULL || versionEx == NULL) {
		snprintf(err, n, "libopenh264 lacks WelsCreateSVCEncoder");
		createEnc = NULL;
		return -1;
	}
	versionEx(version);
	layout6 = version[0] > 2 || (version[0] == 2 && version[1] >= 6);
	return 0;
}

void clack_oh_version(int *v) {
	for (int i = 0; i < 3; i++) {
		v[i] = (int)version[i];
	}
}

typedef struct {
	ohEncoder *e;
	int w, h;
	uint8_t *i420;
	void *info; // ohFrame5 or ohFrame6
	uint8_t *out;
	size_t cap;
} clackOH;

void *clack_oh_new(int w, int h, int fps, int bps, int screen, int *status) {
	clackOH *c = calloc(1, sizeof *c);
	if (createEnc(&c->e) != 0 || c->e == NULL) {
		*status = -1;
		free(c);
		return NULL;
	}
	ohParamBase p = {screen ? 1 : 0, w, h, bps, 1, (float)fps};
	int st = (*c->e)->Initialize(c->e, &p);
	if (st != 0) {
		destroyEnc(c->e);
		free(c);
		*status = st;
		return NULL;
	}
	int fmt = 23, idr = 0, quiet = 0;
	(*c->e)->SetOption(c->e, optDataFormat, &fmt);
	(*c->e)->SetOption(c->e, optIDRInterval, &idr); // keyframes on request (Chime's PLI)
	(*c->e)->SetOption(c->e, optTraceLevel, &quiet);
	c->w = w;
	c->h = h;
	c->i420 = malloc((size_t)w * h * 3 / 2);
	c->info = calloc(1, layout6 ? sizeof(ohFrame6) : sizeof(ohFrame5));
	*status = 0;
	return c;
}

static void put(clackOH *c, size_t *len, const uint8_t *p, size_t n) {
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

// clack_oh_encode encodes one NV12 picture; *out/*n is the access unit
// (Annex B, the parameter sets before an IDR), *n 0 when rate control
// skipped it.
int clack_oh_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, long long ms, int key, uint8_t **out, int *n,
                    int *idr) {
	clackOH *c = p;
	int w = c->w, h = c->h;
	uint8_t *Y = c->i420, *U = Y + (size_t)w * h, *V = U + (size_t)w * h / 4;
	for (int r = 0; r < h; r++) {
		memcpy(Y + (size_t)r * w, y + (size_t)r * ys, w);
	}
	for (int r = 0; r < h / 2; r++) {
		const uint8_t *s = uv + (size_t)r * uvs;
		uint8_t *u = U + (size_t)r * (w / 2), *v = V + (size_t)r * (w / 2);
		for (int x = 0; x < w / 2; x++) {
			u[x] = s[2 * x];
			v[x] = s[2 * x + 1];
		}
	}
	if (key) {
		(*c->e)->ForceIntraFrame(c->e, true);
	}
	ohSource src;
	memset(&src, 0, sizeof src);
	src.iColorFormat = 23;
	src.iStride[0] = w;
	src.iStride[1] = src.iStride[2] = w / 2;
	src.pData[0] = Y;
	src.pData[1] = U;
	src.pData[2] = V;
	src.iPicWidth = w;
	src.iPicHeight = h;
	src.uiTimeStamp = ms;
	memset(c->info, 0, layout6 ? sizeof(ohFrame6) : sizeof(ohFrame5));
	int st = (*c->e)->EncodeFrame(c->e, &src, c->info);
	if (st != 0) {
		return st;
	}
	size_t len = 0;
	int layers = layout6 ? ((ohFrame6 *)c->info)->iLayerNum : ((ohFrame5 *)c->info)->iLayerNum;
	int ftype = layout6 ? ((ohFrame6 *)c->info)->eFrameType : ((ohFrame5 *)c->info)->eFrameType;
	for (int l = 0; l < layers && l < 128; l++) {
		int nals = layout6 ? ((ohFrame6 *)c->info)->sLayerInfo[l].iNalCount : ((ohFrame5 *)c->info)->sLayerInfo[l].iNalCount;
		int *lens = layout6 ? ((ohFrame6 *)c->info)->sLayerInfo[l].pNalLengthInByte : ((ohFrame5 *)c->info)->sLayerInfo[l].pNalLengthInByte;
		uint8_t *bs = layout6 ? ((ohFrame6 *)c->info)->sLayerInfo[l].pBsBuf : ((ohFrame5 *)c->info)->sLayerInfo[l].pBsBuf;
		size_t sz = 0;
		for (int i = 0; i < nals; i++) {
			sz += lens[i];
		}
		if (sz > 0 && bs != NULL) {
			put(c, &len, bs, sz);
		}
	}
	*out = c->out;
	*n = (int)len;
	*idr = ftype == 1 || ftype == 2; // videoFrameTypeIDR, videoFrameTypeI
	return 0;
}

void clack_oh_bitrate(void *p, int bps) {
	clackOH *c = p;
	struct {
		int iLayer; // SPATIAL_LAYER_ALL 4
		int iBitrate;
	} b = {4, bps};
	(*c->e)->SetOption(c->e, optBitrate, &b);
}

void clack_oh_free(void *p) {
	clackOH *c = p;
	(*c->e)->Uninitialize(c->e);
	destroyEnc(c->e);
	free(c->i420);
	free(c->info);
	free(c->out);
	free(c);
}
