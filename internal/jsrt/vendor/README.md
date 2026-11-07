Unmodified production builds from npm (MIT, Meta Platforms, Inc.; see the LICENSE.* files):

| file | package |
|---|---|
| react.production.js, react-jsx-runtime.production.js | react@19.3.1 (`cjs/`) |
| react-reconciler.production.js, react-reconciler-constants.production.js | react-reconciler@0.34.0 (`cjs/`) |
| scheduler.production.js | scheduler@0.28.0 (`cjs/`) |

They are CommonJS; `vendor.go` wraps each in a factory that env.js's small `require` resolves.
To update: download the tarballs from registry.npmjs.org, copy the same files, bump the versions here.
