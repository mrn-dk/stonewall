<script>
  import { api } from '$lib/api.js';
  import { onMount, onDestroy } from 'svelte';
  import { page } from '$app/stores';

  let id = $page.params.id;
  let agent = null;
  let activations = [];
  let events = [];
  let selectedTurn = null;
  let workspace = null;
  let fileContent = null;
  let activeFile = null;
  let es = null;
  let connected = true;
  let err = null;
  let actionErr = null;
  let busy = { input: false, cancel: false };
  let inputBody = '';
  let isReadonly = false;

  onMount(load);
  onDestroy(() => es && es.close());

  async function load() {
    try {
      [agent, activations] = await Promise.all([
        api.getAgent(id),
        api.activations(id).then(r => r.activations || []).catch(() => [])
      ]);
      // Stream events (the durable log). Resume from the last seq we have.
      const firstBatch = await api.listAgents(`?limit=1`).catch(() => null);
      void firstBatch;
      connectEvents(0);
    } catch (e) {
      if (e.status === 404) err = { message: 'agent not found', request_id: e.request_id };
      else err = e;
    }
  }

  function connectEvents(after) {
    es = api.events(id, after);
    es.onopen = () => (connected = true);
    es.onerror = () => (connected = false);
    es.addEventListener('message', (e) => {
      const ev = JSON.parse(e.data);
      events = [...events, ev];
      if (ev.kind === 'turn') selectedTurn = selectedTurn ?? ev.turn;
      es.close();
      connectEvents(ev.seq);
    });
  }

  $: turns = buildTimeline(events);
  $: transcript = events.filter(isConversational);
  $: lastSeq = events.length ? events[events.length - 1].seq : 0;

  function buildTimeline(evs) {
    const out = [];
    for (const e of evs) {
      if (e.kind === 'turn' || e.kind === 'run_start' || e.kind === 'run_end' || e.kind === 'checkpoint') {
        out.push(e);
      }
    }
    return out;
  }
  function isConversational(e) {
    return e.kind === 'message' || e.kind === 'llm_call' || e.kind === 'tool_intent' || e.kind === 'tool_result' || e.kind === 'workspace_modified';
  }

  async function selectTurn(t) {
    selectedTurn = t;
    fileContent = null; activeFile = null;
    try {
      workspace = await api.workspaceAtTurn(id, t);
    } catch (e) {
      if (e.status === 404) workspace = { files: [], _none: true };
      else err = e;
    }
  }

  async function showFile(path) {
    activeFile = path;
    fileContent = '…';
    try {
      const cp = workspace.checkpoint_id;
      const res = await fetch(`/v1/agents/${id}/checkpoints/${cp}/file?path=${encodeURIComponent(path)}`);
      if (res.status >= 400) { fileContent = null; return; }
      const txt = await res.text();
      // Cap rendered size (the dashboard renders defensively; huge files are truncated).
      fileContent = txt.length > 200000 ? txt.slice(0, 200000) + `\n… [truncated ${txt.length - 200000} bytes]` : txt;
    } catch (e) {
      fileContent = `error: ${e.message}`;
    }
  }

  async function sendInput() {
    if (!inputBody.trim()) return;
    busy.input = true; actionErr = null;
    try {
      await api.sendMessage(id, inputBody);
      inputBody = '';
    } catch (e) {
      if (e.status === 403) isReadonly = true; else actionErr = e;
    } finally { busy.input = false; }
  }

  async function cancel() {
    if (!confirm('Cancel this agent?')) return;
    busy.cancel = true; actionErr = null;
    try { await api.cancel(id); }
    catch (e) { if (e.status === 403) isReadonly = true; else actionErr = e; }
    finally { busy.cancel = false; }
  }

  function fmtBytes(n) { if (!n) return '–'; const u = ['B','KiB','MiB','GiB']; let i=0; while(n>=1024&&i<u.length-1){n/=1024;i++;} return `${n.toFixed(i?1:0)} ${u[i]}`; }
  function time(t) { return t && !t.startsWith('0001') ? new Date(t).toLocaleTimeString() : ''; }
</script>

{#if err}
  <p class="error">⚠ {err.message}{#if err.request_id} <span class="small">(request {err.request_id})</span>{/if}</p>
{:else if !agent}
  <p class="muted">loading…</p>
{:else}
  <div class="head">
    <div>
      <h1 class="code">{agent.id}</h1>
      <div class="small muted">
        <span class="state-pill state-{agent.state}">{agent.state}</span>
        turn {agent.last_turn}
        {#if agent.parent_id}<span class="crumb"> · fork of <a href={`/agents/${agent.parent_id}`}>{agent.parent_id}</a> @ turn {agent.parent_turn}</span>{/if}
        · image {agent.image}
      </div>
    </div>
    <div class="actions">
      {#if !isReadonly}
        <input bind:value={inputBody} placeholder="send input…" aria-label="message body" on:keydown={(e)=>e.key==='Enter'&&sendInput()} />
        <button class="btn" onclick={sendInput} disabled={busy.input}>send</button>
        <button class="btn ghost" onclick={cancel} disabled={busy.cancel}>cancel</button>
      {:else}
        <span class="small muted">read-only</span>
      {/if}
    </div>
  </div>

  {#if actionErr}<p class="small error">⚠ {actionErr.message}</p>{/if}

  <div class="config card">
    <div><span class="muted small">goal</span> {agent.goal || '–'}</div>
    <div><span class="muted small">model</span> {agent.model || '–'}</div>
    <div><span class="muted small">isolation</span> {agent.isolation} <span class="muted small">checkpoint {agent.checkpoint}</span></div>
    <div class="small">
      <span class="muted">grants</span>
      <span class="code">fs: {Object.entries(agent.grants?.fs||{}).map(([p,m])=>`${p}:${m}`).join(' ')||'none'}</span>
      · <span class="code">net: {(agent.grants?.net||[]).join(',')||'none'}</span>
      · <span class="code">cmd: {(agent.grants?.cmd||[]).join(',')||'none'}</span>
      {#if (agent.grants?.cmd||[]).some(c=>['python','git','find','awk','xargs','bash','sh','node'].includes(c))}
        <span class="warn" title="allow-list controls binaries, not behaviour">⚠ broad command grant — effectively everything in the image; the security boundary is the sandbox</span>
      {/if}
    </div>
  </div>

  <div class="grid3">
    <div class="col col-scroll card">
      <div class="pane-title">timeline</div>
      {#if !connected}<div class="small red">● disconnected — reconnecting</div>{/if}
      {#each turns as e (e.seq)}
        <button class="tl" class:active={selectedTurn===e.turn} onclick={() => e.turn && selectTurn(e.turn)}>
          {#if e.kind === 'checkpoint'}◆ ckpt @ turn {e.turn}{:else}{e.kind} {e.turn ?? ''}{/if}
          <span class="small muted"> {time(e.occurred_at)}</span>
        </button>
      {/each}
      {#if turns.length === 0}<div class="small muted">no turns yet</div>{/if}
    </div>

    <div class="col col-scroll card">
      <div class="pane-title">transcript {#if selectedTurn}· turn {selectedTurn}{/if}</div>
      {#each transcript as e (e.seq)}
        {#if e.turn <= (selectedTurn ?? Infinity) || !selectedTurn}
          <div class="ev">
            <span class="kind">{e.kind}</span>
            <span class="small muted">seq {e.seq} · turn {e.turn}</span>
            <div class="payload">
              {#if e.kind === 'message'}{@html safeMsg(e.payload)}{:else}{pre(e.payload)}{/if}
            </div>
          </div>
        {/if}
      {/each}
    </div>

    <div class="col col-scroll card">
      <div class="pane-title">workspace @ turn {selectedTurn ?? '–'}</div>
      {#if !workspace}<div class="small muted">select a turn</div>
      {:else if workspace._none}<div class="small muted">no checkpoint at this turn</div>
      {:else}
        <ul class="files">
          {#each workspace.files as f (f.path)}
            <li>
              {#if f.is_dir}<span class="small muted">▸ {f.path}</span>
              {:else}<button class="file" class:active={activeFile===f.path} onclick={() => showFile(f.path)}>
                {f.path} <span class="small muted">{fmtBytes(f.size)}</span>
              </button>{/if}
            </li>
          {/each}
        </ul>
        {#if fileContent !== null}
          <pre class="file-content">{fileContent ?? ''}</pre>
        {/if}
      {/if}
    </div>
  </div>

  <div class="activations card">
    <div class="pane-title">activations</div>
    {#each activations as a (a.id)}
      <div class="act"><span class="code">#{a.number}</span> {time(a.started_at)} → {a.ended_at ? time(a.ended_at) : '…'} <span class="small muted">{a.end_reason||'running'}</span></div>
    {/each}
    {#if activations.length === 0}<div class="small muted">none</div>{/if}
  </div>

  <div class="honesty small muted">
    resource usage: granted quotas shown where set; live per-agent CPU/memory is not yet measured (runtime samples are a later change).
  </div>
{/if}

<script context="module">
  function safeMsg(p) {
    if (!p) return '';
    let m = p;
    if (typeof p === 'string') { try { m = JSON.parse(p); } catch (_) { return esc(p); } }
    const role = m.role || ''; const content = m.content || m.body || '';
    return `<b>${esc(role)}</b>: ${esc(typeof content === 'string' ? content : JSON.stringify(content))}`;
  }
  function pre(p) {
    if (p === undefined || p === null) return '';
    if (typeof p === 'string') return p;
    try { return JSON.stringify(p, null, 2); } catch (_) { return String(p); }
  }
  function esc(s) {
    return String(s).replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c]);
  }
</script>

<style>
  .head { display: flex; justify-content: space-between; align-items: flex-start; gap: 1rem; margin-bottom: 0.5rem; flex-wrap: wrap; }
  h1 { margin: 0; font-size: 1rem; }
  .actions { display: flex; gap: 0.4rem; align-items: center; }
  .actions input { background: var(--panel); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 0.3rem 0.5rem; width: 16rem; }
  .config { margin-bottom: 0.5rem; display: flex; flex-direction: column; gap: 0.2rem; }
  .warn { color: var(--amber); font-size: 0.78rem; margin-left: 0.5rem; }
  .crumb a { color: var(--accent); }
  .tl { display: block; width: 100%; text-align: left; background: none; border: none; color: var(--fg); padding: 0.2rem 0.3rem; cursor: pointer; border-radius: 3px; font-size: 0.82rem; }
  .tl:hover { background: var(--bg); }
  .tl.active { background: var(--accent); color: #fff; }
  .ev { padding: 0.3rem 0; border-bottom: 1px solid var(--border); }
  .ev .kind { font-weight: 600; font-size: 0.8rem; margin-right: 0.4rem; }
  .payload { white-space: pre-wrap; font-family: 'SFMono-Regular', Menlo, Consolas, monospace; font-size: 0.8rem; margin-top: 0.2rem; max-height: 10rem; overflow: auto; }
  .files { list-style: none; padding: 0; margin: 0; }
  .file { background: none; border: none; color: var(--accent); cursor: pointer; padding: 0.1rem 0; font-size: 0.82rem; display: block; }
  .file:hover, .file.active { text-decoration: underline; }
  .file-content { background: var(--code-bg); padding: 0.5rem; border-radius: 4px; font-size: 0.78rem; max-height: 16rem; overflow: auto; margin: 0.4rem 0 0; }
  .activations { margin-top: 0.5rem; }
  .act { font-size: 0.82rem; padding: 0.1rem 0; }
  .honesty { margin-top: 0.5rem; }
  .red { color: var(--red); }
  .error { color: var(--red); }
</style>
