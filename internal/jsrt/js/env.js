// env.js: the browser environment for Taut plugins (skeleton; filled in next).
(() => {
  'use strict';
  const H = globalThis.__jrh;
  delete globalThis.__jrh;
  const mods = new Map(), cache = new Map();
  const jr = {
    define(name, factory) { mods.set(name, factory); },
    require(name) {
      if (cache.has(name)) return cache.get(name).exports;
      const f = mods.get(name);
      if (!f) throw new Error("Cannot find module '" + name + "'");
      const module = { exports: {} };
      cache.set(name, module);
      f(module, module.exports, jr.require);
      return module.exports;
    },
    fire(id) {},
    runImmediates() {},
    fetchDone() {},
  };
  Object.defineProperty(globalThis, '__jr', { value: jr });
  globalThis.__clack = globalThis.__clack || {};
  globalThis.console = {
    log: (...a) => H.log('log', a.map(String).join(' ')),
    info: (...a) => H.log('info', a.map(String).join(' ')),
    warn: (...a) => H.log('warn', a.map(String).join(' ')),
    error: (...a) => H.log('error', a.map(String).join(' ')),
    debug: (...a) => H.log('debug', a.map(String).join(' ')),
  };
})();
