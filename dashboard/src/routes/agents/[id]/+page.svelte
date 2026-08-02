<script>
  import { api } from '$lib/api.js';
  import { onMount, onDestroy } from 'svelte';
  import { page } from '$app/stores';

  let id = $page.params.id;
  let agent = $state(null);
  let activations = $state([]);
  let events = $state([]);
  let selectedTurn = $state(null);
  let workspace = $state(null);
  let fileContent = $state(null);
  let activeFile = $state(null);
  let es = null;
  let connected = $state(true);
  let err = $state(null);
  let actionErr = $state(null);
  let busy = $state({ input: false, cancel: false });
  let inputBody = $state('');
  let isReadonly = $state(false);
  let activeActivation = $state(null);
  let showDiff = $state(false);
  let diff = $state(null);
  let resolving = $state({});

  onMount(load);
  onDestroy(() => es && es.close());

  async function load() {
    try {
      [agent, activations] = await Promise.all([
        api.getAgent(id),
        api.activations(id).then(r => r.activations || []).catch(() => [])
      ]);
      connectEvents(0);
    } catch (e) {
      if (e.status === 404) err = { message: 'agent not found', request_id: e.request_id };
      else err = e;
    }
  }

  // The SSE server emits NAMED events (event: turn, event: llm_call, ...), so a
  // generic `message` listener never fires. Bind the handler to each kind.
  // EventSource auto-reconnects and sends Last-Event-ID, which the server reads
  // to resume — so we open one stream at after=0 and let it run.
  const KINDS = ['run_start','run_end','turn','llm_call','tool_intent','tool_result','checkpoint','message','egress','approval','fork','workspace_modified'];

  function onEvent(e) {
    const ev = JSON.parse(e.data);
    events = [...events, ev];
    if (ev.kind === 'turn' && selectedTurn == null) selectedTurn = ev.turn;
  }

  function connectEvents(after) {
    es = api.events(id, after);
    es.onopen = () => (connected = true);
    es.onerror = () => (connected = false);
    KINDS.forEach(k => es.addEventListener(k, onEvent));
  }

  // Derived views: turn is the join key across the three columns.
  let turns = $derived(buildTimeline(events, activeActivation));
  let transcript = $derived(
    events
      .filter(isConversational)
      .filter(e => !activeActivation || e.activation_id === activeActivation)
  );
  let approvals = $derived(events.filter(e => e.kind === 'approval'));

  // Auto-load the workspace once a turn is first selected (from the first turn event).
  $effect(() => {
    if (selectedTurn != null && workspace == null) {
      selectTurn(selectedTurn);
    }
  });

  function buildTimeline(evs, act) {
    const out = [];
    for (const e of evs) {
      if (act && e.activation_id && e.activation_id !== act) continue;
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
    diff = null; showDiff = false;
    try {
      workspace = await api.workspaceAtTurn(id, t);
    } catch (e) {
      if (e.status === 404) workspace = { files: [], _none: true };
      else err = e;
    }
  }

  // loadDiff compares the current checkpoint tree to the previous checkpoint
  // (turn-1, or the nearest ancestor) and classifies files as added/changed/
  // removed/unchanged. A content-addressed manifest makes this a path+size
  // comparison; real chunk-level diff falls out of the same digests.
  async function loadDiff() {
    if (!workspace || workspace._none || !selectedTurn) return;
    try {
      const prev = await api.workspaceAtTurn(id, selectedTurn - 1).catch(() => null);
      if (!prev || prev._none) { diff = { none: true }; return; }
      const cur = new Map(workspace.files.map(f => [f.path, f]));
      const old = new Map(prev.files.map(f => [f.path, f]));
      const added = [], changed = [], removed = [];
      for (const [p, f] of cur) {
        if (!old.has(p)) added.push(p);
        else if (old.get(p).size !== f.size || !sameChunks(old.get(p), f)) changed.push(p);
      }
      for (const p of old.keys()) if (!cur.has(p)) removed.push(p);
      diff = { added, changed, removed };
    } catch (e) { diff = { error: e.message }; }
  }
  function sameChunks(a, b) {
    if ((a.chunks||[]).length !== (b.chunks||[]).length) return false;
    return (a.chunks||[]).every((c, i) => c === (b.chunks||[])[i]);
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

  async function resolveApproval(aid, decision) {
    resolving[aid] = true; actionErr = null;
    try {
      await fetch(`/v1/agents/${id}/approvals/${aid}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ decision })
      });
    } catch (e) { actionErr = e; }
    finally { resolving[aid] = false; }
  }

  function fmtBytes(n) { if (!n) return '–'; const u = ['B','KiB','MiB','GiB']; let i = 0; while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; } return `${n.toFixed(i ? 1 : 0)} ${u[i]}`; }
  function time(t) { return t && !t.startsWith('0001') ? new Date(t).toLocaleTimeString() : ''; }

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
        <input bind:value={inputBody} placeholder="send input…" aria-label="message body" onkeydown={(e) => e.key === 'Enter' && sendInput()} />
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
      <span class="code">fs: {Object.entries(agent.grants?.fs || {}).map(([p, m]) => `${p}:${m}`).join(' ') || 'none'}</span>
      · <span class="code">net: {(agent.grants?.net || []).join(',') || 'none'}</span>
      · <span class="code">cmd: {(agent.grants?.cmd || []).join(',') || 'none'}</span>
      {#if (agent.grants?.cmd || []).some(c => ['python','git','find','awk','xargs','bash','sh','node'].includes(c))}
        <span class="warn" title="allow-list controls binaries, not behaviour">⚠ broad command grant — effectively everything in the image; the security boundary is the sandbox</span>
      {/if}
    </div>
  </div>

  <div class="grid3">
    <div class="col col-scroll card">
      <div class="pane-title">timeline</div>
      {#if !connected}<div class="small red">● disconnected — reconnecting</div>{/if}
      {#each turns as e (e.seq)}
        <button class="tl" class:active={selectedTurn === e.turn} onclick={() => e.turn && selectTurn(e.turn)}>
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
      <div class="pane-title">workspace @ turn {selectedTurn ?? '–'}
        {#if workspace && !workspace._none && selectedTurn}
          <button class="mini" class:on={showDiff} onclick={() => { showDiff = !showDiff; if (showDiff && !diff) loadDiff(); }}>diff vs prev</button>
        {/if}
      </div>
      {#if !workspace}<div class="small muted">select a turn</div>
      {:else if workspace._none}<div class="small muted">no checkpoint at this turn</div>
      {:else}
        {#if showDiff && diff}
          <div class="diff small">
            {#if diff.none}<span class="muted">no previous checkpoint</span>
            {:else}
              {#each diff.added as p}<div class="add">+ {p}</div>{/each}
              {#each diff.changed as p}<div class="chg">~ {p}</div>{/each}
              {#each diff.removed as p}<div class="rm">- {p}</div>{/each}
              {#if !diff.added.length && !diff.changed.length && !diff.removed.length}<span class="muted">no changes</span>{/if}
            {/if}
          </div>
        {/if}
        <ul class="files">
          {#each workspace.files as f (f.path)}
            <li>
              {#if f.is_dir}<span class="small muted">▸ {f.path}</span>
              {:else}<button class="file" class:active={activeFile === f.path} onclick={() => showFile(f.path)}>
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
    <div class="pane-title">activations
      {#if activeActivation}<button class="mini" onclick={() => activeActivation = null}>clear filter</button>{/if}
    </div>
    {#each activations as a (a.id)}
      <button class="act" class:active={activeActivation === a.id} onclick={() => activeActivation = (activeActivation === a.id ? null : a.id)}>
        <span class="code">#{a.number}</span> {time(a.started_at)} → {a.ended_at ? time(a.ended_at) : '…'} <span class="small muted">{a.end_reason || 'running'}</span>
      </button>
    {/each}
    {#if activations.length === 0}<div class="small muted">none</div>{/if}
  </div>

  <div class="approvals card">
    <div class="pane-title">approvals</div>
    {#if approvals.length === 0}<div class="small muted">none</div>
    {:else}
      {#each approvals as a (a.seq)}
        <div class="approval">
          <span class="code">{(a.payload?.approval_id) || ('seq' + a.seq)}</span>
          <span class="small muted">{(a.payload?.decision) || 'pending'}</span>
          {#if !isReadonly && !a.payload?.decision}
            <button class="mini" onclick={() => resolveApproval(a.payload?.approval_id || '', 'approved')} disabled={resolving[a.payload?.approval_id]}>approve</button>
            <button class="mini" onclick={() => resolveApproval(a.payload?.approval_id || '', 'denied')} disabled={resolving[a.payload?.approval_id]}>deny</button>
          {/if}
        </div>
      {/each}
    {/if}
  </div>

  <div class="honesty small muted">
    resource usage: granted quotas shown where set; live per-agent CPU/memory is not yet measured (runtime samples are a later change).
  </div>
{/if}

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
  .honesty { margin-top: 0.5rem; }
  .red { color: var(--red); }
  .error { color: var(--red); }
  .mini { background: none; border: 1px solid var(--border); color: var(--muted); border-radius: 3px; padding: 0 0.4rem; font-size: 0.72rem; cursor: pointer; margin-left: 0.4rem; }
  .mini:hover { color: var(--fg); border-color: var(--fg); }
  .mini.on { background: var(--accent); color: #fff; border-color: var(--accent); }
  .diff { margin: 0.3rem 0; padding: 0.3rem; background: var(--bg); border-radius: 4px; }
  .diff .add { color: var(--green); }
  .diff .chg { color: var(--amber); }
  .diff .rm { color: var(--red); }
  .act { display: block; width: 100%; text-align: left; background: none; border: none; color: var(--fg); padding: 0.15rem 0.3rem; cursor: pointer; border-radius: 3px; font-size: 0.82rem; }
  .act:hover, .act.active { background: var(--bg); }
  .approval { padding: 0.15rem 0; font-size: 0.82rem; display: flex; gap: 0.4rem; align-items: center; }
</style>
