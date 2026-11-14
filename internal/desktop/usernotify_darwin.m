//go:build darwin && cgo

// Manual retain/release, as the package's other Objective-C (cgo's CFLAGS
// are the whole package's, and dock_darwin.go and dnd_darwin.go release
// what they make). Everything made here is autoreleased inside the call's
// pool except what's noted; the completion blocks the center copies retain
// what they capture.

#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>
#import <objc/message.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void clackUNAuthDone(uintptr_t h, int granted, char *domain, long code, char *desc);
extern void clackUNStatusDone(uintptr_t h, int status);
extern void clackUNShowDone(uintptr_t h, char *domain, long code, char *desc);
extern void clackUNResponse(char *ident, char *category, char *action, char *text, int textInput);

static char *cstr(NSString *s) { return (char *)(s ? s.UTF8String : ""); }

// clack_un_available: an app bundle LaunchServices has a proxy for, which
// is what currentNotificationCenter wants (it raises "bundleProxyForCurrentProcess
// is nil" and aborts without one). Where the private API to ask isn't
// there, the bundle's id and .app path stand in.
int clack_un_available(void) {
	@autoreleasepool {
		NSBundle *b = [NSBundle mainBundle];
		if ([b bundleIdentifier] == nil || ![[[b bundlePath] pathExtension] isEqualToString:@"app"]) {
			return 0;
		}
		Class c = NSClassFromString(@"LSBundleProxy");
		SEL s = NSSelectorFromString(@"bundleProxyForCurrentProcess");
		if (c != nil && [c respondsToSelector:s]) {
			return ((id (*)(Class, SEL))objc_msgSend)(c, s) != nil;
		}
		return 1;
	}
}

// ClackUNDelegate is Electron's NotificationCenterDelegate.
@interface ClackUNDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation ClackUNDelegate
// Banners while Clack is the frontmost app.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))done {
	done(UNNotificationPresentationOptionList | UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionSound);
}

// The response, to Go (desktop.userNotificationEvent sorts it out).
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
    didReceiveNotificationResponse:(UNNotificationResponse *)response
             withCompletionHandler:(void (^)(void))done {
	@autoreleasepool {
		NSString *text = @"";
		int input = 0;
		if ([response isKindOfClass:[UNTextInputNotificationResponse class]]) {
			NSString *t = [(UNTextInputNotificationResponse *)response userText];
			text = t ? t : @"";
			input = 1;
		}
		UNNotificationRequest *req = [[response notification] request];
		clackUNResponse(cstr([req identifier]), cstr([[req content] categoryIdentifier]), cstr([response actionIdentifier]), cstr(text), input);
	}
	done();
}
@end

static ClackUNDelegate *unDelegate; // kept for good: the center holds its delegate weakly

void clack_un_set_delegate(void) {
	@autoreleasepool {
		@synchronized([ClackUNDelegate class]) {
			if (unDelegate == nil) {
				unDelegate = [[ClackUNDelegate alloc] init];
				[[UNUserNotificationCenter currentNotificationCenter] setDelegate:unDelegate];
			}
		}
	}
}

// NotificationPresenterMac's request: alerts, sounds and badges.
void clack_un_request(uintptr_t h) {
	@autoreleasepool {
		[[UNUserNotificationCenter currentNotificationCenter]
		    requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound | UNAuthorizationOptionBadge)
		                  completionHandler:^(BOOL granted, NSError *err) {
			                  clackUNAuthDone(h, granted ? 1 : 0, err ? cstr([err domain]) : NULL, err ? (long)[err code] : 0,
			                                  err ? cstr([err localizedDescription]) : NULL);
		                  }];
	}
}

void clack_un_status(uintptr_t h) {
	@autoreleasepool {
		[[UNUserNotificationCenter currentNotificationCenter] getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *s) {
			clackUNStatusDone(h, (int)[s authorizationStatus]);
		}];
	}
}

static NSString *jsonString(NSDictionary *o, NSString *k) {
	id v = [o objectForKey:k];
	return [v isKindOfClass:[NSString class]] ? v : @"";
}

static BOOL jsonBool(NSDictionary *o, NSString *k) {
	id v = [o objectForKey:k];
	return [v isKindOfClass:[NSNumber class]] && [v boolValue];
}

// unAdd is ScheduleNotification's addNotificationRequest.
static void unAdd(UNUserNotificationCenter *center, UNNotificationRequest *req, uintptr_t h) {
	[center addNotificationRequest:req
	         withCompletionHandler:^(NSError *err) {
		         clackUNShowDone(h, err ? cstr([err domain]) : NULL, err ? (long)[err code] : 0, err ? cstr([err localizedDescription]) : NULL);
	         }];
}

// clack_un_show is CocoaNotification::Show and ScheduleNotification for
// the options in json (desktop.UserNotification). The category goes in
// before the request: setNotificationCategories and addNotificationRequest
// travel in order on the center's connection, so the buttons are there
// when the notification is.
void clack_un_show(const char *json, uintptr_t h) {
	@autoreleasepool {
		id parsed = [NSJSONSerialization JSONObjectWithData:[NSData dataWithBytes:json length:strlen(json)] options:0 error:nil];
		if (![parsed isKindOfClass:[NSDictionary class]]) {
			clackUNShowDone(h, "Clack", 1, "the notification's options didn't parse");
			return;
		}
		NSDictionary *o = parsed;
		UNMutableNotificationContent *content = [[[UNMutableNotificationContent alloc] init] autorelease];
		[content setTitle:jsonString(o, @"title")];
		[content setSubtitle:jsonString(o, @"subtitle")];
		[content setBody:jsonString(o, @"body")];
		NSString *group = jsonString(o, @"groupId");
		if ([group length] > 0) {
			[content setThreadIdentifier:group];
		}
		NSString *sound = jsonString(o, @"sound");
		if (jsonBool(o, @"silent")) {
			[content setSound:nil];
		} else if ([sound length] == 0) {
			[content setSound:[UNNotificationSound defaultSound]];
		} else {
			[content setSound:[UNNotificationSound soundNamed:sound]];
		}

		id texts = [o objectForKey:@"actions"];
		if (![texts isKindOfClass:[NSArray class]]) {
			texts = @[];
		}
		BOOL reply = jsonBool(o, @"hasReply");
		NSString *close = jsonString(o, @"closeButtonText");
		UNNotificationCategory *category = nil;
		if (reply || [texts count] > 0 || [close length] > 0) {
			NSMutableArray *actions = [NSMutableArray array];
			NSMutableString *actionString = [NSMutableString string];
			if (reply) {
				NSString *title = @"Reply"; // IDS_MAC_NOTIFICATION_INLINE_REPLY_BUTTON
				NSString *placeholder = jsonString(o, @"replyPlaceholder");
				[actions addObject:[UNTextInputNotificationAction actionWithIdentifier:@"REPLY_ACTION"
				                                                                 title:title
				                                                               options:UNNotificationActionOptionNone
				                                                  textInputButtonTitle:title
				                                                  textInputPlaceholder:placeholder]];
				[actionString appendFormat:@"REPLY_%@_%@", title, placeholder];
			}
			int i = 0;
			for (id t in texts) {
				NSString *text = ([t isKindOfClass:[NSString class]] && [(NSString *)t length] > 0) ? t : @"Show"; // IDS_NOTIFICATION_SHOW_BUTTON
				[actions addObject:[UNNotificationAction actionWithIdentifier:[NSString stringWithFormat:@"ACTION_%d", i]
				                                                        title:text
				                                                      options:UNNotificationActionOptionNone]];
				[actionString appendFormat:@"ACTION_%d_%@", i, text];
				i++;
			}
			if ([close length] > 0) {
				[actions addObject:[UNNotificationAction actionWithIdentifier:@"CLOSE_ACTION" title:close options:UNNotificationActionOptionNone]];
				[actionString appendFormat:@"CLOSE_%@", close];
			}
			NSString *cid = [NSString stringWithFormat:@"CATEGORY_%lu", (unsigned long)[actionString hash]];
			category = [UNNotificationCategory categoryWithIdentifier:cid
			                                                  actions:actions
			                                        intentIdentifiers:@[]
			                                                  options:UNNotificationCategoryOptionCustomDismissAction];
			[content setCategoryIdentifier:cid];
		}

		NSString *ident = jsonString(o, @"id");
		if ([ident length] == 0) {
			ident = [[NSUUID UUID] UUIDString];
		}
		UNNotificationRequest *req = [UNNotificationRequest requestWithIdentifier:ident content:content trigger:nil];
		UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
		if (category == nil) {
			unAdd(center, req, h);
			return;
		}
		[center getNotificationCategoriesWithCompletionHandler:^(NSSet<UNNotificationCategory *> *existing) {
			if (![existing containsObject:category]) {
				NSMutableSet *updated = existing ? [existing mutableCopy] : [[NSMutableSet alloc] init]; // +1
				[updated addObject:category];
				[center setNotificationCategories:updated];
				[updated release];
			}
			unAdd(center, req, h);
		}];
	}
}
