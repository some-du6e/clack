//go:build darwin && cgo

#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <Security/Security.h>
#import <UserNotifications/UserNotifications.h>
#import <objc/message.h>
#include <stdlib.h>
#include <string.h>

static char *dupstr(NSString *s) {
	const char *u = s.UTF8String;
	return strdup(u ? u : "");
}

static NSString *errText(NSError *e) {
	if (e == nil) {
		return @"error=nil";
	}
	NSError *under = e.userInfo[NSUnderlyingErrorKey];
	return [NSString stringWithFormat:@"error domain=%@ code=%ld desc=\"%@\"%@", e.domain, (long)e.code,
	                                  e.localizedDescription,
	                                  under ? [NSString stringWithFormat:@" underlying=(%@ %ld \"%@\")", under.domain, (long)under.code, under.localizedDescription] : @""];
}

// ClackProbeDelegate shows banners while the probe is the frontmost app,
// as Electron's NotificationCenterDelegate does, and keeps a log of what
// the center told it.
@interface ClackProbeDelegate : NSObject <UNUserNotificationCenterDelegate>
@property(atomic, copy) NSString *log;
@end

@implementation ClackProbeDelegate
- (void)note:(NSString *)s {
	@synchronized(self) {
		self.log = [NSString stringWithFormat:@"%@%@; ", self.log ?: @"", s];
	}
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))done {
	[self note:[NSString stringWithFormat:@"willPresent %@", notification.request.identifier]];
	done(UNNotificationPresentationOptionList | UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionSound);
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
    didReceiveNotificationResponse:(UNNotificationResponse *)response
             withCompletionHandler:(void (^)(void))done {
	[self note:[NSString stringWithFormat:@"response %@ to %@", response.actionIdentifier, response.notification.request.identifier]];
	done();
}
@end

static ClackProbeDelegate *probeDelegate;

// lsProxy says whether LaunchServices has a bundle proxy for this process,
// the thing currentNotificationCenter needs (it raises
// "bundleProxyForCurrentProcess is nil" without one): 1 yes, 0 no, -1 when
// the private API isn't there to ask.
static int lsProxy(void) {
	Class c = NSClassFromString(@"LSBundleProxy");
	SEL s = NSSelectorFromString(@"bundleProxyForCurrentProcess");
	if (c == nil || ![c respondsToSelector:s]) {
		return -1;
	}
	return ((id (*)(Class, SEL))objc_msgSend)(c, s) != nil;
}

// center is currentNotificationCenter. Outside an app bundle that raises
// NSInternalInconsistencyException inside a dispatch_once, which libdispatch
// turns into an abort that no @catch sees (ci run 43585762971, the bare
// variant), so it isn't called without a bundle proxy.
static UNUserNotificationCenter *center(NSString **why) {
	int proxy = lsProxy();
	NSBundle *b = NSBundle.mainBundle;
	if (proxy == 0 || (proxy < 0 && (b.bundleIdentifier == nil || ![b.bundlePath.pathExtension isEqualToString:@"app"]))) {
		if (why) {
			*why = [NSString stringWithFormat:@"no LaunchServices bundle proxy for this process (main bundle %@, id %@): currentNotificationCenter would abort",
			                                  b.bundlePath, b.bundleIdentifier ?: @"(none)"];
		}
		return nil;
	}
	UNUserNotificationCenter *c = [UNUserNotificationCenter currentNotificationCenter];
	if (c == nil) {
		if (why) {
			*why = @"currentNotificationCenter returned nil";
		}
		return nil;
	}
	if (probeDelegate == nil && c.delegate == nil) { // never over desktop's (TestShowUserNotificationInBundle)
		probeDelegate = [ClackProbeDelegate new];
		c.delegate = probeDelegate;
	}
	return c;
}

static NSString *sessionText(void) {
	CFDictionaryRef d = CGSessionCopyCurrentDictionary();
	if (d == NULL) {
		return @"window server session: none";
	}
	NSDictionary *s = CFBridgingRelease(d);
	return [NSString stringWithFormat:@"window server session: user=%@ onConsole=%@ loginDone=%@",
	                                  s[(__bridge NSString *)kCGSessionUserNameKey],
	                                  s[(__bridge NSString *)kCGSessionOnConsoleKey],
	                                  s[(__bridge NSString *)kCGSessionLoginDoneKey]];
}

static NSString *signatureText(void) {
	SecCodeRef code = NULL;
	OSStatus st = SecCodeCopySelf(kSecCSDefaultFlags, &code);
	if (st != errSecSuccess) {
		return [NSString stringWithFormat:@"SecCodeCopySelf=%d", (int)st];
	}
	NSMutableString *s = [NSMutableString string];
	CFDictionaryRef info = NULL;
	st = SecCodeCopySigningInformation((SecStaticCodeRef)code,
	                                   kSecCSSigningInformation | kSecCSRequirementInformation | kSecCSDynamicInformation, &info);
	if (st != errSecSuccess || info == NULL) {
		[s appendFormat:@"SecCodeCopySigningInformation=%d", (int)st];
	} else {
		NSDictionary *d = CFBridgingRelease(info);
		uint32_t flags = [d[(__bridge NSString *)kSecCodeInfoFlags] unsignedIntValue];
		NSString *dr = @"(none)";
		id req = d[(__bridge NSString *)kSecCodeInfoDesignatedRequirement];
		if (req != nil) {
			CFStringRef text = NULL;
			if (SecRequirementCopyString((__bridge SecRequirementRef)req, kSecCSDefaultFlags, &text) == errSecSuccess && text) {
				dr = CFBridgingRelease(text);
			}
		}
		[s appendFormat:@"signature identifier=%@ flags=0x%x adhoc=%d linkerSigned=%d runtime=%d team=%@ infoPlistBound=%d entitlements=%@ designated=%@",
		                d[(__bridge NSString *)kSecCodeInfoIdentifier], flags,
		                (flags & kSecCodeSignatureAdhoc) != 0, (flags & 0x20000) != 0, (flags & 0x10000) != 0,
		                d[(__bridge NSString *)kSecCodeInfoTeamIdentifier] ?: @"(none)",
		                d[(__bridge NSString *)kSecCodeInfoPList] != nil,
		                [[d[(__bridge NSString *)kSecCodeInfoEntitlementsDict] allKeys] componentsJoinedByString:@","] ?: @"(none)",
		                dr];
	}
	st = SecCodeCheckValidity(code, kSecCSDefaultFlags, NULL);
	[s appendFormat:@" valid=%d", (int)st];
	CFRelease(code);
	return s;
}

char *clack_unprobe_bundle(void) {
	@autoreleasepool {
		NSBundle *b = NSBundle.mainBundle;
		NSOperatingSystemVersion v = NSProcessInfo.processInfo.operatingSystemVersion;
		return dupstr([NSString stringWithFormat:@"macOS %ld.%ld.%ld; main bundle path=%@ id=%@ executable=%@ launchServicesProxy=%d; %@; %@",
		                                         (long)v.majorVersion, (long)v.minorVersion, (long)v.patchVersion,
		                                         b.bundlePath, b.bundleIdentifier ?: @"(none)", b.executablePath, lsProxy(),
		                                         sessionText(), signatureText()]);
	}
}

char *clack_unprobe_center(void) {
	@autoreleasepool {
		NSString *why = nil;
		center(&why);
		return dupstr(why ?: @"");
	}
}

static NSString *authName(UNAuthorizationStatus s) {
	switch (s) {
	case UNAuthorizationStatusNotDetermined:
		return @"notDetermined";
	case UNAuthorizationStatusDenied:
		return @"denied";
	case UNAuthorizationStatusAuthorized:
		return @"authorized";
	case UNAuthorizationStatusProvisional:
		return @"provisional";
	default:
		return [NSString stringWithFormat:@"%ld", (long)s];
	}
}

static NSString *settingsText(UNNotificationSettings *s) {
	NSMutableString *t = [NSMutableString stringWithFormat:@"authorizationStatus=%@ alert=%ld sound=%ld badge=%ld notificationCenter=%ld lockScreen=%ld alertStyle=%ld showPreviews=%ld critical=%ld",
	                                                       authName(s.authorizationStatus), (long)s.alertSetting, (long)s.soundSetting,
	                                                       (long)s.badgeSetting, (long)s.notificationCenterSetting, (long)s.lockScreenSetting,
	                                                       (long)s.alertStyle, (long)s.showPreviewsSetting, (long)s.criticalAlertSetting];
	if (@available(macOS 12.0, *)) {
		[t appendFormat:@" timeSensitive=%ld scheduledDelivery=%ld directMessages=%ld", (long)s.timeSensitiveSetting,
		                (long)s.scheduledDeliverySetting, (long)s.directMessagesSetting];
	}
	return t;
}

// waitFor waits secs for sem; the blocks that signal it keep their own
// references, so an answer after the timeout is harmless.
static BOOL waitFor(dispatch_semaphore_t sem, int secs) {
	return dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, (int64_t)secs * (int64_t)NSEC_PER_SEC)) == 0;
}

char *clack_unprobe_settings(int secs) {
	@autoreleasepool {
		NSString *why = nil;
		UNUserNotificationCenter *c = center(&why);
		if (c == nil) {
			return dupstr(why);
		}
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		NSMutableArray *box = [NSMutableArray array];
		[c getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *s) {
			@synchronized(box) {
				[box addObject:settingsText(s)];
			}
			dispatch_semaphore_signal(sem);
		}];
		if (!waitFor(sem, secs)) {
			return dupstr([NSString stringWithFormat:@"no settings in %ds", secs]);
		}
		@synchronized(box) {
			return dupstr(box.firstObject);
		}
	}
}

char *clack_unprobe_request(long opts, int secs) {
	@autoreleasepool {
		NSString *why = nil;
		UNUserNotificationCenter *c = center(&why);
		if (c == nil) {
			return dupstr(why);
		}
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		NSMutableArray *box = [NSMutableArray array];
		NSDate *t0 = [NSDate date];
		[c requestAuthorizationWithOptions:(UNAuthorizationOptions)opts
		                 completionHandler:^(BOOL granted, NSError *err) {
			                 NSString *r = [NSString stringWithFormat:@"granted=%s %@ (after %.1fs)", granted ? "YES" : "NO",
			                                                          errText(err), -[t0 timeIntervalSinceNow]];
			                 @synchronized(box) {
				                 [box addObject:r];
			                 }
			                 dispatch_semaphore_signal(sem);
		                 }];
		if (!waitFor(sem, secs)) {
			return dupstr([NSString stringWithFormat:@"no answer in %ds (macOS is showing its prompt, or usernoted never replied)", secs]);
		}
		@synchronized(box) {
			return dupstr(box.firstObject);
		}
	}
}

char *clack_unprobe_post(const char *ident, const char *title, const char *subtitle, const char *body, const char *thread, int category, int secs) {
	@autoreleasepool {
		NSString *why = nil;
		UNUserNotificationCenter *c = center(&why);
		if (c == nil) {
			return dupstr(why);
		}
		UNMutableNotificationContent *n = [UNMutableNotificationContent new];
		n.title = @(title);
		n.subtitle = @(subtitle);
		n.body = @(body);
		if (thread[0] != 0) {
			n.threadIdentifier = @(thread);
		}
		n.sound = UNNotificationSound.defaultSound;
		if (category) {
			// cocoa_notification.mm's shape: the reply box, the buttons as
			// ACTION_<i>, closeButtonText as CLOSE_ACTION.
			UNTextInputNotificationAction *reply = [UNTextInputNotificationAction actionWithIdentifier:@"REPLY_ACTION"
			                                                                                     title:@"Reply"
			                                                                                   options:UNNotificationActionOptionNone
			                                                                      textInputButtonTitle:@"Reply"
			                                                                      textInputPlaceholder:@"Reply…"];
			UNNotificationAction *mark = [UNNotificationAction actionWithIdentifier:@"ACTION_0" title:@"Mark as read" options:UNNotificationActionOptionNone];
			UNNotificationAction *close = [UNNotificationAction actionWithIdentifier:@"CLOSE_ACTION" title:@"Close" options:UNNotificationActionOptionNone];
			UNNotificationCategory *cat = [UNNotificationCategory categoryWithIdentifier:@"CATEGORY_PROBE"
			                                                                     actions:@[ reply, mark, close ]
			                                                           intentIdentifiers:@[]
			                                                                     options:UNNotificationCategoryOptionCustomDismissAction];
			[c setNotificationCategories:[NSSet setWithObject:cat]];
			n.categoryIdentifier = @"CATEGORY_PROBE";
		}
		UNNotificationRequest *req = [UNNotificationRequest requestWithIdentifier:@(ident) content:n trigger:nil];
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		NSMutableArray *box = [NSMutableArray array];
		NSDate *t0 = [NSDate date];
		[c addNotificationRequest:req
		    withCompletionHandler:^(NSError *err) {
			    NSString *r = [NSString stringWithFormat:@"%@ (after %.1fs)", err ? errText(err) : @"added", -[t0 timeIntervalSinceNow]];
			    @synchronized(box) {
				    [box addObject:r];
			    }
			    dispatch_semaphore_signal(sem);
		    }];
		if (!waitFor(sem, secs)) {
			return dupstr([NSString stringWithFormat:@"no answer in %ds", secs]);
		}
		@synchronized(box) {
			return dupstr(box.firstObject);
		}
	}
}

char *clack_unprobe_delivered(int secs) {
	@autoreleasepool {
		NSString *why = nil;
		UNUserNotificationCenter *c = center(&why);
		if (c == nil) {
			return dupstr(why);
		}
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		NSMutableArray *box = [NSMutableArray array];
		[c getDeliveredNotificationsWithCompletionHandler:^(NSArray<UNNotification *> *ns) {
			NSMutableString *s = [NSMutableString stringWithFormat:@"%lu delivered", (unsigned long)ns.count];
			for (UNNotification *n in ns) {
				UNNotificationContent *k = n.request.content;
				[s appendFormat:@" [id=%@ title=%@ subtitle=%@ body=%@ thread=%@ category=%@]", n.request.identifier, k.title,
				                k.subtitle, k.body, k.threadIdentifier, k.categoryIdentifier];
			}
			@synchronized(box) {
				[box addObject:s];
			}
			dispatch_semaphore_signal(sem);
		}];
		if (!waitFor(sem, secs)) {
			return dupstr([NSString stringWithFormat:@"no delivered list in %ds; delegate: %@", secs, probeDelegate.log ?: @"(nothing)"]);
		}
		@synchronized(box) {
			return dupstr([NSString stringWithFormat:@"%@; delegate: %@", box.firstObject, probeDelegate.log ?: @"(nothing)"]);
		}
	}
}

// ---- read-backs for TestShowUserNotificationInBundle ----

// jsonOut writes obj as JSON (booleans must be boxed BOOLs: a boxed C
// comparison is an int and comes out as 1).
static char *jsonOut(id obj) {
	NSData *d = [NSJSONSerialization dataWithJSONObject:obj options:NSJSONWritingSortedKeys error:nil];
	return d ? strndup(d.bytes, d.length) : strdup("null");
}

// deliveredList is getDeliveredNotifications' list, or nil after secs.
static NSArray<UNNotification *> *deliveredList(UNUserNotificationCenter *c, int secs) {
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	NSMutableArray *box = [NSMutableArray array];
	[c getDeliveredNotificationsWithCompletionHandler:^(NSArray<UNNotification *> *ns) {
		@synchronized(box) {
			[box addObjectsFromArray:ns];
		}
		dispatch_semaphore_signal(sem);
	}];
	if (!waitFor(sem, secs)) {
		return nil;
	}
	@synchronized(box) {
		return [box copy];
	}
}

// clack_unprobe_delivered_json: the delivered notifications' requests as
// JSON objects {id, title, subtitle, body, thread, category, sound}.
char *clack_unprobe_delivered_json(int secs) {
	@autoreleasepool {
		UNUserNotificationCenter *c = center(nil);
		NSArray<UNNotification *> *ns = c ? deliveredList(c, secs) : nil;
		if (ns == nil) {
			return strdup("null");
		}
		NSMutableArray *out = [NSMutableArray array];
		for (UNNotification *n in ns) {
			UNNotificationContent *k = n.request.content;
			[out addObject:@{
				@"id" : n.request.identifier ?: @"",
				@"title" : k.title ?: @"",
				@"subtitle" : k.subtitle ?: @"",
				@"body" : k.body ?: @"",
				@"thread" : k.threadIdentifier ?: @"",
				@"category" : k.categoryIdentifier ?: @"",
				@"sound" : @((BOOL)(k.sound != nil)),
			}];
		}
		return jsonOut(out);
	}
}

// clack_unprobe_categories: getNotificationCategories as JSON objects {id,
// customDismiss, actions: [{id, title, foreground, textInput, buttonTitle,
// placeholder}]}.
char *clack_unprobe_categories(int secs) {
	@autoreleasepool {
		UNUserNotificationCenter *c = center(nil);
		if (c == nil) {
			return strdup("null");
		}
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		NSMutableArray *box = [NSMutableArray array];
		[c getNotificationCategoriesWithCompletionHandler:^(NSSet<UNNotificationCategory *> *cats) {
			NSMutableArray *out = [NSMutableArray array];
			for (UNNotificationCategory *cat in cats) {
				NSMutableArray *acts = [NSMutableArray array];
				for (UNNotificationAction *a in cat.actions) {
					NSMutableDictionary *o = [@{
						@"id" : a.identifier ?: @"",
						@"title" : a.title ?: @"",
						@"foreground" : @((BOOL)((a.options & UNNotificationActionOptionForeground) != 0)),
						@"textInput" : @((BOOL)[a isKindOfClass:[UNTextInputNotificationAction class]]),
					} mutableCopy];
					if ([a isKindOfClass:[UNTextInputNotificationAction class]]) {
						o[@"buttonTitle"] = ((UNTextInputNotificationAction *)a).textInputButtonTitle ?: @"";
						o[@"placeholder"] = ((UNTextInputNotificationAction *)a).textInputPlaceholder ?: @"";
					}
					[acts addObject:o];
				}
				[out addObject:@{
					@"id" : cat.identifier ?: @"",
					@"customDismiss" : @((BOOL)((cat.options & UNNotificationCategoryOptionCustomDismissAction) != 0)),
					@"actions" : acts,
				}];
			}
			@synchronized(box) {
				[box addObject:out];
			}
			dispatch_semaphore_signal(sem);
		}];
		if (!waitFor(sem, secs)) {
			return strdup("null");
		}
		@synchronized(box) {
			return jsonOut(box.firstObject);
		}
	}
}

// clack_unprobe_constants: the default and dismiss action identifiers.
char *clack_unprobe_constants(void) {
	return dupstr([NSString stringWithFormat:@"%@|%@", UNNotificationDefaultActionIdentifier, UNNotificationDismissActionIdentifier]);
}

// clack_unprobe_delegate_class: the center's delegate's class.
char *clack_unprobe_delegate_class(void) {
	@autoreleasepool {
		UNUserNotificationCenter *c = center(nil);
		return dupstr(c && c.delegate ? NSStringFromClass([(NSObject *)c.delegate class]) : @"(none)");
	}
}

// clack_unprobe_respond hands the center's delegate a response to the
// delivered notification ident, as a click on it would: action (a text
// input's with text). macOS makes responses only for a real click, so this
// one is decoded from an archive with UNNotificationResponse's keys. It
// returns "" once the delegate took it, else what went wrong.
char *clack_unprobe_respond(const char *ident, const char *action, const char *text, int textInput, int secs) {
	@autoreleasepool {
		UNUserNotificationCenter *c = center(nil);
		if (c == nil || c.delegate == nil) {
			return strdup("no center or no delegate");
		}
		UNNotification *note = nil;
		for (UNNotification *n in deliveredList(c, secs)) {
			if ([n.request.identifier isEqualToString:@(ident)]) {
				note = n;
			}
		}
		if (note == nil) {
			return strdup("not delivered");
		}
		NSKeyedArchiver *ar = [[NSKeyedArchiver alloc] initRequiringSecureCoding:NO];
		[ar encodeObject:note forKey:@"notification"];
		[ar encodeObject:@(action) forKey:@"actionIdentifier"];
		if (textInput) {
			[ar encodeObject:@(text) forKey:@"userText"];
		}
		[ar finishEncoding];
		NSError *err = nil;
		NSKeyedUnarchiver *un = [[NSKeyedUnarchiver alloc] initForReadingFromData:ar.encodedData error:&err];
		if (un == nil) {
			return dupstr([NSString stringWithFormat:@"unarchiver: %@", err]);
		}
		un.requiresSecureCoding = NO;
		Class cls = textInput ? [UNTextInputNotificationResponse class] : [UNNotificationResponse class];
		UNNotificationResponse *resp = nil;
		@try {
			resp = [[cls alloc] initWithCoder:un];
		} @catch (NSException *e) {
			return dupstr([NSString stringWithFormat:@"initWithCoder raised %@", e.reason]);
		}
		[un finishDecoding];
		if (resp == nil || ![resp.actionIdentifier isEqualToString:@(action)] || ![resp.notification.request.identifier isEqualToString:@(ident)] ||
		    (textInput && ![((UNTextInputNotificationResponse *)resp).userText isEqualToString:@(text)])) {
			return dupstr([NSString stringWithFormat:@"no response from the archive (%@ %@)", resp.actionIdentifier, resp.notification.request.identifier]);
		}
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[c.delegate userNotificationCenter:c didReceiveNotificationResponse:resp withCompletionHandler:^{
			dispatch_semaphore_signal(sem);
		}];
		return strdup(waitFor(sem, secs) ? "" : "the delegate never called its completion handler");
	}
}

// clack_unprobe_will_present: what the delegate answers for the delivered
// notification ident while the app is frontmost (-1: no answer).
long clack_unprobe_will_present(const char *ident, int secs) {
	@autoreleasepool {
		UNUserNotificationCenter *c = center(nil);
		if (c == nil || ![c.delegate respondsToSelector:@selector(userNotificationCenter:willPresentNotification:withCompletionHandler:)]) {
			return -1;
		}
		for (UNNotification *n in deliveredList(c, secs)) {
			if ([n.request.identifier isEqualToString:@(ident)]) {
				__block long got = -1;
				[c.delegate userNotificationCenter:c willPresentNotification:n withCompletionHandler:^(UNNotificationPresentationOptions o) {
					got = (long)o;
				}];
				return got;
			}
		}
		return -1;
	}
}
