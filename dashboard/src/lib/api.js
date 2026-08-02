// api.js — a tiny client over the control-plane API. The dashboard is an
// ordinary API client: no privileged path, no orchestration. Every function
// here maps to one documented endpoint. Credentials come from the session
// (see lib/session.svelte.js) and are never logged.

import { authToken } from './session.svelte.js';

const base = ''; // same origin; the Go binary serves both UI and API

async function req(path, opts = {}) {
  const headers = opts.headers || {};
  if (opts.body !== undefined && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }
  const token = authToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;
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

/**
 * Builds the agent-list query. Filtering is server-side by design: the fleet
 * view must stay usable without loading the whole fleet, so a text query is a
 * request parameter, never a filter over the rows the browser happens to hold.
 *
 * @param {{ state?: string, q?: string, after?: string, limit?: number }} [params]
 */
export function agentListQuery(params = {}) {
  const sp = new URLSearchParams();
  if (params.state) sp.set('state', params.state);
  if (params.q) sp.set('q', params.q);
  if (params.after) sp.set('after', params.after);
  if (params.limit) sp.set('limit', String(params.limit));
  const s = sp.toString();
  return s ? `?${s}` : '';
}

export const api = {
  // Agents
  listAgents: (params = {}, opts = {}) => req(`/v1/agents${agentListQuery(params)}`, opts),
  getAgent: (id) => req(`/v1/agents/${id}`),
  createAgent: (body) => req('/v1/agents', { method: 'POST', body: JSON.stringify(body) }),
  deleteAgent: (id) => req(`/v1/agents/${id}`, { method: 'DELETE' }),
  sendMessage: (id, body) =>
    req(`/v1/agents/${id}/messages`, { method: 'POST', body: JSON.stringify({ body }) }),
  cancel: (id) => req(`/v1/agents/${id}/cancel`, { method: 'POST' }),
  fork: (id, atTurn) =>
    req(`/v1/agents/${id}/fork`, { method: 'POST', body: JSON.stringify({ at_turn: atTurn }) }),
  restore: (id, checkpointId) =>
    req(`/v1/agents/${id}/restore`, {
      method: 'POST',
      body: JSON.stringify({ checkpoint_id: checkpointId })
    }),
  checkpoint: (id) => req(`/v1/agents/${id}/checkpoint`, { method: 'POST' }),
  resolveApproval: (id, approvalId, decision) =>
    req(`/v1/agents/${id}/approvals/${approvalId}`, {
      method: 'POST',
      body: JSON.stringify({ decision })
    }),
  activations: (id) => req(`/v1/agents/${id}/activations`),
  // Workspace + stats
  nodeStats: () => req('/v1/node/stats'),
  workspaceAtTurn: (id, turn) => req(`/v1/agents/${id}/workspace${turn ? `?at_turn=${turn}` : ''}`),
  // Events stream (SSE) — returns an EventSource with Last-Event-ID resume.
  events: (id, lastSeq = 0) => new EventSource(`/v1/agents/${id}/events?after=${lastSeq}`)
};

/**
 * Fetches one file's contents from a checkpoint. Kept outside `api` because it
 * returns text, not JSON, and the caller wants the raw body.
 */
export async function checkpointFileText(id, ckpt, path) {
  const token = authToken();
  const res = await fetch(
    `/v1/agents/${id}/checkpoints/${ckpt}/file?path=${encodeURIComponent(path)}`,
    { headers: token ? { Authorization: `Bearer ${token}` } : {} }
  );
  if (!res.ok) {
    const e = new Error(res.statusText);
    e.status = res.status;
    throw e;
  }
  return res.text();
}
