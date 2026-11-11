//go:build linux && cgo

// Screen capture on X11 (share_x11_linux.go): one Xlib connection of its
// own, MIT-SHM grabs of the root window (a monitor's area) or of one
// window, the cursor composited in from XFixes, converted to NV12 with an
// area-average downscale when the source is larger than asked for.
// libXext (MIT-SHM), libXrandr (monitors) and libXfixes (the cursor) are
// loaded at run time and declared below, so building needs only Xlib's
// headers; without them capture falls back to XGetImage, the whole root
// as one screen, and no cursor.

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ipc.h>
#include <sys/shm.h>

typedef struct {
	unsigned long shmseg;
	int shmid;
	char *shmaddr;
	Bool readOnly;
} clackShmInfo; // XShmSegmentInfo

typedef struct {
	Atom name;
	Bool primary, automatic;
	int noutput;
	int x, y, width, height, mwidth, mheight;
	unsigned long *outputs;
} clackMonitor; // XRRMonitorInfo

typedef struct {
	short x, y;
	unsigned short width, height, xhot, yhot;
	unsigned long cursor_serial;
	unsigned long *pixels;
	Atom atom;
	const char *name;
} clackCursor; // XFixesCursorImage

static Bool (*shmQuery)(Display *);
static XImage *(*shmCreate)(Display *, Visual *, unsigned int, int, char *, clackShmInfo *, unsigned int, unsigned int);
static Bool (*shmAttach)(Display *, clackShmInfo *);
static Bool (*shmDetach)(Display *, clackShmInfo *);
static Bool (*shmGet)(Display *, Drawable, XImage *, int, int, unsigned long);
static clackMonitor *(*rrMonitors)(Display *, Window, Bool, int *);
static void (*rrFree)(clackMonitor *);
static clackCursor *(*fixesCursor)(Display *);

static Display *dpy;
static int haveShm;
static int lastErr;
static int (*prevHandler)(Display *, XErrorEvent *);

// onXError records errors on this connection (a window that went away)
// instead of Xlib's default, which exits; other connections' errors go
// to whatever handler was there.
static int onXError(Display *d, XErrorEvent *e) {
	if (d == dpy) {
		lastErr = e->error_code ? e->error_code : 1;
		return 0;
	}
	return prevHandler ? prevHandler(d, e) : 0;
}

static void *sym(void *lib, const char *name) { return lib ? dlsym(lib, name) : NULL; }

int clack_x11_open(char *err, int n) {
	if (dpy != NULL) {
		return 0;
	}
	Display *d = XOpenDisplay(NULL);
	if (d == NULL) {
		snprintf(err, n, "can't open the X display");
		return -1;
	}
	void *xext = dlopen("libXext.so.6", RTLD_NOW | RTLD_LOCAL);
	void *xrr = dlopen("libXrandr.so.2", RTLD_NOW | RTLD_LOCAL);
	void *xfix = dlopen("libXfixes.so.3", RTLD_NOW | RTLD_LOCAL);
	shmQuery = sym(xext, "XShmQueryExtension");
	shmCreate = sym(xext, "XShmCreateImage");
	shmAttach = sym(xext, "XShmAttach");
	shmDetach = sym(xext, "XShmDetach");
	shmGet = sym(xext, "XShmGetImage");
	rrMonitors = sym(xrr, "XRRGetMonitors");
	rrFree = sym(xrr, "XRRFreeMonitors");
	fixesCursor = sym(xfix, "XFixesGetCursorImage");
	dpy = d;
	prevHandler = XSetErrorHandler(onXError);
	haveShm = shmQuery && shmCreate && shmAttach && shmDetach && shmGet && shmQuery(dpy);
	return 0;
}

// clack_x11_monitors writes x, y, w, h for each monitor (RandR 1.5, or
// the root window) and returns how many.
int clack_x11_monitors(int *out, int max) {
	Window root = DefaultRootWindow(dpy);
	int n = 0;
	if (rrMonitors && rrFree) {
		int cnt = 0;
		clackMonitor *m = rrMonitors(dpy, root, True, &cnt);
		for (int i = 0; m != NULL && i < cnt && n < max; i++) {
			if (m[i].width > 0 && m[i].height > 0) {
				out[4 * n] = m[i].x;
				out[4 * n + 1] = m[i].y;
				out[4 * n + 2] = m[i].width;
				out[4 * n + 3] = m[i].height;
				n++;
			}
		}
		if (m != NULL) {
			rrFree(m);
		}
	}
	if (n == 0 && max > 0) {
		XWindowAttributes a;
		if (XGetWindowAttributes(dpy, root, &a)) {
			out[0] = 0;
			out[1] = 0;
			out[2] = a.width;
			out[3] = a.height;
			n = 1;
		}
	}
	return n;
}

static Atom atom(const char *name) { return XInternAtom(dpy, name, False); }

// prop reads a property's items (caller frees).
static unsigned char *prop(Window w, Atom name, Atom type, unsigned long *count) {
	Atom got;
	int fmt;
	unsigned long after;
	unsigned char *data = NULL;
	*count = 0;
	if (XGetWindowProperty(dpy, w, name, 0, 1 << 20, False, type, &got, &fmt, count, &after, &data) != Success) {
		return NULL;
	}
	if (data != NULL && got == None) {
		XFree(data);
		return NULL;
	}
	return data;
}

// clack_x11_windows lists the window manager's client windows, top
// first, that a picker shows: viewable, and normal or dialog windows
// (_NET_WM_WINDOW_TYPE), like Chromium's X11 window list.
int clack_x11_windows(unsigned long *ids, int max) {
	lastErr = 0;
	Window root = DefaultRootWindow(dpy);
	unsigned long cnt = 0;
	Window *list = (Window *)prop(root, atom("_NET_CLIENT_LIST_STACKING"), XA_WINDOW, &cnt);
	if (list == NULL) {
		list = (Window *)prop(root, atom("_NET_CLIENT_LIST"), XA_WINDOW, &cnt);
	}
	if (list == NULL) {
		return 0;
	}
	Atom typeAtom = atom("_NET_WM_WINDOW_TYPE"), normal = atom("_NET_WM_WINDOW_TYPE_NORMAL"),
	     dialog = atom("_NET_WM_WINDOW_TYPE_DIALOG");
	int n = 0;
	for (long i = (long)cnt - 1; i >= 0 && n < max; i--) {
		Window w = list[i];
		XWindowAttributes a;
		lastErr = 0;
		if (!XGetWindowAttributes(dpy, w, &a) || lastErr || a.map_state != IsViewable || a.width < 16 || a.height < 16) {
			continue;
		}
		unsigned long tc = 0;
		Atom *types = (Atom *)prop(w, typeAtom, XA_ATOM, &tc);
		int ok = types == NULL || tc == 0;
		for (unsigned long t = 0; types != NULL && t < tc; t++) {
			if (types[t] == normal || types[t] == dialog) {
				ok = 1;
			}
		}
		if (types != NULL) {
			XFree(types);
		}
		if (ok) {
			ids[n++] = w;
		}
	}
	XFree(list);
	return n;
}

// clack_x11_window_info fills a window's title (_NET_WM_NAME, else
// WM_NAME), its class (WM_CLASS) and its frame in root coordinates.
int clack_x11_window_info(unsigned long w, char *name, int nn, char *cls, int cn, int *x, int *y, int *wd, int *ht) {
	lastErr = 0;
	name[0] = cls[0] = 0;
	XWindowAttributes a;
	if (!XGetWindowAttributes(dpy, w, &a) || lastErr) {
		return -1;
	}
	Window child;
	int rx = 0, ry = 0;
	XTranslateCoordinates(dpy, w, DefaultRootWindow(dpy), 0, 0, &rx, &ry, &child);
	*x = rx;
	*y = ry;
	*wd = a.width;
	*ht = a.height;
	unsigned long len = 0;
	unsigned char *t = prop(w, atom("_NET_WM_NAME"), atom("UTF8_STRING"), &len);
	if (t != NULL) {
		snprintf(name, nn, "%.*s", (int)len, (char *)t);
		XFree(t);
	} else {
		char *wn = NULL;
		if (XFetchName(dpy, w, &wn) && wn != NULL) {
			snprintf(name, nn, "%s", wn);
			XFree(wn);
		}
	}
	XClassHint ch;
	if (XGetClassHint(dpy, w, &ch)) {
		if (ch.res_class != NULL) {
			snprintf(cls, cn, "%s", ch.res_class);
		}
		if (ch.res_name != NULL) {
			XFree(ch.res_name);
		}
		if (ch.res_class != NULL) {
			XFree(ch.res_class);
		}
	}
	return lastErr ? -1 : 0;
}

static int truecolor32(XImage *img) {
	return img->bits_per_pixel == 32 && img->red_mask == 0xff0000 && img->green_mask == 0xff00 && img->blue_mask == 0xff &&
	       img->byte_order == LSBFirst;
}

static uint8_t clamp8(int v) { return v < 0 ? 0 : v > 255 ? 255 : (uint8_t)v; }

// toNV12 converts a BGRX picture (sw×sh, stride ss) into NV12 at dw×dh
// (even), averaging the source area under each pixel when scaling down.
static void toNV12(const uint8_t *src, int ss, int sw, int sh, uint8_t *y, uint8_t *uv, int dw, int dh) {
	for (int dy = 0; dy < dh; dy += 2) {
		for (int dx = 0; dx < dw; dx += 2) {
			int rs = 0, gs = 0, bs = 0;
			for (int k = 0; k < 4; k++) {
				int px = dx + (k & 1), py = dy + (k >> 1);
				int x0 = px * sw / dw, x1 = (px + 1) * sw / dw, y0 = py * sh / dh, y1 = (py + 1) * sh / dh;
				if (x1 <= x0) {
					x1 = x0 + 1;
				}
				if (y1 <= y0) {
					y1 = y0 + 1;
				}
				int r = 0, g = 0, b = 0, cnt = 0;
				for (int yy = y0; yy < y1; yy++) {
					const uint8_t *row = src + (size_t)yy * ss;
					for (int xx = x0; xx < x1; xx++) {
						b += row[4 * xx];
						g += row[4 * xx + 1];
						r += row[4 * xx + 2];
						cnt++;
					}
				}
				r /= cnt;
				g /= cnt;
				b /= cnt;
				y[(size_t)py * dw + px] = (uint8_t)((66 * r + 129 * g + 25 * b + 128 + (16 << 8)) >> 8);
				rs += r;
				gs += g;
				bs += b;
			}
			rs /= 4;
			gs /= 4;
			bs /= 4;
			uv[(size_t)(dy / 2) * dw + dx] = clamp8((-38 * rs - 74 * gs + 112 * bs + 128 + (128 << 8)) >> 8);
			uv[(size_t)(dy / 2) * dw + dx + 1] = clamp8((112 * rs - 94 * gs - 18 * bs + 128 + (128 << 8)) >> 8);
		}
	}
}

// blendCursor draws the pointer (XFixes, premultiplied ARGB) into a BGRX
// picture whose top-left is at ox, oy on the root window.
static void blendCursor(uint8_t *pix, int stride, int w, int h, int ox, int oy) {
	if (fixesCursor == NULL) {
		return;
	}
	clackCursor *c = fixesCursor(dpy);
	if (c == NULL) {
		return;
	}
	int cx = c->x - c->xhot - ox, cy = c->y - c->yhot - oy;
	for (int j = 0; j < c->height; j++) {
		int py = cy + j;
		if (py < 0 || py >= h) {
			continue;
		}
		for (int i = 0; i < c->width; i++) {
			int px = cx + i;
			if (px < 0 || px >= w) {
				continue;
			}
			unsigned long p = c->pixels[j * c->width + i];
			int a = (p >> 24) & 0xff;
			if (a == 0) {
				continue;
			}
			uint8_t *d = pix + (size_t)py * stride + 4 * px;
			d[0] = (uint8_t)((p & 0xff) + d[0] * (255 - a) / 255);
			d[1] = (uint8_t)(((p >> 8) & 0xff) + d[1] * (255 - a) / 255);
			d[2] = (uint8_t)(((p >> 16) & 0xff) + d[2] * (255 - a) / 255);
		}
	}
	XFree(c);
}

// clack_x11_thumb grabs a window (win) or a root area as RGBA scaled
// into tw×th (area average).
int clack_x11_thumb(unsigned long win, int x, int y, int w, int h, uint8_t *rgba, int tw, int th) {
	lastErr = 0;
	Drawable d = win ? win : DefaultRootWindow(dpy);
	XImage *img = XGetImage(dpy, d, win ? 0 : x, win ? 0 : y, w, h, AllPlanes, ZPixmap);
	XSync(dpy, False);
	if (img == NULL || lastErr) {
		if (img != NULL) {
			XDestroyImage(img);
		}
		return lastErr ? lastErr : -1;
	}
	if (!truecolor32(img)) {
		XDestroyImage(img);
		return -2;
	}
	for (int ty = 0; ty < th; ty++) {
		int y0 = ty * h / th, y1 = (ty + 1) * h / th;
		if (y1 <= y0) {
			y1 = y0 + 1;
		}
		for (int tx = 0; tx < tw; tx++) {
			int x0 = tx * w / tw, x1 = (tx + 1) * w / tw;
			if (x1 <= x0) {
				x1 = x0 + 1;
			}
			int r = 0, g = 0, b = 0, cnt = 0;
			for (int yy = y0; yy < y1; yy++) {
				const uint8_t *row = (uint8_t *)img->data + (size_t)yy * img->bytes_per_line;
				for (int xx = x0; xx < x1; xx++) {
					b += row[4 * xx];
					g += row[4 * xx + 1];
					r += row[4 * xx + 2];
					cnt++;
				}
			}
			uint8_t *o = rgba + ((size_t)ty * tw + tx) * 4;
			o[0] = r / cnt;
			o[1] = g / cnt;
			o[2] = b / cnt;
			o[3] = 255;
		}
	}
	XDestroyImage(img);
	return 0;
}

typedef struct {
	Window win; // 0: the root area at x, y
	int x, y, w, h;
	XImage *img;
	clackShmInfo shm;
	int cursor;
} clackX11Cap;

static void freeImage(clackX11Cap *c) {
	if (c->img == NULL) {
		return;
	}
	if (c->shm.shmaddr != NULL) {
		shmDetach(dpy, &c->shm);
		XSync(dpy, False);
		c->img->data = NULL;
		XDestroyImage(c->img);
		shmdt(c->shm.shmaddr);
		c->shm.shmaddr = NULL;
	} else {
		XDestroyImage(c->img);
	}
	c->img = NULL;
}

void *clack_x11_cap_new(unsigned long win, int x, int y, int w, int h, int cursor) {
	clackX11Cap *c = calloc(1, sizeof *c);
	c->win = win;
	c->x = x;
	c->y = y;
	c->w = w;
	c->h = h;
	c->cursor = cursor;
	return c;
}

// makeImage sets up the MIT-SHM image for the current size.
static int makeImage(clackX11Cap *c) {
	freeImage(c);
	if (!haveShm) {
		return 0;
	}
	int scr = DefaultScreen(dpy);
	XImage *img = shmCreate(dpy, DefaultVisual(dpy, scr), DefaultDepth(dpy, scr), ZPixmap, NULL, &c->shm, c->w, c->h);
	if (img == NULL) {
		return -1;
	}
	c->shm.shmid = shmget(IPC_PRIVATE, (size_t)img->bytes_per_line * img->height, IPC_CREAT | 0600);
	if (c->shm.shmid < 0) {
		XDestroyImage(img);
		return -1;
	}
	c->shm.shmaddr = img->data = shmat(c->shm.shmid, NULL, 0);
	c->shm.readOnly = False;
	lastErr = 0;
	if (c->shm.shmaddr == (char *)-1 || !shmAttach(dpy, &c->shm)) {
		shmctl(c->shm.shmid, IPC_RMID, NULL);
		c->shm.shmaddr = NULL;
		img->data = NULL;
		XDestroyImage(img);
		return -1;
	}
	XSync(dpy, False);
	shmctl(c->shm.shmid, IPC_RMID, NULL); // freed once both sides detach
	if (lastErr) {
		shmdt(c->shm.shmaddr);
		c->shm.shmaddr = NULL;
		img->data = NULL;
		XDestroyImage(img);
		return -1;
	}
	c->img = img;
	return 0;
}

// clack_x11_cap_grab grabs one picture into NV12 at dw×dh. It returns 0;
// 1 when a window's size changed (*nw, *nh: the new size; nothing
// grabbed); or an X error (the window is gone) or -1.
int clack_x11_cap_grab(void *p, uint8_t *y, uint8_t *uv, int dw, int dh, int *nw, int *nh) {
	clackX11Cap *c = p;
	lastErr = 0;
	int ox = c->x, oy = c->y;
	if (c->win) {
		XWindowAttributes a;
		if (!XGetWindowAttributes(dpy, c->win, &a) || lastErr) {
			return lastErr ? lastErr : -1;
		}
		Window child;
		XTranslateCoordinates(dpy, c->win, DefaultRootWindow(dpy), 0, 0, &ox, &oy, &child);
		if (a.width != c->w || a.height != c->h) {
			c->w = a.width;
			c->h = a.height;
			freeImage(c);
			*nw = a.width;
			*nh = a.height;
			return 1;
		}
	}
	Drawable d = c->win ? c->win : DefaultRootWindow(dpy);
	int gx = c->win ? 0 : c->x, gy = c->win ? 0 : c->y;
	XImage *img = NULL;
	if (haveShm && c->img == NULL) {
		makeImage(c);
	}
	if (c->img != NULL) {
		lastErr = 0;
		if (!shmGet(dpy, d, c->img, gx, gy, AllPlanes)) {
			XSync(dpy, False);
			return lastErr ? lastErr : -1;
		}
		img = c->img;
	} else {
		img = XGetImage(dpy, d, gx, gy, c->w, c->h, AllPlanes, ZPixmap);
		XSync(dpy, False);
		if (img == NULL || lastErr) {
			if (img != NULL) {
				XDestroyImage(img);
			}
			return lastErr ? lastErr : -1;
		}
	}
	if (!truecolor32(img)) {
		if (img != c->img) {
			XDestroyImage(img);
		}
		return -2;
	}
	if (c->cursor) {
		blendCursor((uint8_t *)img->data, img->bytes_per_line, c->w, c->h, ox, oy);
	}
	toNV12((uint8_t *)img->data, img->bytes_per_line, c->w, c->h, y, uv, dw, dh);
	if (img != c->img) {
		XDestroyImage(img);
	}
	return 0;
}

void clack_x11_cap_free(void *p) {
	clackX11Cap *c = p;
	freeImage(c);
	free(c);
}
