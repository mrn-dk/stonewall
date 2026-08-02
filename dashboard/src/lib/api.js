// api.js — a tiny client over the control-plane API. The dashboard is an
// ordinary API client: no privileged path, no orchestration. Every function
// here maps to one documented endpoint. Credentials are configured via the
// session (see lib/session.js) and never logged.

const base = ''; // same origin; the Go binary serves both UI and API

async function req(path, opts = {}) {
  const headers = opts.headers || {};
  if (opts.body !== undefined && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }
  const sess = session();
  if (sess.token) headers['Authorization'] = `Bearer ${sess.token}`;
  const res = await fetch(base + path, { ...opts, headers });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ code: 'http', message: res.statusText }));
    const e = new Error(err.message || res.statusText);
    e.code = err.code || 'http';
    e.status = res.status;
    e.request_id = err.request_id;
    throw e;
  }
  if (res.status === 204) return null;
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) return res.json();
  return res;
}

export function session(tok) {
  if (tok !== undefined) {
    if (tok) localStorage.setItem('stonewall.token', tok);
    else localStorage.removeItem('stonewall.token');
    return { token: tok };
  }
  const token = localStorage.getItem('stonewall.token');
  return { token: token || '' };
}

export const api = {
  // Agents
  listAgents: (q = '') => req(`/v1/agents${q}`),
  getAgent: (id) => req(`/v1/agents/${id}`),
  createAgent: (body) => req('/v1/agents', { method: 'POST', body: JSON.stringify(body) }),
  deleteAgent: (id) => req(`/v1/agents/${id}`, { method: 'DELETE' }),
  sendMessage: (id, body) => req(`/v1/agents/${id}/messages`, { method: 'POST', body: JSON.stringify({ body }) }),
  cancel: (id) => req(`/v1/agents/${id}/cancel`, { method: 'POST' }),
  fork: (id, atTurn) => req(`/v1/agents/${id}/fork`, { method: 'POST', body: JSON.stringify({ at_turn: atTurn }) }),
  activations: (id) => req(`/v1/agents/${id}/activations`),
  // Workspace + stats
  nodeStats: () => req('/v1/node/stats'),
  workspaceAtTurn: (id, turn) => req(`/v1/agents/${id}/workspace${turn ? `?at_turn=${turn}` : ''}`),
  // Events stream (SSE) — returns an EventSource with Last-Event-ID resume.
  events: (id, lastSeq = 0) => new EventSource(`/v1/agents/${id}/events?after=${lastSeq}`)
};
