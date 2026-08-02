// agents.js — shared vocabulary for rendering agents.
//
// The state list mirrors internal/model.AgentState. Keeping it in one place is
// what stops a fleet-table pill and a workbench badge from disagreeing about
// what "parked" looks like.

export const AGENT_STATES = ['pending', 'running', 'parked', 'completed', 'failed', 'cancelled'];

export const TERMINAL_STATES = ['completed', 'failed', 'cancelled'];

/** Maps a state to its token colour (see the --state-* layer in app.css). */
export const STATE_TONE = {
  pending: 'pending',
  running: 'running',
  parked: 'parked',
  completed: 'terminal',
  failed: 'failed',
  cancelled: 'failed'
};

export const ISOLATION_MODES = ['shared', 'dedicated', 'dedicated_vm'];
export const CHECKPOINT_POLICIES = ['none', 'interval', 'on_write', 'per_turn'];

/**
 * Commands whose presence in the allow-list means the grant is effectively
 * unbounded: an interpreter or a shell can run anything else in the image. The
 * dashboard surfaces this rather than implying the list is a real boundary.
 */
export const BROAD_COMMANDS = ['python', 'python3', 'git', 'find', 'awk', 'xargs', 'bash', 'sh', 'node', 'perl', 'ruby'];

/** @param {{ grants?: { cmd?: string[] } }} agent */
export function broadCommandGrants(agent) {
  return (agent?.grants?.cmd || []).filter((c) => BROAD_COMMANDS.includes(c));
}

/** @param {number | undefined} n */
export function fmtBytes(n) {
  if (!n) return '–';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i ? 1 : 0)} ${units[i]}`;
}

/** Compact relative age, e.g. "4m". Zero-value timestamps render as an em dash. */
export function relTime(t) {
  if (!t || t.startsWith('0001')) return '–';
  const d = (Date.now() - new Date(t).getTime()) / 1000;
  if (d < 60) return `${d | 0}s`;
  if (d < 3600) return `${(d / 60) | 0}m`;
  if (d < 86400) return `${(d / 3600) | 0}h`;
  return `${(d / 86400) | 0}d`;
}

export function timeOfDay(t) {
  return t && !t.startsWith('0001') ? new Date(t).toLocaleTimeString() : '';
}

/** One-line summary of an agent's grants, for the fleet table. */
export function grantsSummary(agent) {
  const fs = Object.entries(agent?.grants?.fs || {})
    .map(([p, mode]) => `${p}:${mode}`)
    .join(' ');
  const cmd = (agent?.grants?.cmd || []).join(',');
  return [fs, cmd && `cmd:${cmd}`].filter(Boolean).join(' ');
}
