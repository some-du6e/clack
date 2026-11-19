#!/usr/bin/env node
// The site's checks, site/tools/check.mjs, from the repo root. wrangler runs
// site/wrangler.jsonc's build command, `node tools/check.mjs --production
// --demo` (the deploy's guard), in the directory the shell is in, so this
// file lets the same command work whether `wrangler deploy` starts in site/
// or here with `-c site/wrangler.jsonc`. check.mjs finds site/dist from its
// own location, and process.argv passes through.
import '../site/tools/check.mjs';
