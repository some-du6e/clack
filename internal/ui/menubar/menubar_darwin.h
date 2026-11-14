// menubar_darwin.m's C interface (cgo).

#pragma once

#include <stdint.h>
#include <CoreFoundation/CoreFoundation.h>

typedef struct {
	const char *title, *key, *role;
	int mods, parent, flags, tag;
} clack_mi;

enum {
	CLACK_MI_SEP = 1,
	CLACK_MI_FORWARD = 2,
	CLACK_MI_RECENT = 4,
	CLACK_MI_SUB = 8,
	CLACK_MI_MENU = 16,
};

void clack_menubar_edit_role(const char *sel, const char *key, int mods);
CFTypeRef clack_menubar_spec(const clack_mi *items, int n);
void clack_menubar_install(CFTypeRef spec);

// main thread only (tests)
CFTypeRef clack_menubar_build(CFTypeRef spec);
void clack_menubar_add_edit_roles(void);
char *clack_menubar_dump(CFTypeRef menu);
char *clack_menubar_install_now(CFTypeRef spec);
