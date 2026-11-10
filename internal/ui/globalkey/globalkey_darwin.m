//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <Carbon/Carbon.h>
extern void clackHotkeyPressed(void);

static EventHotKeyRef hotkeyRef;
static EventHandlerRef handlerRef;

static OSStatus onHotkey(EventHandlerCallRef next, EventRef ev, void *data) {
	EventHotKeyID id;
	if (GetEventParameter(ev, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof id, NULL, &id) == noErr &&
	    id.signature == 'CLAK') {
		clackHotkeyPressed();
	}
	return noErr;
}

static int doRegister(void) {
	EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
	if (handlerRef == NULL &&
	    InstallEventHandler(GetApplicationEventTarget(), NewEventHandlerUPP(onHotkey), 1, &spec, NULL, &handlerRef) != noErr) {
		return 2;
	}
	EventHotKeyID id = {'CLAK', 1};
	// Cmd+Shift+Space (kVK_Space)
	OSStatus st = RegisterEventHotKey(kVK_Space, cmdKey | shiftKey, id, GetApplicationEventTarget(), 0, &hotkeyRef);
	if (st == eventHotKeyExistsErr) {
		return 1;
	}
	return st == noErr ? 0 : 2;
}

// clack_hotkey_register registers on the main thread (Carbon's event
// APIs aren't thread-safe). Off it, the registration is queued for the
// main run loop and reported as done.
int clack_hotkey_register(void) {
	if ([NSThread isMainThread]) {
		return doRegister();
	}
	dispatch_async(dispatch_get_main_queue(), ^{ doRegister(); });
	return 0;
}

void clack_hotkey_unregister(void) {
	void (^undo)(void) = ^{
		if (hotkeyRef != NULL) {
			UnregisterEventHotKey(hotkeyRef);
			hotkeyRef = NULL;
		}
	};
	if ([NSThread isMainThread]) {
		undo();
	} else {
		dispatch_async(dispatch_get_main_queue(), undo);
	}
}
