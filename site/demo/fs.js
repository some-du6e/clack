// An in-memory file system for Go's js/wasm runtime, which does its file
// I/O through a Node-style globalThis.fs (syscall/fs_js.go), and the
// process object it asks for its working directory. The demo's settings,
// message cache and image cache live in the page's memory and go with it.
// Standard output and error go to the console, line by line, as
// wasm_exec.js's own stub does. Load before wasm_exec.js, which keeps an
// fs and a process that are already there.
(() => {
  const S_IFDIR = 0o040000, S_IFREG = 0o100000;
  // Linux's values; Go reads them from here
  const constants = { O_WRONLY: 1, O_RDWR: 2, O_CREAT: 64, O_EXCL: 128, O_TRUNC: 512, O_APPEND: 1024, O_DIRECTORY: 65536 };
  const fail = (code) => Object.assign(new Error(code), { code });
  let lastIno = 0;
  const now = () => Date.now();
  const mk = (dir, mode) => ({ dir, mode: mode & 0o777, ino: ++lastIno, data: new Uint8Array(0), size: 0, kids: dir ? new Map() : null, atime: now(), mtime: now(), ctime: now() });
  const root = mk(true, 0o755);
  const parts = (path) => {
    const out = [];
    for (const s of String(path).split('/')) {
      if (s === '' || s === '.') continue;
      if (s === '..') out.pop(); else out.push(s);
    }
    return out;
  };
  const walk = (names) => {
    let n = root;
    for (const s of names) {
      if (!n.dir) throw fail('ENOTDIR');
      n = n.kids.get(s);
      if (!n) throw fail('ENOENT');
    }
    return n;
  };
  const lookup = (path) => walk(parts(path));
  // the directory path's last element goes in, and that element's name
  const parentOf = (path) => {
    const names = parts(path);
    const name = names.pop();
    if (name === undefined) throw fail('EBUSY'); // the root itself
    const dir = walk(names);
    if (!dir.dir) throw fail('ENOTDIR');
    return [dir, name];
  };
  const statOf = (n) => ({
    dev: 1, ino: n.ino, mode: (n.dir ? S_IFDIR : S_IFREG) | n.mode, nlink: 1, uid: 1000, gid: 1000, rdev: 0,
    size: n.dir ? 4096 : n.size, blksize: 4096, blocks: Math.ceil(n.size / 512),
    atimeMs: n.atime, mtimeMs: n.mtime, ctimeMs: n.ctime, isDirectory: () => n.dir,
  });
  const resize = (n, size) => {
    if (size > n.data.length) {
      const b = new Uint8Array(Math.max(size, n.data.length * 2, 64));
      b.set(n.data.subarray(0, n.size));
      n.data = b;
    } else if (size < n.size) {
      n.data.fill(0, size, n.size);
    }
    n.size = size;
    n.mtime = now();
  };
  const files = new Map(); // fd -> { node, pos, write, append }
  let nextFd = 3;
  const file = (fd) => {
    const f = files.get(fd);
    if (!f) throw fail('EBADF');
    return f;
  };
  // every call answers through its callback, null for no error
  const run = (cb, f) => {
    let v;
    try { v = f(); } catch (e) { cb(e.code ? e : fail('EIO')); return; }
    cb(null, v);
  };

  const decoder = new TextDecoder('utf-8');
  let pending = '';
  const toConsole = (buf) => {
    pending += decoder.decode(buf, { stream: true });
    const nl = pending.lastIndexOf('\n');
    if (nl !== -1) {
      console.log(pending.slice(0, nl));
      pending = pending.slice(nl + 1);
    }
    return buf.length;
  };

  globalThis.fs = {
    constants,
    writeSync(fd, buf) {
      if (fd === 1 || fd === 2) return toConsole(buf);
      let n;
      globalThis.fs.write(fd, buf, 0, buf.length, null, (e, v) => { if (e) throw e; n = v; });
      return n;
    },
    write(fd, buf, off, len, pos, cb) {
      if (fd === 1 || fd === 2) { cb(null, toConsole(buf.subarray(off, off + len))); return; }
      run(cb, () => {
        const f = file(fd);
        if (!f.write) throw fail('EBADF');
        const n = f.node;
        const here = pos == null;
        const at = here ? (f.append ? n.size : f.pos) : pos;
        if (at + len > n.size) resize(n, at + len);
        n.data.set(buf.subarray(off, off + len), at);
        n.mtime = now();
        if (here) f.pos = at + len;
        return len;
      });
    },
    read(fd, buf, off, len, pos, cb) {
      run(cb, () => {
        const f = file(fd);
        const n = f.node;
        if (n.dir) throw fail('EISDIR');
        const here = pos == null;
        const at = here ? f.pos : pos;
        const k = Math.max(0, Math.min(len, n.size - at));
        buf.set(n.data.subarray(at, at + k), off);
        if (here) f.pos = at + k;
        n.atime = now();
        return k;
      });
    },
    open(path, flags, mode, cb) {
      run(cb, () => {
        let n;
        try {
          n = lookup(path);
          if ((flags & constants.O_CREAT) && (flags & constants.O_EXCL)) throw fail('EEXIST');
        } catch (e) {
          if (e.code !== 'ENOENT' || !(flags & constants.O_CREAT)) throw e;
          const [dir, name] = parentOf(path);
          n = mk(false, mode);
          dir.kids.set(name, n);
          dir.mtime = now();
        }
        const access = flags & 3;
        if (n.dir && access !== 0) throw fail('EISDIR');
        if ((flags & constants.O_DIRECTORY) && !n.dir) throw fail('ENOTDIR');
        if ((flags & constants.O_TRUNC) && access !== 0) resize(n, 0);
        const fd = nextFd++;
        files.set(fd, { node: n, pos: 0, write: access !== 0, append: !!(flags & constants.O_APPEND) });
        return fd;
      });
    },
    close(fd, cb) { run(cb, () => { file(fd); files.delete(fd); }); },
    fsync(fd, cb) { run(cb, () => { file(fd); }); },
    fstat(fd, cb) { run(cb, () => statOf(file(fd).node)); },
    stat(path, cb) { run(cb, () => statOf(lookup(path))); },
    lstat(path, cb) { run(cb, () => statOf(lookup(path))); },
    readdir(path, cb) {
      run(cb, () => {
        const n = lookup(path);
        if (!n.dir) throw fail('ENOTDIR');
        return [...n.kids.keys()];
      });
    },
    mkdir(path, perm, cb) {
      run(cb, () => {
        const [dir, name] = parentOf(path);
        if (dir.kids.has(name)) throw fail('EEXIST');
        dir.kids.set(name, mk(true, perm));
        dir.mtime = now();
      });
    },
    unlink(path, cb) {
      run(cb, () => {
        const [dir, name] = parentOf(path);
        const n = dir.kids.get(name);
        if (!n) throw fail('ENOENT');
        if (n.dir) throw fail('EISDIR');
        dir.kids.delete(name);
        dir.mtime = now();
      });
    },
    rmdir(path, cb) {
      run(cb, () => {
        const [dir, name] = parentOf(path);
        const n = dir.kids.get(name);
        if (!n) throw fail('ENOENT');
        if (!n.dir) throw fail('ENOTDIR');
        if (n.kids.size) throw fail('ENOTEMPTY');
        dir.kids.delete(name);
        dir.mtime = now();
      });
    },
    rename(from, to, cb) {
      run(cb, () => {
        const [fdir, fname] = parentOf(from);
        const n = fdir.kids.get(fname);
        if (!n) throw fail('ENOENT');
        const [tdir, tname] = parentOf(to);
        const old = tdir.kids.get(tname);
        if (old && old !== n) {
          if (old.dir && !n.dir) throw fail('EISDIR');
          if (!old.dir && n.dir) throw fail('ENOTDIR');
          if (old.dir && old.kids.size) throw fail('ENOTEMPTY');
        }
        fdir.kids.delete(fname);
        tdir.kids.set(tname, n);
        fdir.mtime = tdir.mtime = now();
      });
    },
    truncate(path, len, cb) {
      run(cb, () => {
        const n = lookup(path);
        if (n.dir) throw fail('EISDIR');
        resize(n, len);
      });
    },
    ftruncate(fd, len, cb) {
      run(cb, () => {
        const f = file(fd);
        if (!f.write) throw fail('EINVAL');
        resize(f.node, len);
      });
    },
    chmod(path, mode, cb) { run(cb, () => { lookup(path).mode = mode & 0o777; }); },
    fchmod(fd, mode, cb) { run(cb, () => { file(fd).node.mode = mode & 0o777; }); },
    chown(path, uid, gid, cb) { run(cb, () => { lookup(path); }); },
    fchown(fd, uid, gid, cb) { run(cb, () => { file(fd); }); },
    lchown(path, uid, gid, cb) { run(cb, () => { lookup(path); }); },
    utimes(path, atime, mtime, cb) {
      run(cb, () => {
        const n = lookup(path);
        n.atime = atime * 1000;
        n.mtime = mtime * 1000;
      });
    },
    readlink(path, cb) { run(cb, () => { lookup(path); throw fail('EINVAL'); }); },
    link(path, link, cb) { cb(fail('ENOSYS')); },
    symlink(path, link, cb) { cb(fail('ENOSYS')); },
  };

  globalThis.process = {
    getuid: () => 1000, getgid: () => 1000, geteuid: () => 1000, getegid: () => 1000,
    getgroups: () => [1000], pid: 1, ppid: 0,
    umask: () => 0o022,
    cwd: () => '/',
    chdir: () => { throw fail('ENOSYS'); },
  };
})();
