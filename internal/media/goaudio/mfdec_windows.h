// Media Foundation's Source Reader as Go audio's decoder of what Go can't
// decode (mfdec_windows.c).

#ifndef CLACK_MFDEC_H
#define CLACK_MFDEC_H

#include <stdint.h>

typedef struct clackDec clackDec;

// The calling thread's COM (the multithreaded apartment), for the
// decoder's own thread.
void clack_dec_thread_init(void);
void clack_dec_thread_done(void);

// clack_dec_open decodes n bytes of a file (copied: data needn't outlive
// the call), mime its type's hint (UTF-16, NUL-terminated; NULL none): its
// first audio stream to 32-bit float PCM at its own rate and channels.
// *dur is its length in 100 ns units (-1 unknown).
int32_t clack_dec_open(const void *data, uint32_t n, const uint16_t *mime, clackDec **out, uint32_t *rate, uint32_t *channels, int64_t *dur);

// clack_dec_read copies up to cap frames into dst: *frames of them, the
// first at *ts (100 ns units, -1 unknown); *eof once there are no more.
int32_t clack_dec_read(clackDec *d, float *dst, uint32_t cap, uint32_t *frames, int64_t *ts, int *eof);

// clack_dec_seek makes the next read start at the frame holding at (100 ns
// units).
int32_t clack_dec_seek(clackDec *d, int64_t at);

void clack_dec_close(clackDec *d);

#endif
