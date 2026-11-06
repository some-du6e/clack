// composer.js: the message composer (ui-core). Contenteditable with Slack's toolbar, live mrkdwn
// formatting, autocomplete for @users #channels :emoji: /commands, and rich_text block output.
import { esc, h, debounce, avatarFor, isMac } from './util.js';
import { icon } from './icons.js';
import { state, user, conv, convName, ensureUsers, emit, putUsers } from './state.js';
import { api } from './api.js';
import { ext } from './app.js';

const ZW = '​';
const MOD = isMac() ? '⌘' : 'Ctrl';

// ---------------------------------------------------------------- serialization: editor DOM -> rich_text

function styleKey(s) { return ['bold', 'italic', 'strike', 'underline', 'code'].filter((k) => s[k]).join(','); }
function cleanStyle(s) {
  const o = {};
  for (const k of ['bold', 'italic', 'strike', 'underline', 'code']) if (s[k]) o[k] = true;
  return Object.keys(o).length ? o : undefined;
}

function pushText(out, text, style) {
  text = text.replace(/​/g, '').replace(/ /g, ' ');
  if (!text) return;
  const st = cleanStyle(style);
  const last = out[out.length - 1];
  if (last && last.type === 'text' && styleKey(last.style || {}) === styleKey(st || {})) { last.text += text; return; }
  const el = { type: 'text', text };
  if (st) el.style = st;
  out.push(el);
}

function inlineElements(node, style, out) {
  for (const n of node.childNodes) {
    if (n.nodeType === 3) { pushText(out, n.nodeValue, style); continue; }
    if (n.nodeType !== 1) continue;
    const tag = n.tagName;
    if (tag === 'BR') { pushText(out, '\n', style); continue; }
    if (n.classList.contains('c-member_slug') && n.dataset.memberId) { out.push({ type: 'user', user_id: n.dataset.memberId }); continue; }
    if (n.classList.contains('c-channel_entity') && n.dataset.channelId) { out.push({ type: 'channel', channel_id: n.dataset.channelId }); continue; }
    if (n.dataset.broadcast) { out.push({ type: 'broadcast', range: n.dataset.broadcast }); continue; }
    if (n.dataset.usergroupId) { out.push({ type: 'usergroup', usergroup_id: n.dataset.usergroupId }); continue; }
    if (tag === 'IMG' && (n.dataset.name || n.dataset.stringifyEmoji)) {
      const name = (n.dataset.name || n.dataset.stringifyEmoji).replace(/:/g, '');
      const m = name.match(/^(.*)::skin-tone-(\d)$/);
      out.push(m ? { type: 'emoji', name: m[1], skin_tone: +m[2] } : { type: 'emoji', name });
      continue;
    }
    if (tag === 'A' && n.getAttribute('href') && !n.classList.contains('c-member_slug')) {
      const text = n.textContent.replace(/​/g, '');
      const url = n.getAttribute('href');
      const el = { type: 'link', url };
      if (text && text !== url) el.text = text;
      const st = cleanStyle(style);
      if (st) el.style = st;
      out.push(el);
      continue;
    }
    const s = { ...style };
    if (tag === 'B' || tag === 'STRONG' || n.style?.fontWeight === 'bold' || +n.style?.fontWeight >= 600) s.bold = true;
    if (tag === 'I' || tag === 'EM' || n.style?.fontStyle === 'italic') s.italic = true;
    if (tag === 'S' || tag === 'STRIKE' || tag === 'DEL' || /line-through/.test(n.style?.textDecoration || '')) s.strike = true;
    if (tag === 'U' || /underline/.test(n.style?.textDecoration || '')) s.underline = true;
    if (tag === 'CODE') s.code = true;
    if (/^(P|DIV)$/.test(tag)) { // nested blocks inside inline context (pasted content)
      if (out.length) pushText(out, '\n', {});
      inlineElements(n, s, out);
      continue;
    }
    inlineElements(n, s, out);
  }
  return out;
}

function trimTrailingNewline(els) {
  const last = els[els.length - 1];
  if (last && last.type === 'text') {
    last.text = last.text.replace(/\n+$/, '');
    if (!last.text) els.pop();
  }
  return els;
}

function listBlocks(listEl, indent, out) {
  const style = listEl.tagName === 'OL' ? 'ordered' : 'bullet';
  let cur = null;
  for (const li of listEl.children) {
    if (li.tagName === 'UL' || li.tagName === 'OL') { cur = null; listBlocks(li, indent + 1, out); continue; }
    if (!cur) { cur = { type: 'rich_text_list', style, indent, elements: [] }; out.push(cur); }
    const nested = [...li.children].filter((c) => c.tagName === 'UL' || c.tagName === 'OL');
    const clone = li.cloneNode(true);
    clone.querySelectorAll(':scope > ul, :scope > ol').forEach((x) => x.remove());
    cur.elements.push({ type: 'rich_text_section', elements: trimTrailingNewline(inlineElements(clone, {}, [])) });
    for (const nl of nested) { cur = null; listBlocks(nl, indent + 1, out); }
  }
}

// editorToRichText converts the editor DOM to rich_text elements.
export function editorToRichText(root) {
  const out = [];
  let section = null;
  const addLine = (els) => {
    if (!section) { section = { type: 'rich_text_section', elements: [] }; out.push(section); }
    else pushText(section.elements, '\n', {});
    section.elements.push(...els);
    // merge adjacent texts with equal style
    const merged = [];
    for (const e of section.elements) {
      if (e.type === 'text') pushText(merged, e.text, e.style || {});
      else merged.push(e);
    }
    section.elements = merged;
  };
  const inlineRun = [];
  const flushRun = () => {
    if (!inlineRun.length) return;
    const tmp = document.createElement('p');
    inlineRun.forEach((n) => tmp.appendChild(n.cloneNode(true)));
    inlineRun.length = 0;
    addLine(inlineElements(tmp, {}, []));
  };
  for (const n of root.childNodes) {
    const tag = n.nodeType === 1 ? n.tagName : '';
    if (!tag || !/^(P|DIV|UL|OL|BLOCKQUOTE|PRE|H1|H2|H3)$/.test(tag)) { inlineRun.push(n); continue; }
    flushRun();
    if (tag === 'P' || tag === 'DIV' || /^H\d$/.test(tag)) {
      const els = inlineElements(n, {}, []);
      // a <p><br></p> is an empty line
      addLine(trimTrailingNewline(els));
    } else if (tag === 'UL' || tag === 'OL') {
      section = null;
      listBlocks(n, 0, out);
    } else if (tag === 'BLOCKQUOTE') {
      section = null;
      const els = [];
      [...n.childNodes].forEach((c, i) => {
        if (c.nodeType === 1 && /^(P|DIV)$/.test(c.tagName)) { if (els.length) pushText(els, '\n', {}); inlineElements(c, {}, els); }
        else { const t = document.createElement('span'); t.appendChild(c.cloneNode(true)); inlineElements(t, {}, els); }
      });
      out.push({ type: 'rich_text_quote', elements: trimTrailingNewline(els) });
    } else if (tag === 'PRE') {
      section = null;
      const text = n.innerText.replace(/​/g, '').replace(/\n$/, '');
      out.push({ type: 'rich_text_preformatted', elements: [{ type: 'text', text }], border: 0 });
    }
  }
  flushRun();
  // drop leading/trailing empty sections and trailing newlines
  while (out.length && out[0].type === 'rich_text_section' && isBlankSection(out[0])) out.shift();
  while (out.length && out[out.length - 1].type === 'rich_text_section' && isBlankSection(out[out.length - 1])) out.pop();
  for (const b of out) if (b.type === 'rich_text_section') trimEdges(b.elements);
  return out;
}

function isBlankSection(s) { return s.elements.every((e) => e.type === 'text' && !e.text.trim()); }
function trimEdges(els) {
  while (els.length && els[0].type === 'text' && /^\n/.test(els[0].text)) { els[0].text = els[0].text.replace(/^\n+/, ''); if (!els[0].text) els.shift(); }
  trimTrailingNewline(els);
}

// ---------------------------------------------------------------- rich_text -> mrkdwn fallback text and preview HTML

function escMrkdwn(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }

function inlineText(els) {
  let s = '';
  for (const e of els) {
    switch (e.type) {
      case 'text': {
        let t = escMrkdwn(e.text);
        const st = e.style || {};
        if (st.code) t = '`' + t + '`';
        else {
          const wrap = (m) => { t = t.replace(/^(\s*)([\s\S]*?)(\s*)$/, (_, a, b, c) => (b ? a + m + b + m + c : a + c)); };
          if (st.strike) wrap('~');
          if (st.italic) wrap('_');
          if (st.bold) wrap('*');
        }
        s += t; break;
      }
      case 'link': s += e.text ? `<${e.url}|${escMrkdwn(e.text)}>` : `<${e.url}>`; break;
      case 'user': s += `<@${e.user_id}>`; break;
      case 'channel': s += `<#${e.channel_id}>`; break;
      case 'usergroup': s += `<!subteam^${e.usergroup_id}>`; break;
      case 'broadcast': s += `<!${e.range}>`; break;
      case 'emoji': s += `:${e.name}:${e.skin_tone ? `:skin-tone-${e.skin_tone}:` : ''}`; break;
      default: break;
    }
  }
  return s;
}

export function richTextToMrkdwn(elements) {
  const parts = [];
  for (const b of elements) {
    if (b.type === 'rich_text_section') parts.push(inlineText(b.elements));
    else if (b.type === 'rich_text_list') {
      parts.push(b.elements.map((it, i) => '    '.repeat(b.indent || 0) + (b.style === 'ordered' ? `${i + 1}. ` : '• ') + inlineText(it.elements)).join('\n'));
    } else if (b.type === 'rich_text_quote') parts.push(inlineText(b.elements).split('\n').map((l) => '&gt; ' + l).join('\n'));
    else if (b.type === 'rich_text_preformatted') parts.push('```' + inlineText(b.elements) + '```');
  }
  return parts.join('\n');
}

function inlineHTML(els) {
  let s = '';
  for (const e of els) {
    let t;
    switch (e.type) {
      case 'text': {
        t = esc(e.text).replace(/\n/g, '<br>');
        const st = e.style || {};
        if (st.code) t = `<code class="c-mrkdwn__code">${t}</code>`;
        if (st.strike) t = `<s>${t}</s>`;
        if (st.italic) t = `<i>${t}</i>`;
        if (st.bold) t = `<b>${t}</b>`;
        if (st.underline) t = `<u>${t}</u>`;
        break;
      }
      case 'link': t = `<a class="c-link" href="${esc(e.url)}" target="_blank" rel="noopener noreferrer">${esc(e.text || e.url)}</a>`; break;
      case 'user': {
        const u = user(e.user_id);
        t = `<a class="c-member_slug c-member_slug--link${e.user_id === state.self?.id ? ' c-member_slug--mention' : ''}" data-member-id="${esc(e.user_id)}" href="#/u/${esc(e.user_id)}">@${esc(u ? u.name || u.real_name : e.user_id)}</a>`;
        break;
      }
      case 'channel': t = `<a class="c-channel_entity c-link" data-channel-id="${esc(e.channel_id)}" href="#/c/${esc(e.channel_id)}">#${esc(convName(conv(e.channel_id)) || e.channel_id)}</a>`; break;
      case 'broadcast': t = `<span class="c-mention_badge">@${esc(e.range)}</span>`; break;
      case 'emoji': {
        const url = ext.emoji?.emojiURL?.(e.name);
        t = url ? `<img class="c-emoji c-emoji__medium" src="${esc(url)}" alt=":${esc(e.name)}:">` : `:${esc(e.name)}:`;
        break;
      }
      default: t = '';
    }
    s += t;
  }
  return s;
}

// richTextToHTML renders rich_text elements the way the backend does, for optimistic rows.
export function richTextToHTML(elements) {
  let s = '<div class="c-message_kit__blocks c-message_kit__blocks--rich_text"><div class="c-message__message_blocks"><div class="p-rich_text_block" dir="auto">';
  for (const b of elements) {
    if (b.type === 'rich_text_section') s += `<div class="p-rich_text_section">${inlineHTML(b.elements)}</div>`;
    else if (b.type === 'rich_text_list') {
      const tag = b.style === 'ordered' ? 'ol' : 'ul';
      s += `<${tag} class="p-rich_text_list p-rich_text_list__${b.style}" data-indent="${b.indent || 0}" style="--indent:${b.indent || 0}">${b.elements.map((it) => `<li>${inlineHTML(it.elements)}</li>`).join('')}</${tag}>`;
    } else if (b.type === 'rich_text_quote') s += `<blockquote class="c-mrkdwn__quote">${inlineHTML(b.elements)}</blockquote>`;
    else if (b.type === 'rich_text_preformatted') s += `<pre class="c-mrkdwn__pre"><div class="p-rich_text_block--no-overflow">${inlineHTML(b.elements)}</div></pre>`;
  }
  return s + '</div></div></div>';
}

// htmlToEditor turns rendered message HTML (from the backend) into editor content, for editing.
function htmlToEditor(html) {
  const tmp = h(`<div>${html}</div>`);
  const blocks = tmp.querySelector('.p-rich_text_block') || tmp.querySelector('.c-message__message_blocks') || tmp;
  blocks.querySelectorAll('.c-message__edited_label').forEach((x) => x.remove());
  let out = '';
  for (const n of blocks.children) {
    if (n.classList.contains('p-rich_text_section')) {
      out += n.innerHTML.split(/<br\s*\/?>/).map((l) => `<p>${l || '<br>'}</p>`).join('');
    } else if (n.tagName === 'UL' || n.tagName === 'OL') {
      out += `<${n.tagName.toLowerCase()}>${[...n.children].map((li) => `<li>${li.innerHTML}</li>`).join('')}</${n.tagName.toLowerCase()}>`;
    } else if (n.tagName === 'BLOCKQUOTE') out += `<blockquote>${n.innerHTML}</blockquote>`;
    else if (n.tagName === 'PRE') out += `<pre>${esc(n.innerText)}</pre>`;
    else out += `<p>${n.innerHTML}</p>`;
  }
  const ed = h(`<div>${out}</div>`);
  ed.querySelectorAll('img.c-emoji').forEach((img) => { img.dataset.name = (img.dataset.stringifyEmoji || img.alt || '').replace(/:/g, ''); });
  ed.querySelectorAll('.c-member_slug, .c-channel_entity, .c-mention_badge').forEach((a) => {
    a.setAttribute('contenteditable', 'false');
    if (a.classList.contains('c-mention_badge') && !a.dataset.usergroupId) a.dataset.broadcast = a.textContent.replace('@', '');
  });
  ed.querySelectorAll('code').forEach((c) => c.removeAttribute('class'));
  return ed.innerHTML;
}

// ---------------------------------------------------------------- autocomplete sources

let commandsCache = null;
async function loadCommands() {
  if (commandsCache) return commandsCache;
  commandsCache = [];
  try {
    const r = await api.call('commands.list', {});
    const list = r.commands || [];
    commandsCache = list.map((c) => ({ name: c.name, desc: c.desc || '', usage: c.usage || '' }));
  } catch { /* ignore */ }
  if (!commandsCache.length) {
    commandsCache = ['/away', '/collapse', '/expand', '/feed', '/invite', '/leave', '/me', '/msg', '/mute', '/remind', '/search', '/shrug', '/status', '/topic', '/who'].map((n) => ({ name: n, desc: '', usage: '' }));
  }
  return commandsCache;
}

function scoreName(q, ...names) {
  q = q.toLowerCase();
  let best = 0;
  for (const n of names) {
    if (!n) continue;
    const l = n.toLowerCase();
    if (l === q) best = Math.max(best, 4);
    else if (l.startsWith(q)) best = Math.max(best, 3);
    else if (l.split(/[\s._-]+/).some((p) => p.startsWith(q))) best = Math.max(best, 2);
    else if (q.length > 1 && l.includes(q)) best = Math.max(best, 1);
  }
  return best;
}

function localUsers(q, limit) {
  const out = [];
  for (const u of state.users.values()) {
    if (u.deleted) continue;
    const s = q ? scoreName(q, u.name, u.real_name, u.handle) : 1;
    if (s) out.push([s, u]);
  }
  out.sort((a, b) => b[0] - a[0] || (a[1].name || '').localeCompare(b[1].name || ''));
  return out.slice(0, limit).map((x) => x[1]);
}

const remoteUserSearch = (() => {
  const cache = new Map();
  return async (q) => {
    if (!q || q.length < 2) return [];
    if (cache.has(q)) return cache.get(q);
    let res = [];
    try {
      const r = await api.edge('users/search', { query: q, count: 12, fuzz: 1, uax29_tokenizer: false, include_profile_only_users: true, enterprise_token: '' });
      res = (r.results || []).map((u) => ({
        id: u.id, name: u.profile?.display_name || u.profile?.real_name || u.name, real_name: u.profile?.real_name || u.real_name || '',
        handle: u.name, avatar: u.profile?.image_72 || '', is_bot: u.is_bot, deleted: u.deleted,
      }));
      const fresh = {};
      for (const u of res) if (!state.users.has(u.id)) fresh[u.id] = u;
      putUsers(fresh);
    } catch { /* edge not available */ }
    cache.set(q, res);
    return res;
  };
})();

// ---------------------------------------------------------------- the composer

const TOOLBAR = [
  ['bold', 'bold', `Bold (${MOD}B)`],
  ['italic', 'italic', `Italic (${MOD}I)`],
  ['underline', 'underline', `Underline (${MOD}U)`],
  ['strike', 'strike', `Strikethrough (${MOD}Shift X)`],
  ['style', 'paragraph', 'Text style', true],
  'sep',
  ['link', 'link', `Link (${MOD}Shift U)`],
  ['ol', 'ol', `Ordered list (${MOD}Shift 7)`],
  ['ul', 'ul', `Bulleted list (${MOD}Shift 8)`],
  'sep',
  ['quote', 'quote', `Blockquote (${MOD}Shift 9)`],
  ['code', 'code', `Code (${MOD}Shift C)`],
  ['codeblock', 'codeBlock', `Code block (${MOD}Alt Shift C)`],
];

export class Composer {
  // opts: {channel, threadTs, placeholder, mode:'message'|'edit', draftKey, initialHTML, broadcastLabel,
  //        onSend({text, blocks, html, files, broadcast}) -> Promise|void, onCancel(), onEditLast()}
  constructor(opts) {
    this.opts = opts;
    this.files = [];
    this.ac = null;
    this.lastTyping = 0;
    this.el = h(this.template());
    this.box = this.el.querySelector('.c-wysiwyg_container');
    this.editor = this.el.querySelector('.ql-editor');
    this.filesEl = this.el.querySelector('.c-wysiwyg_container__files');
    this.sendBtn = this.el.querySelector('.c-wysiwyg_container__send');
    this.hint = this.el.querySelector('.p-notification_bar__section--right');
    this.typingEl = this.el.querySelector('.p-notification_bar__section--left');
    this.bind();
    const draft = opts.draftKey && state.drafts.get(opts.draftKey);
    if (opts.initialHTML != null) this.editor.innerHTML = htmlToEditor(opts.initialHTML);
    else if (draft) this.editor.innerHTML = draft;
    this.refresh();
  }

  template() {
    const o = this.opts;
    const edit = o.mode === 'edit';
    const tb = TOOLBAR.map((t) => t === 'sep' ? '<span class="p-composer__separator"></span>' : `<button class="c-wysiwyg_container__button${t[3] ? ' c-wysiwyg_container__button--wide' : ''}" type="button" data-fmt="${t[0]}" aria-label="${esc(t[2])}" data-tip="${esc(t[2])}">${icon(t[1], 18)}${t[3] ? icon('chevronDown', 18, 'c-wysiwyg_container__button_caret') : ''}</button>`).join('');
    const hidden = !edit && !state.prefs.showFormatting;
    return `<div class="p-message_pane_input${edit ? ' p-message_pane_input--edit' : ''}">
<div class="c-wysiwyg_container${hidden ? ' c-wysiwyg_container--no_formatting' : ''}">
<div class="c-wysiwyg_container__formatting">${tb}</div>
<div class="c-wysiwyg_container__files" hidden></div>
<div class="ql-editor" contenteditable="true" role="textbox" aria-multiline="true" spellcheck="true" data-placeholder="${esc(o.placeholder || 'Message')}"></div>
<div class="c-wysiwyg_container__footer">${edit ? `<span class="c-wysiwyg_container__spacer"></span><button class="c-button c-button--outline c-button--small" type="button" data-act="cancel">Cancel</button><button class="c-button c-button--primary c-button--small" type="button" data-act="save">Save</button>` : `
<button class="c-wysiwyg_container__button c-wysiwyg_container__button--plus" type="button" data-act="attach" data-tip="Attach">${icon('plus', 18)}</button>
<button class="c-wysiwyg_container__button" type="button" data-act="toggle-format" data-tip="${hidden ? 'Show' : 'Hide'} formatting">${icon('formatting', 18)}</button>
<button class="c-wysiwyg_container__button" type="button" data-act="emoji" data-tip="Emoji">${icon('emoji', 18)}</button>
<button class="c-wysiwyg_container__button" type="button" data-act="mention" data-tip="Mention someone">${icon('at', 18)}</button>
<span class="c-wysiwyg_container__footer_divider"></span>
<button class="c-wysiwyg_container__button" type="button" data-act="video" data-tip="Record video clip">${icon('video', 18)}</button>
<button class="c-wysiwyg_container__button" type="button" data-act="audio" data-tip="Record audio clip">${icon('mic', 18)}</button>
<span class="c-wysiwyg_container__footer_divider"></span>
<button class="c-wysiwyg_container__button" type="button" data-act="slash" data-tip="Run shortcut">${icon('slash', 18)}</button>
<span class="c-wysiwyg_container__spacer"></span>
<span class="c-wysiwyg_container__send"><button type="button" data-act="send" class="c-wysiwyg_container__send_main" aria-label="Send now" data-tip="Send now">${icon('send', 16)}</button><span class="c-wysiwyg_container__send__sep"></span><button type="button" data-act="send-options" class="c-wysiwyg_container__send_options" aria-label="Schedule for later" data-tip="Schedule for later">${icon('chevronDown', 16)}</button></span>
<input type="file" class="p-hidden_file_input" multiple hidden>`}
</div>
</div>
${o.broadcastLabel ? `<label class="p-thread_broadcast_option"><input type="checkbox" class="p-thread_broadcast_option__input"> Also send to ${esc(o.broadcastLabel)}</label>` : ''}
${edit ? '' : `<div class="p-notification_bar"><div class="p-notification_bar__section p-notification_bar__section--left"></div><div class="p-notification_bar__section p-notification_bar__section--right"><span class="p-notification_bar__formatting"><b>Shift + Return</b> to add a new line</span></div></div>`}
</div>`;
  }

  bind() {
    const ed = this.editor;
    ed.addEventListener('focus', () => this.box.classList.add('c-wysiwyg_container--focus'));
    ed.addEventListener('blur', () => { this.box.classList.remove('c-wysiwyg_container--focus'); setTimeout(() => { if (document.activeElement !== ed) this.closeAC(); }, 150); });
    ed.addEventListener('keydown', (e) => this.onKey(e));
    ed.addEventListener('input', (e) => this.onInput(e));
    ed.addEventListener('paste', (e) => this.onPaste(e));
    ed.addEventListener('drop', (e) => { if (e.dataTransfer?.files?.length) { e.preventDefault(); this.addFiles(e.dataTransfer.files); } });
    ed.addEventListener('click', () => this.updateAC());
    this.el.querySelector('.c-wysiwyg_container__formatting').addEventListener('mousedown', (e) => {
      const b = e.target.closest('[data-fmt]');
      if (!b) return;
      e.preventDefault();
      this.format(b.dataset.fmt);
    });
    this.el.addEventListener('click', (e) => {
      const b = e.target.closest('[data-act]');
      if (!b) return;
      this.action(b.dataset.act, b);
    });
    const fi = this.el.querySelector('.p-hidden_file_input');
    fi?.addEventListener('change', () => { this.addFiles(fi.files); fi.value = ''; });
    document.addEventListener('selectionchange', this.selHandler = () => { if (document.activeElement === ed) this.updateActive(); });
  }

  destroy() { document.removeEventListener('selectionchange', this.selHandler); this.closeAC(); }

  focus(atEnd = true) {
    this.editor.focus();
    if (atEnd) {
      const r = document.createRange();
      r.selectNodeContents(this.editor);
      r.collapse(false);
      const s = getSelection(); s.removeAllRanges(); s.addRange(r);
    }
  }

  isEmpty() {
    return !this.editor.textContent.replace(/[​\s]/g, '') && !this.editor.querySelector('img, .c-member_slug, .c-channel_entity, .c-mention_badge') && !this.files.length;
  }

  refresh() {
    const empty = this.isEmpty();
    this.editor.classList.toggle('ql-blank', !this.editor.textContent && !this.editor.querySelector('img, li, pre, blockquote, .c-member_slug'));
    this.sendBtn?.classList.toggle('c-wysiwyg_container__send--active', !empty);
    this.hint?.classList.toggle('p-notification_bar__section--visible', !empty);
    if (this.opts.draftKey) {
      if (empty) state.drafts.delete(this.opts.draftKey);
      else state.drafts.set(this.opts.draftKey, this.editor.innerHTML);
    }
  }

  setTyping(text) { if (this.typingEl) this.typingEl.textContent = text || ''; }

  clear() {
    this.editor.innerHTML = '';
    this.files.forEach((f) => f.url && URL.revokeObjectURL(f.url));
    this.files = [];
    this.renderFiles();
    const cb = this.el.querySelector('.p-thread_broadcast_option__input');
    if (cb) cb.checked = false;
    this.refresh();
  }

  value() {
    const elements = editorToRichText(this.editor);
    const text = richTextToMrkdwn(elements);
    const blocks = elements.length ? [{ type: 'rich_text', elements }] : undefined;
    return { text, blocks, html: elements.length ? richTextToHTML(elements) : '', elements, files: [...this.files], broadcast: !!this.el.querySelector('.p-thread_broadcast_option__input')?.checked };
  }

  async submit() {
    if (this.ac && this.acPick()) return;
    if (this.isEmpty()) return;
    const v = this.value();
    // slash commands go to chat.command
    const m = this.opts.mode !== 'edit' && v.text.match(/^\/([\w-]+)(?:\s+([\s\S]*))?$/);
    if (m && !v.files.length) {
      const cmds = await loadCommands();
      if (cmds.some((c) => c.name === '/' + m[1]) || ['me', 'shrug', 'msg', 'remind', 'status', 'away', 'topic', 'invite', 'leave'].includes(m[1])) {
        this.clear();
        this.runCommand('/' + m[1], m[2] || '');
        return;
      }
    }
    if (this.opts.mode !== 'edit') this.clear();
    try { await this.opts.onSend?.(v); } catch (e) { console.error(e); }
  }

  async runCommand(command, text) {
    try {
      if (command === '/shrug') {
        await this.opts.onSend?.({ text: (text ? text + ' ' : '') + '¯\\_(ツ)_/¯', html: '', files: [] });
        return;
      }
      await api.call('chat.command', { channel: this.opts.channel, command, text, ...(this.opts.threadTs ? { thread_ts: this.opts.threadTs } : {}) });
    } catch (e) {
      ext.popups?.toast?.(`${command} failed: ${e.code || e.message}`);
    }
  }

  // ---- keyboard

  onKey(e) {
    const mod = e.metaKey || e.ctrlKey;
    if (this.ac) {
      if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey && this.ac.items.length > 1 && false)) { e.preventDefault(); this.acMove(1); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); this.acMove(-1); return; }
      if (e.key === 'Enter' || e.key === 'Tab') { if (this.acPick()) { e.preventDefault(); return; } }
      if (e.key === 'Escape') { e.preventDefault(); this.closeAC(); return; }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey && !e.isComposing) {
      if (this.inBlock('PRE') && !mod) { /* Slack: Enter in code block still sends */ }
      e.preventDefault();
      if (this.opts.mode === 'edit') this.action('save');
      else this.submit();
      return;
    }
    if (e.key === 'Enter' && (e.shiftKey || e.altKey)) {
      e.preventDefault();
      this.newline();
      return;
    }
    if (e.key === 'Escape' && this.opts.mode === 'edit') { e.preventDefault(); this.opts.onCancel?.(); return; }
    if (e.key === 'ArrowUp' && this.isEmpty() && this.opts.onEditLast && !mod) { e.preventDefault(); this.opts.onEditLast(); return; }
    if (e.key === 'Backspace' && this.atLineStart()) {
      const li = this.inBlock('LI'), q = this.inBlock('BLOCKQUOTE'), pre = this.inBlock('PRE');
      if ((li || q || pre) && this.currentLineEmpty()) { e.preventDefault(); document.execCommand(li ? (li.parentElement.tagName === 'OL' ? 'insertOrderedList' : 'insertUnorderedList') : 'formatBlock', false, li ? undefined : 'p'); this.refresh(); return; }
    }
    if (mod && !e.altKey) {
      const k = e.key.toLowerCase();
      const map = e.shiftKey ? { x: 'strike', u: 'link', c: 'code', 7: 'ol', '&': 'ol', 8: 'ul', '*': 'ul', 9: 'quote', '(': 'quote' } : { b: 'bold', i: 'italic', u: 'underline' };
      if (map[k]) { e.preventDefault(); this.format(map[k]); return; }
    }
    if (mod && e.altKey && e.shiftKey && e.key.toLowerCase() === 'c') { e.preventDefault(); this.format('codeblock'); }
  }

  newline() {
    const pre = this.inBlock('PRE');
    if (pre) { document.execCommand('insertText', false, '\n'); return; }
    const li = this.inBlock('LI');
    if (li && this.currentLineEmpty()) { document.execCommand(li.parentElement.tagName === 'OL' ? 'insertOrderedList' : 'insertUnorderedList'); return; }
    const q = this.inBlock('BLOCKQUOTE');
    if (q && this.currentLineEmpty()) { document.execCommand('formatBlock', false, 'p'); return; }
    if (!this.editor.firstElementChild || !this.inBlock('P') && !li && !q) {
      // make sure the content is wrapped in paragraphs so lines serialize cleanly
      document.execCommand('defaultParagraphSeparator', false, 'p');
    }
    document.execCommand('insertParagraph');
    this.refresh();
  }

  inBlock(tag) {
    const s = getSelection();
    if (!s.rangeCount) return null;
    let n = s.anchorNode;
    while (n && n !== this.editor) { if (n.nodeType === 1 && n.tagName === tag) return n; n = n.parentNode; }
    return null;
  }

  currentBlock() {
    const s = getSelection();
    let n = s.anchorNode;
    while (n && n.parentNode !== this.editor && n !== this.editor) {
      if (n.nodeType === 1 && /^(LI|P|DIV|PRE|BLOCKQUOTE)$/.test(n.tagName)) return n;
      n = n.parentNode;
    }
    return n === this.editor ? null : n;
  }

  currentLineEmpty() {
    const b = this.currentBlock();
    return !b || !b.textContent.replace(/​/g, '').length;
  }

  atLineStart() {
    const s = getSelection();
    if (!s.rangeCount || !s.isCollapsed) return false;
    const b = this.currentBlock();
    if (!b) return true;
    const r = document.createRange();
    r.selectNodeContents(b);
    r.setEnd(s.anchorNode, s.anchorOffset);
    return !r.toString().replace(/​/g, '').length;
  }

  // ---- formatting

  format(kind) {
    this.editor.focus();
    document.execCommand('defaultParagraphSeparator', false, 'p');
    switch (kind) {
      case 'bold': document.execCommand('bold'); break;
      case 'italic': document.execCommand('italic'); break;
      case 'underline': document.execCommand('underline'); break;
      case 'strike': document.execCommand('strikeThrough'); break;
      case 'ol': document.execCommand('insertOrderedList'); break;
      case 'ul': document.execCommand('insertUnorderedList'); break;
      case 'quote': document.execCommand('formatBlock', false, this.inBlock('BLOCKQUOTE') ? 'p' : 'blockquote'); break;
      case 'codeblock': {
        if (this.inBlock('PRE')) document.execCommand('formatBlock', false, 'p');
        else document.execCommand('formatBlock', false, 'pre');
        break;
      }
      case 'code': this.wrapInline('code'); break;
      case 'link': this.insertLink(); break;
      case 'style': {
        const b = this.el.querySelector('[data-fmt=style]');
        const pick = (k) => () => { this.editor.focus(); if (k === 'p') document.execCommand('formatBlock', false, 'p'); else this.format(k); };
        ext.popups?.openMenu?.(b, [
          { label: 'Normal text', onClick: pick('p') },
          { label: 'Bulleted list', hint: `${MOD}Shift 8`, onClick: pick('ul') },
          { label: 'Numbered list', hint: `${MOD}Shift 7`, onClick: pick('ol') },
          { label: 'Quote', hint: `${MOD}Shift 9`, onClick: pick('quote') },
          { label: 'Code block', hint: `${MOD}Alt Shift C`, onClick: pick('codeblock') },
        ]);
        return;
      }
      default: break;
    }
    this.refresh();
    this.updateActive();
  }

  wrapInline(tag) {
    const s = getSelection();
    if (!s.rangeCount) return;
    const existing = this.inBlock(tag.toUpperCase());
    if (existing) { // unwrap
      existing.replaceWith(...existing.childNodes);
      return;
    }
    const r = s.getRangeAt(0);
    const el = document.createElement(tag);
    if (r.collapsed) {
      el.textContent = ZW;
      r.insertNode(el);
      const nr = document.createRange(); nr.setStart(el.firstChild, 1); nr.collapse(true);
      s.removeAllRanges(); s.addRange(nr);
    } else {
      el.appendChild(r.extractContents());
      r.insertNode(el);
      const after = document.createTextNode(ZW);
      el.after(after);
      const nr = document.createRange(); nr.setStart(after, 1); nr.collapse(true);
      s.removeAllRanges(); s.addRange(nr);
    }
  }

  insertLink() {
    const s = getSelection();
    const sel = s.rangeCount ? s.getRangeAt(0).cloneRange() : null;
    const text = sel ? sel.toString() : '';
    const done = (url, label) => {
      if (!url) return;
      if (!/^[a-z]+:/i.test(url)) url = 'https://' + url;
      this.editor.focus();
      const s2 = getSelection();
      if (sel) { s2.removeAllRanges(); s2.addRange(sel); }
      if (sel && !sel.collapsed && !label) document.execCommand('createLink', false, url);
      else document.execCommand('insertHTML', false, `<a href="${esc(url)}">${esc(label || text || url)}</a>${ZW}`);
      this.refresh();
    };
    if (ext.popups?.openModal) {
      const body = h(`<div><label class="c-label">Text</label><input class="c-input_text" data-f="text" value="${esc(text)}"><label class="c-label">Link</label><input class="c-input_text" data-f="url" placeholder="https://"></div>`);
      ext.popups.openModal({
        title: text ? 'Add link' : 'Add link', body,
        buttons: [{ label: 'Cancel' }, { label: 'Save', primary: true, onClick: () => done(body.querySelector('[data-f=url]').value.trim(), body.querySelector('[data-f=text]').value.trim()) }],
      });
      setTimeout(() => body.querySelector(text ? '[data-f=url]' : '[data-f=text]')?.focus(), 30);
    } else done(prompt('Link URL'), text);
  }

  updateActive() {
    const q = (c) => { try { return document.queryCommandState(c); } catch { return false; } };
    const st = { bold: q('bold'), italic: q('italic'), underline: q('underline'), strike: q('strikeThrough'), ol: !!this.inBlock('OL'), ul: !!this.inBlock('UL'), quote: !!this.inBlock('BLOCKQUOTE'), code: !!this.inBlock('CODE'), codeblock: !!this.inBlock('PRE'), link: !!this.inBlock('A') };
    for (const b of this.el.querySelectorAll('[data-fmt]')) b.classList.toggle('c-wysiwyg_container__button--active', !!st[b.dataset.fmt]);
  }

  // ---- input: live markup, typing, autocomplete

  onInput(e) {
    if (e.inputType === 'insertText' && e.data) {
      if ('*_~`'.includes(e.data)) this.liveInline(e.data);
      else if (e.data === ' ') this.liveBlock();
    }
    // keep content in paragraphs: a bare text node at the root becomes a <p>
    const first = this.editor.firstChild;
    if (first && first.nodeType === 3 && this.editor.childNodes.length === 1) {
      const p = document.createElement('p');
      const s = getSelection();
      const off = s.anchorNode === first ? s.anchorOffset : first.length;
      first.replaceWith(p); p.appendChild(first);
      const r = document.createRange(); r.setStart(first, off); r.collapse(true); s.removeAllRanges(); s.addRange(r);
    }
    if (this.editor.innerHTML === '<br>' || this.editor.innerHTML === '<p><br></p>') this.editor.innerHTML = '';
    this.refresh();
    this.updateAC();
    this.sendTyping();
  }

  sendTyping() {
    const now = Date.now();
    if (this.opts.mode === 'edit' || now - this.lastTyping < 3000 || this.isEmpty()) return;
    this.lastTyping = now;
    api.post('typing', { channel: this.opts.channel, thread_ts: this.opts.threadTs || undefined }).catch(() => {});
  }

  liveInline(ch) {
    const s = getSelection();
    if (!s.isCollapsed) return;
    const node = s.anchorNode;
    if (!node || node.nodeType !== 3 || this.inBlock('PRE') || this.inBlock('CODE')) return;
    const before = node.nodeValue.slice(0, s.anchorOffset);
    if (ch === '`' && /(^|\n)```$/.test(before) && this.currentBlock()?.textContent.replace(/​/g, '') === '```') {
      node.nodeValue = node.nodeValue.slice(0, s.anchorOffset - 3) + node.nodeValue.slice(s.anchorOffset);
      document.execCommand('formatBlock', false, 'pre');
      return;
    }
    const e = ch === '*' ? '\\*' : ch;
    const re = ch === '`'
      ? new RegExp('(^|[\\s(\\u200B])`([^`]+)`$')
      : new RegExp(`(^|[\\s(\\u200B])${e}([^${e}\\s](?:[^${e}]*[^${e}\\s])?)${e}$`);
    const m = before.match(re);
    if (!m) return;
    const start = m.index + m[1].length;
    const tag = { '*': 'b', _: 'i', '~': 's', '`': 'code' }[ch];
    const r = document.createRange();
    r.setStart(node, start);
    r.setEnd(node, s.anchorOffset);
    r.deleteContents();
    const el = document.createElement(tag);
    el.textContent = m[2];
    r.insertNode(el);
    const after = document.createTextNode(ZW);
    el.after(after);
    const nr = document.createRange(); nr.setStart(after, 1); nr.collapse(true);
    s.removeAllRanges(); s.addRange(nr);
  }

  liveBlock() {
    if (this.inBlock('PRE') || this.inBlock('LI')) return;
    const s = getSelection();
    const node = s.anchorNode;
    if (!node || node.nodeType !== 3) return;
    const b = this.currentBlock();
    const lineText = b ? b.textContent : node.nodeValue;
    const before = node.nodeValue.slice(0, s.anchorOffset).replace(/​/g, '');
    if (lineText.replace(/​/g, '').trim() !== before.trim()) return; // only at start of the line
    let cmd = null;
    if (/^[-*•] $/.test(before)) cmd = 'insertUnorderedList';
    else if (/^1[.)] $/.test(before)) cmd = 'insertOrderedList';
    else if (/^> $/.test(before) || before === '&gt; ') cmd = 'quote';
    if (!cmd) return;
    node.nodeValue = node.nodeValue.slice(s.anchorOffset);
    const r = document.createRange(); r.setStart(node, 0); r.collapse(true); s.removeAllRanges(); s.addRange(r);
    if (cmd === 'quote') document.execCommand('formatBlock', false, 'blockquote');
    else document.execCommand(cmd);
  }

  onPaste(e) {
    const dt = e.clipboardData;
    if (!dt) return;
    if (dt.files && dt.files.length) { e.preventDefault(); this.addFiles(dt.files); return; }
    const html = dt.getData('text/html');
    const text = dt.getData('text/plain');
    e.preventDefault();
    if (html && /<(b|strong|i|em|a|ul|ol|code|pre|blockquote)\b/i.test(html)) {
      document.execCommand('insertHTML', false, sanitizePaste(html));
    } else {
      const lines = text.replace(/\r\n?/g, '\n').split('\n');
      if (lines.length === 1) document.execCommand('insertText', false, text);
      else document.execCommand('insertHTML', false, lines.map((l) => `<p>${esc(l) || '<br>'}</p>`).join(''));
    }
    this.refresh();
  }

  // ---- autocomplete

  tokenBeforeCaret() {
    const s = getSelection();
    if (!s.rangeCount || !s.isCollapsed) return null;
    const node = s.anchorNode;
    if (!node || node.nodeType !== 3 || !this.editor.contains(node) || this.inBlock('PRE') || this.inBlock('CODE')) return null;
    const before = node.nodeValue.slice(0, s.anchorOffset);
    let m = before.match(/(?:^|[\s(​])([@#])([^\s@#]{0,40})$/);
    if (m) return { kind: m[1] === '@' ? 'user' : 'channel', q: m[2], node, start: s.anchorOffset - m[2].length - 1, end: s.anchorOffset };
    m = before.match(/(?:^|[\s(​]):([\w+\-']{2,40})$/);
    if (m) return { kind: 'emoji', q: m[1], node, start: s.anchorOffset - m[1].length - 1, end: s.anchorOffset };
    m = before.match(/^\/([\w-]*)$/);
    if (m && this.opts.mode !== 'edit' && this.editor.textContent.replace(/​/g, '').trim() === before.trim()) return { kind: 'command', q: m[1], node, start: 0, end: s.anchorOffset };
    return null;
  }

  async updateAC() {
    const tok = this.tokenBeforeCaret();
    if (!tok) { this.closeAC(); return; }
    const seq = (this.acSeq = (this.acSeq || 0) + 1);
    let items = [];
    if (tok.kind === 'user') {
      items = localUsers(tok.q, 8).map((u) => ({ kind: 'user', id: u.id, u }));
      if ('here'.startsWith(tok.q.toLowerCase()) || 'channel'.startsWith(tok.q.toLowerCase()) || 'everyone'.startsWith(tok.q.toLowerCase())) {
        for (const r of ['here', 'channel', 'everyone']) if (r.startsWith(tok.q.toLowerCase()) && tok.q.length > 0) items.push({ kind: 'broadcast', id: r });
      }
      this.showAC(tok, items);
      if (tok.q.length >= 2) {
        const remote = await remoteUserSearch(tok.q);
        if (seq !== this.acSeq) return;
        const known = new Set(items.map((i) => i.id));
        for (const u of remote) if (!known.has(u.id) && !u.deleted) items.push({ kind: 'user', id: u.id, u: state.users.get(u.id) || u });
        items = items.slice(0, 10);
      }
    } else if (tok.kind === 'channel') {
      const out = [];
      for (const c of state.convs.values()) {
        if (c.kind !== 'channel' && c.kind !== 'private') continue;
        const s = tok.q ? scoreName(tok.q, c.name) : 1;
        if (s) out.push([s, c]);
      }
      out.sort((a, b) => b[0] - a[0] || a[1].name.localeCompare(b[1].name));
      items = out.slice(0, 10).map(([, c]) => ({ kind: 'channel', id: c.id, c }));
    } else if (tok.kind === 'emoji') {
      try { items = (ext.emoji?.searchEmoji?.(tok.q, 10) || []).map((e) => ({ kind: 'emoji', id: e.name, url: e.url })); } catch { items = []; }
    } else if (tok.kind === 'command') {
      const cmds = await loadCommands();
      if (seq !== this.acSeq) return;
      items = cmds.filter((c) => c.name.slice(1).startsWith(tok.q.toLowerCase())).slice(0, 12).map((c) => ({ kind: 'command', id: c.name, c }));
    }
    if (seq !== this.acSeq) return;
    this.showAC(tok, items);
  }

  showAC(tok, items) {
    if (!items.length) { this.closeAC(); return; }
    if (!this.ac) {
      this.ac = { el: h('<div class="c-autocomplete" role="listbox"></div>'), sel: 0 };
      this.el.querySelector('.c-wysiwyg_container').appendChild(this.ac.el);
      this.ac.el.addEventListener('mousedown', (e) => {
        const it = e.target.closest('[data-i]');
        if (!it) return;
        e.preventDefault();
        this.ac.sel = +it.dataset.i;
        this.acPick();
      });
      this.ac.el.addEventListener('mousemove', (e) => {
        const it = e.target.closest('[data-i]');
        if (it && +it.dataset.i !== this.ac.sel) { this.ac.sel = +it.dataset.i; this.renderAC(); }
      });
    }
    const keep = this.ac.items && this.ac.tok?.kind === tok.kind ? this.ac.items[this.ac.sel]?.id : null;
    this.ac.tok = tok;
    this.ac.items = items;
    this.ac.sel = Math.max(0, keep ? items.findIndex((i) => i.id === keep) : 0);
    this.renderAC();
  }

  renderAC() {
    const { items, sel, tok } = this.ac;
    const title = { user: 'People', channel: 'Channels', emoji: 'Emoji matching ' + ':' + tok.q, command: 'Shortcuts and commands' }[tok.kind];
    this.ac.el.innerHTML = `<div class="c-autocomplete__header">${esc(title)}</div>` + items.map((it, i) => {
      let body = '';
      if (it.kind === 'user') {
        const u = it.u;
        const pres = u.presence === 'active' ? 'c-presence--active' : '';
        body = `<span class="c-autocomplete__icon c-autocomplete__avatar"><img src="${esc(avatarFor(u))}" alt=""></span><span class="c-autocomplete__label">${esc(u.name || u.real_name || u.handle)}</span>${u.presence ? `<span class="c-presence ${pres}"></span>` : ''}<span class="c-autocomplete__sub">${esc(u.real_name && u.real_name !== u.name ? u.real_name : '')}</span>`;
      } else if (it.kind === 'broadcast') {
        body = `<span class="c-autocomplete__icon">${icon('users', 18)}</span><span class="c-autocomplete__label">@${esc(it.id)}</span><span class="c-autocomplete__sub">${{ here: 'Notify every online member in this channel.', channel: 'Notify everyone in this channel.', everyone: 'Notify everyone in the workspace.' }[it.id]}</span>`;
      } else if (it.kind === 'channel') {
        body = `<span class="c-autocomplete__icon">${icon(it.c.kind === 'private' ? 'lock' : 'hash', 16)}</span><span class="c-autocomplete__label">${esc(it.c.name)}</span>`;
      } else if (it.kind === 'emoji') {
        body = `<span class="c-autocomplete__icon"><img class="c-emoji" src="${esc(it.url)}" alt=""></span><span class="c-autocomplete__label">:${esc(it.id)}:</span>`;
      } else if (it.kind === 'command') {
        body = `<span class="c-autocomplete__label">${esc(it.c.name)}</span><span class="c-autocomplete__sub">${esc(it.c.usage)}</span><span class="c-autocomplete__desc">${esc(it.c.desc)}</span>`;
      }
      return `<div class="c-autocomplete__item${i === sel ? ' c-autocomplete__item--selected' : ''}" role="option" data-i="${i}">${body}</div>`;
    }).join('');
    this.ac.el.querySelector('.c-autocomplete__item--selected')?.scrollIntoView({ block: 'nearest' });
  }

  acMove(d) {
    const n = this.ac.items.length;
    this.ac.sel = (this.ac.sel + d + n) % n;
    this.renderAC();
  }

  closeAC() {
    if (!this.ac) return;
    this.ac.el.remove();
    this.ac = null;
    this.acSeq = (this.acSeq || 0) + 1;
  }

  acPick() {
    if (!this.ac) return false;
    const it = this.ac.items[this.ac.sel];
    const tok = this.ac.tok;
    this.closeAC();
    if (!it) return false;
    this.editor.focus();
    const s = getSelection();
    const r = document.createRange();
    r.setStart(tok.node, Math.max(0, tok.start));
    r.setEnd(tok.node, Math.min(tok.node.length, tok.end));
    s.removeAllRanges(); s.addRange(r);
    let html;
    if (it.kind === 'user') html = `<a class="c-member_slug c-member_slug--link" data-member-id="${esc(it.id)}" contenteditable="false">@${esc(it.u.name || it.u.real_name || it.u.handle)}</a>&nbsp;`;
    else if (it.kind === 'broadcast') html = `<span class="c-mention_badge" data-broadcast="${esc(it.id)}" contenteditable="false">@${esc(it.id)}</span>&nbsp;`;
    else if (it.kind === 'channel') html = `<a class="c-channel_entity" data-channel-id="${esc(it.id)}" contenteditable="false">#${esc(it.c.name)}</a>&nbsp;`;
    else if (it.kind === 'emoji') { html = `<img class="c-emoji" data-name="${esc(it.id)}" src="${esc(it.url)}" alt=":${esc(it.id)}:">`; try { ext.emoji?.recordEmojiUse?.(it.id); } catch { /* ignore */ } }
    else if (it.kind === 'command') html = esc(it.id) + '&nbsp;';
    document.execCommand('insertHTML', false, html);
    if (it.kind === 'user') ensureUsers([it.id]);
    this.refresh();
    return true;
  }

  insertEmoji(name) {
    const url = ext.emoji?.emojiURL?.(name);
    this.editor.focus();
    if (this.savedRange) { const s = getSelection(); s.removeAllRanges(); s.addRange(this.savedRange); }
    document.execCommand('insertHTML', false, url ? `<img class="c-emoji" data-name="${esc(name)}" src="${esc(url)}" alt=":${esc(name)}:">` : `:${esc(name)}:`);
    try { ext.emoji?.recordEmojiUse?.(name); } catch { /* ignore */ }
    this.refresh();
  }

  // ---- files

  addFiles(list) {
    for (const f of list) {
      this.files.push({ file: f, name: f.name || 'pasted file', url: f.type.startsWith('image/') ? URL.createObjectURL(f) : '' });
    }
    this.renderFiles();
    this.refresh();
    this.editor.focus();
  }

  renderFiles() {
    if (!this.filesEl) return;
    this.filesEl.hidden = !this.files.length;
    this.filesEl.innerHTML = this.files.map((f, i) => f.url
      ? `<div class="c-composer_file"><img src="${esc(f.url)}" alt=""><button class="c-composer_file__remove" data-rm="${i}" aria-label="Remove">${icon('close', 12)}</button></div>`
      : `<div class="c-composer_file c-composer_file--wide"><span class="c-file_entity__icon">${esc((f.name.split('.').pop() || 'FILE').slice(0, 4).toUpperCase())}</span><span class="c-composer_file__name">${esc(f.name)}</span><button class="c-composer_file__remove" data-rm="${i}" aria-label="Remove">${icon('close', 12)}</button></div>`).join('');
    this.filesEl.onclick = (e) => {
      const b = e.target.closest('[data-rm]');
      if (!b) return;
      const [f] = this.files.splice(+b.dataset.rm, 1);
      if (f?.url) URL.revokeObjectURL(f.url);
      this.renderFiles();
      this.refresh();
    };
  }

  // ---- footer actions

  action(act, el) {
    switch (act) {
      case 'send': this.submit(); break;
      case 'save': {
        const v = this.value();
        if (!v.text.trim()) { this.opts.onCancel?.(); return; }
        this.opts.onSend?.(v);
        break;
      }
      case 'cancel': this.opts.onCancel?.(); break;
      case 'attach': this.el.querySelector('.p-hidden_file_input')?.click(); break;
      case 'toggle-format': {
        state.prefs.showFormatting = !state.prefs.showFormatting;
        import('./state.js').then((m) => m.savePrefs());
        document.querySelectorAll('.c-wysiwyg_container').forEach((c) => { if (!c.closest('.p-message_pane_input--edit')) c.classList.toggle('c-wysiwyg_container--no_formatting', !state.prefs.showFormatting); });
        el.dataset.tip = state.prefs.showFormatting ? 'Hide formatting' : 'Show formatting';
        break;
      }
      case 'emoji': {
        const s = getSelection();
        this.savedRange = s.rangeCount && this.editor.contains(s.anchorNode) ? s.getRangeAt(0).cloneRange() : null;
        if (ext.emoji?.openEmojiPicker) ext.emoji.openEmojiPicker(el, (name) => this.insertEmoji(name), {});
        else { const n = prompt('Emoji name'); if (n) this.insertEmoji(n.replace(/:/g, '')); }
        break;
      }
      case 'mention': this.focus(); document.execCommand('insertText', false, (this.editor.textContent && !/\s$/.test(this.editor.textContent) ? ' ' : '') + '@'); this.onInput({}); break;
      case 'slash': this.focus(); if (this.isEmpty()) { document.execCommand('insertText', false, '/'); this.onInput({}); } break;
      case 'video': case 'audio': ext.popups?.toast?.('Clips aren’t supported in yoloslack yet'); break;
      case 'send-options': ext.popups?.toast?.('Scheduled messages aren’t supported yet'); break;
      default: break;
    }
  }
}

function sanitizePaste(html) {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const allowed = new Set(['B', 'STRONG', 'I', 'EM', 'U', 'S', 'STRIKE', 'DEL', 'CODE', 'PRE', 'A', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'P', 'BR', 'DIV']);
  const walk = (n) => {
    for (const c of [...n.childNodes]) {
      if (c.nodeType === 1) {
        walk(c);
        if (!allowed.has(c.tagName)) { c.replaceWith(...c.childNodes); continue; }
        for (const a of [...c.attributes]) if (!(c.tagName === 'A' && a.name === 'href')) c.removeAttribute(a.name);
      } else if (c.nodeType !== 3) c.remove();
    }
  };
  walk(doc.body);
  return doc.body.innerHTML;
}

// uploadFiles posts each file to /api/upload (backend). Returns true if all uploads worked.
export async function uploadFiles(files, { channel, threadTs, text, blocks }) {
  for (let i = 0; i < files.length; i++) {
    const f = files[i];
    const form = new FormData();
    form.append('file', f.file, f.name);
    form.append('channel', channel);
    if (threadTs) form.append('thread_ts', threadTs);
    if (i === files.length - 1 && text) { form.append('initial_comment', text); if (blocks) form.append('blocks', JSON.stringify(blocks)); }
    try { await api.upload(form); } catch (e) {
      ext.popups?.toast?.(e.status === 404 ? 'File uploads need the backend /api/upload endpoint' : `Upload failed: ${e.code || e.message}`);
      return false;
    }
  }
  return true;
}

export { emit };
