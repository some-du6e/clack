// thread.js: the thread pane in the secondary view (ui-core).
import { esc, h, plural } from './util.js';
import { icon } from './icons.js';
import { state, on, conv, convName, putUsers, userName } from './state.js';
import { api } from './api.js';
import { messageHTML, bindMessageEvents, MessageList, hydrateTimestamps } from './messages.js';
import { Composer } from './composer.js';
import { sendMessage, startEdit } from './channel.js';
import { openSecondary, navigate, ext } from './app.js';

let currentThread = null;

const inflight = new Map();
export function fetchThread(channel, ts, fresh) {
  const key = channel + ':' + ts;
  if (inflight.has(key)) return inflight.get(key);
  const p = api.get('replies', { channel, ts, fresh: fresh ? 1 : undefined }).then((r) => {
    putUsers(r.users);
    const entry = state.threads.get(key) || { messages: [] };
    const pending = entry.messages.filter((m) => m.pending);
    entry.messages.splice(0, entry.messages.length, ...(r.messages || []), ...pending);
    entry.fetchedAt = Date.now();
    state.threads.set(key, entry);
    return entry;
  }).finally(() => inflight.delete(key));
  inflight.set(key, p);
  return p;
}

class ThreadView {
  constructor(channel, ts) {
    this.channel = channel;
    this.ts = ts;
    this.key = channel + ':' + ts;
    const c = conv(channel);
    const where = c ? (c.kind === 'im' || c.kind === 'mpim' ? convName(c) : '#' + convName(c)) : '';
    this.el = h(`<div class="p-flexpane p-thread_view" data-channel="${esc(channel)}" data-ts="${esc(ts)}">
<div class="p-flexpane_header"><span class="p-flexpane_header__title">Thread</span><button class="p-flexpane_header__sub" data-act="channel">${esc(where)}</button><span class="p-flexpane_header__spacer"></span><button class="p-flexpane_header__close" data-act="close" aria-label="Close" data-tip="Close">${icon('close', 20)}</button></div>
<div class="p-flexpane_body p-message_pane__scroller p-thread_view__scroller"><div class="p-message_pane__top"></div><div class="p-message_pane__list p-thread_view__list"></div><div class="p-thread_view__composer"></div></div>
</div>`);
    this.scroller = this.el.querySelector('.p-thread_view__scroller');
    this.listEl = this.el.querySelector('.p-thread_view__list');
    this.list = new MessageList(this.scroller, {
      channel, threadTs: ts, inThread: true,
      getView: (t) => this.msgs().find((m) => m.ts === t),
    });
    // The thread list renders differently: parent, a "N replies" divider, then replies, no day sections.
    this.list.render = (msgs) => { this.list.msgs = msgs; this.renderAll(); };
    this.list.rerender = () => { const p = this.list.savePos(); this.renderAll(); this.list.restorePos(p); };
    this.list.add = (v) => this.add(v);
    this.el.addEventListener('click', (e) => {
      const b = e.target.closest('[data-act]');
      if (!b) return;
      if (b.dataset.act === 'close') closeThread();
      if (b.dataset.act === 'channel') navigate('#/c/' + channel);
    });
    this.composer = new Composer({
      channel, threadTs: ts, placeholder: 'Reply…', draftKey: this.key,
      broadcastLabel: c && c.kind !== 'im' ? (c.kind === 'mpim' ? 'group' : '#' + convName(c)) : (c ? 'direct message' : ''),
      onSend: (v) => sendMessage({ channel, threadTs: ts }, v, this.list),
      onEditLast: () => {
        const self = state.self?.id;
        const m = [...this.msgs()].reverse().find((x) => x.user === self && !x.pending && x.ts !== this.ts);
        if (m) startEdit(this.list, m);
      },
    });
    this.el.querySelector('.p-thread_view__composer').appendChild(this.composer.el);
  }

  msgs() { return this.list.msgs; }

  renderAll() {
    const msgs = this.list.msgs;
    if (!msgs.length) { this.listEl.innerHTML = '<div class="p-message_pane__loading"><span class="c-spinner"></span></div>'; return; }
    const [parent, ...replies] = msgs;
    let s = messageHTML(parent, { inThread: true, isParent: true });
    const n = replies.filter((m) => !m.pending).length;
    s += `<div class="p-thread_replies_divider">${n ? plural(n, 'reply', 'replies') : ''}</div>`;
    let prev = null;
    for (const r of replies) {
      s += messageHTML(r, { inThread: true, compact: canGroupReply(prev, r) });
      prev = r;
    }
    this.listEl.innerHTML = s;
    hydrateTimestamps(this.listEl);
  }

  add(v) {
    const msgs = this.list.msgs;
    if (msgs.some((m) => m.ts === v.ts)) { this.list.update(v); return; }
    const wasBottom = this.list.atBottom;
    let i = msgs.length;
    while (i > 1 && msgs[i - 1].pending && !v.pending) i--;
    msgs.splice(i, 0, v);
    this.renderAll();
    if (wasBottom || v.pending || v.user === state.self?.id) this.list.scrollToBottom();
  }

  async load() {
    const cached = state.threads.get(this.key);
    if (cached) {
      this.list.msgs = cached.messages;
      this.renderAll();
      this.list.scrollToBottom();
      if (Date.now() - (cached.fetchedAt || 0) > 30000) fetchThread(this.channel, this.ts).then(() => { this.renderAll(); }).catch(() => {});
      return;
    }
    // show the parent immediately if the channel history has it
    const parent = state.history.get(this.channel)?.messages.find((m) => m.ts === this.ts);
    this.list.msgs = parent ? [parent] : [];
    this.renderAll();
    try {
      const e = await fetchThread(this.channel, this.ts);
      this.list.msgs = e.messages;
      this.renderAll();
      this.list.scrollToBottom();
    } catch (err) {
      this.listEl.insertAdjacentHTML('beforeend', `<div class="p-message_pane__error">Couldn’t load replies: ${esc(err.code || err.message)}</div>`);
    }
  }

  renderTyping() {
    const t = state.typing.get(this.channel);
    const now = Date.now();
    const names = [];
    if (t) for (const [id, x] of t) if (x.until > now && x.thread_ts === this.ts) names.push(x.name || userName(id));
    this.composer.setTyping(names.length === 1 ? `${names[0]} is typing…` : names.length === 2 ? `${names[0]} and ${names[1]} are typing…` : names.length ? 'Several people are typing…' : '');
  }

  destroy() { this.list.destroy(); this.composer.destroy(); }
}

function canGroupReply(prev, v) {
  if (!prev) return false;
  return prev.user === v.user && !prev.name === !v.name && parseFloat(v.ts) - parseFloat(prev.ts) < 300 && !v.broadcast;
}

export function openThreadPane(channel, ts) {
  if (currentThread && currentThread.key === channel + ':' + ts) { currentThread.composer.focus(); return currentThread; }
  currentThread?.destroy();
  const tv = new ThreadView(channel, ts);
  currentThread = tv;
  openSecondary(tv.el, { kind: 'thread', onClose: () => { if (currentThread === tv) { tv.destroy(); currentThread = null; } } });
  tv.load().then(() => tv.composer.focus());
  return tv;
}

export function closeThread() {
  openSecondary(null);
}

export function currentThreadView() { return currentThread; }

// ---- live updates

on('message', (d) => {
  const m = d.msg;
  const key = d.channel + ':' + (m.thread_ts || m.ts);
  const entry = state.threads.get(key);
  if (!d.reply && !(m.thread_ts && m.thread_ts !== m.ts)) return;
  if (currentThread && currentThread.key === key) currentThread.add(m);
  else if (entry && !entry.messages.some((x) => x.ts === m.ts)) entry.messages.push(m);
});
for (const t of ['update', 'local_update']) {
  on(t, (d) => {
    const m = d.msg;
    const key = d.channel + ':' + (m.thread_ts || m.ts);
    if (currentThread && currentThread.key === key) { currentThread.list.update(m); return; }
    const entry = state.threads.get(key);
    if (entry) { const i = entry.messages.findIndex((x) => x.ts === m.ts); if (i >= 0) entry.messages[i] = { ...entry.messages[i], ...m }; }
  });
}
on('delete', (d) => {
  if (!currentThread || currentThread.channel !== d.channel) return;
  if (d.ts === currentThread.ts) { closeThread(); return; }
  const i = currentThread.list.msgs.findIndex((m) => m.ts === d.ts);
  if (i >= 0) { currentThread.list.msgs.splice(i, 1); currentThread.renderAll(); }
});
on('typing_changed', (d) => { if (currentThread && currentThread.channel === d.channel) currentThread.renderTyping(); });
on('thread_edit_request', (v) => {
  if (currentThread && currentThread.list.msgs.some((m) => m.ts === v.ts)) startEdit(currentThread.list, v);
});

export { bindMessageEvents };
