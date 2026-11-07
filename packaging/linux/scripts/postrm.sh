#!/bin/sh
# Refresh the desktop and icon caches after clack.desktop and its icons are
# removed (or replaced on upgrade). Never fails the removal.
if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
	gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi
exit 0
