<script>
  import { api } from '$lib/api.js';
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';

  let agents = [];
  let stats = null;
  let loading = true;
  let err = null;
  let cursor = '';
  let next = null;

  onMount(load);

  async function load() {
    loading = true;
    try {
      const [list, node] = await Promise.all([api.listAgents(''), api.nodeStats().catch(() => null)]);
      agents = list.agents || [];
      next = list.next_cursor || '';
      stats = node;
    } catch (e) {
      err = e;
    } finally {
      loading = false;
    }
  }

  async function more() {
    if (!next) return;
    const list = await api.listAgents(`?after=${next}&limit=100`);
    agents = [...agents, ...(list.agents || [])];
    next = list.next_cursor || '';
  }

  $: counts = agents.reduce((c, a) => {
    c[a.state] = (c[a.state] || 0) + 1;
    c.total = (c.total || 0) + 1;
    return c;
  }, {});

  function open(a) { goto(`/agents/${a.id}`); }
  function fmtBytes(n) {
    if (!n) return '–';
    const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let i = 0; while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
  }
  function relTime(t) {
    if (!t || t.startsWith('0001')) return '–';
    const d = (Date.now() - new Date(t).getTime()) / 1000;
    if (d < 60) return `${d | 0}s`;
    if (d < 3600) return `${d / 60 | 0}m`;
    if (d < 86400) return `${d / 3600 | 0}h`;
    return `${d / 86400 | 0}d`;
  }
  function grantsSummary(a) {
    const fs = Object.keys(a.grants?.fs || {}).map(p => `${p}:${a.grants.fs[p]}`).join(' ');
    const cmd = (a.grants?.cmd || []).join(',');
    return [fs, cmd ? `cmd:${cmd}` : ''].filter(Boolean).join(' ');
  }
  function readonly() { return false; } // API will 403 mutations; UI hides on 403
</script>

<section>
  <h1>Fleet</h1>

  <div class="strip">
    <div class="stat"><span class="n">{counts.running || 0}</span><span class="l">running</span></div>
    <div class="stat"><span class="n">{counts.parked || 0}</span><span class="l">parked</span></div>
    <div class="stat"><span class="n">{(counts.completed || 0) + (counts.failed || 0) + (counts.cancelled || 0)}</span><span class="l">terminal</span></div>
    <div class="stat"><span class="n">{counts.total || 0}</span><span class="l">total</span></div>
    <div class="sep"></div>
    {#if stats}
      <div class="stat"><span class="n">{stats.cpu_usage_percent.toFixed(0)}%</span><span class="l">cpu</span></div>
      <div class="stat"><span class="n">{fmtBytes(stats.memory_bytes)}</span><span class="l">mem / {fmtBytes(stats.memory_total_bytes)}</span></div>
      <div class="stat"><span class="n">{fmtBytes(stats.storage_bytes)}</span><span class="l">disk / {fmtBytes(stats.storage_total_bytes)}</span></div>
    {:else}
      <div class="stat muted"><span class="n">–</span><span class="l">node stats unavailable</span></div>
    {/if}
  </div>

  {#if err}
    <p class="error">⚠ {err.message} {#if err.request_id}<span class="small"> (request {err.request_id})</span>{/if}
      <button class="btn ghost" onclick={load}>retry</button></p>
  {:else if loading}
    <p class="muted">loading…</p>
  {:else}
    <table aria-label="agents">
      <thead>
        <tr><th>ID</th><th>state</th><th>goal</th><th>image</th><th>grants</th><th>isolation</th><th>model</th><th>act.</th><th>last</th></tr>
      </thead>
      <tbody>
        {#each agents as a (a.id)}
          <tr class="row" onclick={() => open(a)} tabindex="0"
              on:keydown={(e) => (e.key === 'Enter' && open(a))}>
            <td class="code">{a.id}</td>
            <td><span class="state-pill state-{a.state}">{a.state}</span></td>
            <td class="small">{(a.goal || '').slice(0, 48)}</td>
            <td class="small">{a.image}</td>
            <td class="small muted">{grantsSummary(a)}</td>
            <td class="small">{a.isolation}</td>
            <td class="small">{a.model || '–'}</td>
            <td class="small">{a.activation_count}</td>
            <td class="small muted">{relTime(a.updated_at)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if next}<div class="more"><button class="btn ghost" onclick={more}>load more</button></div>{/if}
  {/if}
</section>

<style>
  h1 { margin: 0 0 0.75rem; font-size: 1.1rem; font-weight: 700; }
  .strip { display: flex; gap: 1.5rem; align-items: baseline; margin-bottom: 1rem; flex-wrap: wrap; }
  .stat { display: flex; flex-direction: column; }
  .stat .n { font-weight: 700; font-size: 1.1rem; }
  .stat .l { color: var(--muted); font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .sep { width: 1px; background: var(--border); align-self: stretch; }
  .error { color: var(--red); }
  .more { text-align: center; padding: 0.75rem; }
  td { max-width: 16rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
