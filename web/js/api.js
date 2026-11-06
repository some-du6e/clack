// api.js: JSON client for the Go backend (ui-core).

export class APIError extends Error {
  constructor(code, status) { super(code); this.code = code; this.status = status; }
}

async function parse(res) {
  if (res.status === 204) return { ok: true };
  const ct = res.headers.get('content-type') || '';
  if (!ct.includes('json')) {
    const t = await res.text();
    if (!res.ok) throw new APIError(t.trim() || `http_${res.status}`, res.status);
    return { ok: true, text: t };
  }
  const j = await res.json();
  if (!res.ok || j.ok === false) throw new APIError(j.error || `http_${res.status}`, res.status);
  return j;
}

function qs(params) {
  if (!params) return '';
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v != null && v !== '') p.set(k, v);
  const s = p.toString();
  return s ? '?' + s : '';
}

export const api = {
  async get(path, params) {
    return parse(await fetch(`/api/${path}${qs(params)}`, { headers: { accept: 'application/json' } }));
  },
  async post(path, body) {
    return parse(await fetch(`/api/${path}`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body || {}),
    }));
  },
  // POST /api/call {method, params}; params values are stringified (the backend takes map[string]string).
  async call(method, params = {}) {
    const p = {};
    for (const [k, v] of Object.entries(params)) if (v != null) p[k] = typeof v === 'string' ? v : JSON.stringify(v);
    return this.post('call', { method, params: p });
  },
  async edge(path, body = {}) {
    return this.post('call', { edge: path, body });
  },
  async upload(form) {
    return parse(await fetch('/api/upload', { method: 'POST', body: form }));
  },
};
