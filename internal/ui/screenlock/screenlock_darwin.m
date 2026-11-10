//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <stdint.h>

extern void clackScreenLocked(uintptr_t h, int locked);

// clack_screenlock_observe registers for the lock screen's distributed
// notifications; the returned object holds both observers.
void *clack_screenlock_observe(uintptr_t h) {
	NSDistributedNotificationCenter *nc = [NSDistributedNotificationCenter defaultCenter];
	id lock = [nc addObserverForName:@"com.apple.screenIsLocked" object:nil queue:nil
		usingBlock:^(NSNotification *n) { clackScreenLocked(h, 1); }];
	id unlock = [nc addObserverForName:@"com.apple.screenIsUnlocked" object:nil queue:nil
		usingBlock:^(NSNotification *n) { clackScreenLocked(h, 0); }];
	NSArray *both = @[lock, unlock];
	return (__bridge_retained void *)both;
}

void clack_screenlock_stop(void *obs) {
	NSArray *both = (__bridge_transfer NSArray *)obs;
	NSDistributedNotificationCenter *nc = [NSDistributedNotificationCenter defaultCenter];
	for (id o in both) {
		[nc removeObserver:o];
	}
}
