<script>
  // The agent-author workbench — the primary surface.
  //
  // Three panes keyed by turn: the operational timeline, the conversation
  // transcript, and the workspace as it existed at the selected turn. Turn is
  // the join key: selecting one in the timeline syncs the other two, which is
  // possible because each turn's checkpoint id is recorded in the event log.
  //
  // The live feed and the audit record are the same object: this reads the
  // durable log over SSE, and EventSource's own Last-Event-ID reconnect is the
  // resume mechanism — there is no separate "archive mode" for a terminal
  // agent.
  import { onDestroy } from 'svelte';
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { api, checkpointFileBlob } from '$lib/api.js';
  import { isImage } from '$lib/files.js';
  import { session } from '$lib/session.svelte.js';
  import { toasts } from '$lib/toasts.svelte.js';
  import { registerPaletteActions } from '$lib/palette.svelte.js';
  import { coalesce } from '$lib/live.svelte.js';

  import * as Resizable from '$lib/components/ui/resizable/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import AgentStateBadge from '$lib/components/agent-state-badge.svelte';
  import ConfirmDialog from '$lib/components/confirm-dialog.svelte';
  import AgentConfig from '$lib/components/workbench/agent-config.svelte';
  import Timeline from '$lib/components/workbench/timeline.svelte';
  import Transcript from '$lib/components/workbench/transcript.svelte';
  import WorkspacePane from '$lib/components/workbench/workspace-pane.svelte';
  import Activations from '$lib/components/workbench/activations.svelte';
  import Approvals from '$lib/components/workbench/approvals.svelte';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import ErrorState from '$lib/components/states/error-state.svelte';

  import Send from '@lucide/svelte/icons/send';
  import Ban from '@lucide/svelte/icons/ban';
  import GitFork from '@lucide/svelte/icons/git-fork';
  import History from '@lucide/svelte/icons/history';
  import Diamond from '@lucide/svelte/icons/diamond';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';

  const id = $derived(page.params.id);

  let agent = $state(null);
  let agentError = $state(null);
  let activations = $state([]);
  let activationsError = $state(null);
  let activationsLoading = $state(true);
  let events = $state([]);
  let connected = $state(true);
  let activeActivation = $state(null);

  let selectedTurn = $state(null);
  // Turn numbers restart per activation, so the selected *row* needs
  // activation+turn to identify it even though the workspace endpoint is keyed
  // by turn alone.
  let selectedKey = $state(null);
  let workspace = $state(null);
  let workspaceLoading = $state(false);
  let workspaceError = $state(null);
  let showDiff = $state(false);
  let diff = $state(null);
  let activeFile = $state(null);
  /** @type {{ kind: 'text'|'image'|'binary', content?: string, imageUrl?: string, size: number, truncated: boolean } | null} */
  let file = $state(null);
  let fileLoading = $state(false);

  let inputBody = $state('');
  let busy = $state({
    input: false,
    cancel: false,
    fork: false,
    restore: false,
    checkpoint: false,
    delete: false,
    approval: false
  });
  let resolving = $state({});
  // `confirm` describes the pending confirmation; `confirmOpen` is the dialog's
  // own open state, so dismissing with Escape or Cancel closes it properly
  // rather than leaving a dialog the parent still believes is open.
  let confirm = $state(null);
  let confirmOpen = $state(false);

  function ask(spec) {
    confirm = spec;
    confirmOpen = true;
  }

  let es = null;
  // Seqs already rendered. The durable log is append-only with unique seqs, so
  // the same seq arriving twice is always a transport artefact — a reconnect
  // replay, say — never new information. Dropping it here keeps the keyed
  // lists valid no matter what the stream does.
  let seenSeqs = new Set();
  onDestroy(() => {
    es?.close();
    releaseFile();
  });

  // Re-entering the workbench for a different agent must not show the previous
  // agent's stream, so everything resets on id change.
  $effect(() => {
    const agentId = id;
    reset();
    loadAgent(agentId);
    loadActivations(agentId);
    connectEvents(agentId);
    return () => {
      refreshAgent.cancel();
      es?.close();
      es = null;
    };
  });

  function reset() {
    agent = null;
    agentError = null;
    activations = [];
    activationsError = null;
    activationsLoading = true;
    events = [];
    connected = true;
    seenSeqs = new Set();
    activeActivation = null;
    selectedTurn = null;
    selectedKey = null;
    workspace = null;
    workspaceError = null;
    diff = null;
    showDiff = false;
    closeFile();
    es?.close();
    es = null;
  }

  async function loadAgent(agentId, { background = false } = {}) {
    try {
      agent = await api.getAgent(agentId);
      agentError = null;
    } catch (e) {
      // A failed background refresh keeps the agent we already have on screen.
      if (background) return;
      agentError = e.status === 404 ? { message: 'Agent not found', request_id: e.request_id } : e;
    }
  }

  async function loadActivations(agentId = id, { background = false } = {}) {
    if (!background) activationsLoading = true;
    try {
      const res = await api.activations(agentId);
      activations = res.activations || [];
      activationsError = null;
    } catch (e) {
      if (!background) activationsError = e;
    } finally {
      if (!background) activationsLoading = false;
    }
  }

  // The server emits NAMED events, so a generic `message` listener never fires;
  // each kind is bound explicitly. EventSource reconnects on its own and sends
  // Last-Event-ID, which the server reads to resume — so one stream from 0 is
  // all that is needed.
  const KINDS = [
    'run_start', 'run_end', 'turn', 'llm_call', 'tool_intent', 'tool_result',
    'checkpoint', 'message', 'egress', 'approval', 'fork', 'workspace_modified'
  ];

  // A run emits several events in quick succession; refetching per event would
  // be a burst of redundant requests. Coalesce to one refresh per quiet moment.
  const refreshAgent = coalesce(() => {
    loadAgent(id, { background: true });
    loadActivations(id, { background: true });
  }, 400);

  function connectEvents(agentId) {
    es = api.events(agentId, 0);
    es.onopen = () => (connected = true);
    es.onerror = () => (connected = false);
    const onEvent = (ev) => {
      const parsed = JSON.parse(ev.data);
      if (seenSeqs.has(parsed.seq)) return;
      seenSeqs.add(parsed.seq);
      events = [...events, parsed];
      if (parsed.kind === 'turn' && selectedTurn == null) selectTurn(parsed.turn);
      // The event log says what happened; the agent resource says what the
      // agent now *is* (state, last turn). Keep both current, or the header
      // goes stale while the transcript scrolls on.
      if (['turn', 'run_start', 'run_end', 'fork', 'checkpoint'].includes(parsed.kind)) {
        refreshAgent();
      }
    };
    KINDS.forEach((k) => es.addEventListener(k, onEvent));
  }

  const inActivation = (e) => !activeActivation || e.activation_id === activeActivation;

  const timelineEntries = $derived(
    events.filter(
      (e) =>
        inActivation(e) &&
        ['turn', 'run_start', 'run_end', 'checkpoint'].includes(e.kind)
    )
  );

  const transcriptEvents = $derived(
    events.filter(
      (e) =>
        inActivation(e) &&
        ['message', 'llm_call', 'tool_intent', 'tool_result', 'workspace_modified'].includes(e.kind)
    )
  );

  const approvals = $derived(events.filter((e) => e.kind === 'approval'));

  async function selectTurn(turn, key = null) {
    selectedTurn = turn;
    selectedKey = key;
    closeFile();
    diff = null;
    showDiff = false;
    await loadWorkspace();
  }

  async function loadWorkspace() {
    if (selectedTurn == null) return;
    workspaceLoading = true;
    workspaceError = null;
    try {
      workspace = await api.workspaceAtTurn(id, selectedTurn);
    } catch (e) {
      if (e.status === 404) workspace = { files: [], _none: true };
      else workspaceError = e;
    } finally {
      workspaceLoading = false;
    }
  }

  // Compares the selected checkpoint's manifest to the previous turn's. A
  // content-addressed manifest makes this a path + chunk comparison rather than
  // a content diff.
  async function loadDiff() {
    if (!workspace || workspace._none || selectedTurn == null) return;
    try {
      const prev = await api.workspaceAtTurn(id, selectedTurn - 1).catch(() => null);
      if (!prev || prev._none) {
        diff = { none: true };
        return;
      }
      const cur = new Map((workspace.files ?? []).map((f) => [f.path, f]));
      const old = new Map((prev.files ?? []).map((f) => [f.path, f]));
      const added = [];
      const changed = [];
      const removed = [];
      for (const [path, f] of cur) {
        if (!old.has(path)) added.push(path);
        else if (old.get(path).size !== f.size || !sameChunks(old.get(path), f)) changed.push(path);
      }
      for (const path of old.keys()) if (!cur.has(path)) removed.push(path);
      diff = { added, changed, removed };
    } catch (e) {
      diff = { error: e.message };
    }
  }

  function sameChunks(a, b) {
    const ac = a.chunks ?? [];
    const bc = b.chunks ?? [];
    return ac.length === bc.length && ac.every((c, i) => c === bc[i]);
  }

  function toggleDiff() {
    showDiff = !showDiff;
    if (showDiff && !diff) loadDiff();
  }

  const FILE_LIMIT = 200_000;

  /**
   * Reads a file from the checkpoint and decides how to present it. The API
   * returns bytes; what those bytes *are* is a client-side question, so it is
   * answered here rather than guessed from the extension alone — an extension
   * says "png", a failed UTF-8 decode says "not text", and the second is the
   * one that matters for whether a text pane would be gibberish.
   */
  async function openFile(path) {
    // Revoke the previous object URL before replacing it, or every file opened
    // leaks a blob for the lifetime of the page.
    releaseFile();
    activeFile = path;
    fileLoading = true;
    file = null;
    try {
      const blob = await checkpointFileBlob(id, workspace.checkpoint_id, path);
      const size = blob.size;

      if (isImage(path)) {
        file = { kind: 'image', imageUrl: URL.createObjectURL(blob), size, truncated: false };
        return;
      }

      const text = await blob.text();
      // A NUL byte or a replacement character means the decode did not survive:
      // treat it as binary rather than rendering mojibake.
      if (/\u0000/.test(text) || text.includes('\uFFFD')) {
        file = { kind: 'binary', size, truncated: false };
        return;
      }

      const truncated = text.length > FILE_LIMIT;
      file = {
        kind: 'text',
        content: truncated ? text.slice(0, FILE_LIMIT) : text,
        size,
        truncated
      };
    } catch (e) {
      activeFile = null;
      file = null;
      toasts.error(`Could not read ${path}`, e, { retry: () => openFile(path) });
    } finally {
      fileLoading = false;
    }
  }

  function releaseFile() {
    if (file?.imageUrl) URL.revokeObjectURL(file.imageUrl);
  }

  function closeFile() {
    releaseFile();
    activeFile = null;
    file = null;
  }

  // --- intervention -------------------------------------------------------
  //
  // One shape for every action: run it, report the outcome explicitly, and on
  // a 403 record the refusal so the controls disappear (hidden, not disabled)
  // and the interface can say a request was refused rather than claiming to
  // know the credential is read-only.

  async function intervene(key, action, { label, onsuccess }) {
    busy[key] = true;
    try {
      const result = await action();
      onsuccess?.(result);
    } catch (e) {
      if (e.status === 403) {
        session.noteRefusal(label);
        toasts.error(`${label} was refused`, e);
      } else {
        toasts.error(`Could not ${label}`, e, {
          retry: () => intervene(key, action, { label, onsuccess })
        });
      }
    } finally {
      busy[key] = false;
      confirmOpen = false;
    }
  }

  function sendInput() {
    const body = inputBody.trim();
    if (!body) return;
    intervene('input', () => api.sendMessage(id, body), {
      label: 'send input',
      onsuccess: () => {
        inputBody = '';
        toasts.success('Input sent');
      }
    });
  }

  const confirmCancel = () =>
    ask({
      key: 'cancel',
      title: 'Cancel this agent?',
      description: `Agent ${id} will stop at its next safe point and move to the cancelled state. Its log and workspace are kept.`,
      confirmLabel: 'Cancel agent',
      cancelLabel: 'Keep running',
      run: () =>
        intervene('cancel', () => api.cancel(id), {
          label: 'cancel the agent',
          onsuccess: () => toasts.success(`Agent ${id} cancelled`)
        })
    });

  const confirmRestore = () =>
    ask({
      key: 'restore',
      title: `Restore the workspace to turn ${selectedTurn}?`,
      description: `This rewrites agent ${id}'s live workspace on disk to the checkpoint recorded at turn ${selectedTurn}. Work done after that turn is not represented in the restored files.`,
      confirmLabel: 'Restore workspace',
      run: () =>
        intervene('restore', () => api.restore(id, workspace.checkpoint_id), {
          label: 'restore the workspace',
          onsuccess: () => toasts.success(`Workspace restored to turn ${selectedTurn}`)
        })
    });

  const confirmDelete = () =>
    ask({
      key: 'delete',
      title: 'Delete this agent?',
      description: `Agent ${id} and its durable record are destroyed. This cannot be undone.`,
      confirmLabel: 'Delete agent',
      run: () =>
        intervene('delete', () => api.deleteAgent(id), {
          label: 'delete the agent',
          onsuccess: () => {
            toasts.success(`Agent ${id} deleted`);
            goto('/');
          }
        })
    });

  function forkHere() {
    intervene('fork', () => api.fork(id, selectedTurn), {
      label: 'fork the agent',
      onsuccess: (forked) =>
        toasts.success(`Forked at turn ${selectedTurn}`, {
          description: `New agent ${forked.id}`,
          href: `/agents/${forked.id}`,
          hrefLabel: 'Open fork'
        })
    });
  }

  function takeCheckpoint() {
    intervene('checkpoint', () => api.checkpoint(id), {
      label: 'take a checkpoint',
      onsuccess: () => toasts.success('Checkpoint taken')
    });
  }

  function resolveApproval(approvalId, decision) {
    resolving[approvalId] = true;
    intervene('approval', () => api.resolveApproval(id, approvalId, decision), {
      label: `${decision === 'approved' ? 'approve' : 'deny'} the request`,
      onsuccess: () => toasts.success(`Approval ${decision}`)
    }).finally(() => (resolving[approvalId] = false));
  }

  // Contextual palette actions. Only what is valid right now is registered, so
  // the palette can never offer an action whose visible control is hidden.
  registerPaletteActions(() => {
    if (!agent || !session.canIntervene) return [];
    const actions = [
      { id: 'cancel-agent', label: 'Cancel agent', group: 'Agent', icon: Ban, run: confirmCancel },
      { id: 'checkpoint-agent', label: 'Take checkpoint', group: 'Agent', icon: Diamond, run: takeCheckpoint },
      { id: 'delete-agent', label: 'Delete agent', group: 'Agent', icon: Trash2, run: confirmDelete }
    ];
    if (selectedTurn != null) {
      actions.unshift({
        id: 'fork-agent',
        label: `Fork at turn ${selectedTurn}`,
        group: 'Agent',
        icon: GitFork,
        run: forkHere
      });
      if (workspace && !workspace._none) {
        actions.push({
          id: 'restore-workspace',
          label: `Restore workspace to turn ${selectedTurn}`,
          group: 'Agent',
          icon: History,
          run: confirmRestore
        });
      }
    }
    return actions;
  });

  const canRestore = $derived(workspace && !workspace._none && selectedTurn != null);
</script>

<svelte:head><title>{id} — Stonewall</title></svelte:head>

{#if agentError}
  <div class="mx-auto max-w-2xl pt-8">
    <ErrorState error={agentError} retry={() => loadAgent(id)} title="Could not load this agent" />
    <div class="mt-3">
      <Button variant="outline" size="sm" href="/">
        <ArrowLeft data-icon="inline-start" />
        Back to fleet
      </Button>
    </div>
  </div>
{:else if !agent}
  <LoadingState label="Loading agent" />
{:else}
  <div class="mx-auto flex max-w-[110rem] flex-col gap-3">
    <!-- header -->
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h1 class="font-mono text-base font-semibold">{agent.id}</h1>
          <AgentStateBadge state={agent.state} />
          <span class="text-muted-foreground text-sm">turn {agent.last_turn}</span>
        </div>
        <p class="text-muted-foreground mt-0.5 text-xs">
          <span class="font-mono">{agent.image}</span>
          {#if agent.parent_id}
            · forked from
            <a class="underline underline-offset-2" href="/agents/{agent.parent_id}">
              {agent.parent_id}
            </a>
            at turn {agent.parent_turn}
          {/if}
        </p>
      </div>

      {#if session.canIntervene}
        <div class="flex flex-wrap items-center gap-1.5">
          <div class="flex items-center gap-1">
            <Input
              bind:value={inputBody}
              placeholder="Send input…"
              aria-label="Message body"
              class="h-7 w-56"
              onkeydown={(e) => e.key === 'Enter' && sendInput()}
            />
            <Button size="sm" onclick={sendInput} disabled={busy.input || !inputBody.trim()}>
              <Send data-icon="inline-start" />
              Send
            </Button>
          </div>
          <Button
            variant="outline"
            size="sm"
            onclick={forkHere}
            disabled={busy.fork || selectedTurn == null}
            title={selectedTurn == null ? 'Select a turn to fork at' : `Fork at turn ${selectedTurn}`}
          >
            <GitFork data-icon="inline-start" />
            Fork
          </Button>
          <Button variant="outline" size="sm" onclick={takeCheckpoint} disabled={busy.checkpoint}>
            <Diamond data-icon="inline-start" />
            Checkpoint
          </Button>
          <Button variant="outline" size="sm" onclick={confirmRestore} disabled={!canRestore}>
            <History data-icon="inline-start" />
            Restore
          </Button>
          <Button variant="outline" size="sm" onclick={confirmCancel} disabled={busy.cancel}>
            <Ban data-icon="inline-start" />
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onclick={confirmDelete} disabled={busy.delete}>
            <Trash2 data-icon="inline-start" />
            Delete
          </Button>
        </div>
      {:else}
        <p class="text-muted-foreground max-w-sm text-xs">
          Intervention controls are hidden because a request
          {session.refusal ? `(${session.refusal.action})` : ''} was refused by the server. That is
          what the dashboard observed — it does not know what your credential is allowed to do.
        </p>
      {/if}
    </div>

    <AgentConfig {agent} />

    <!-- three panes, keyed by turn -->
    <div class="hidden h-[calc(100vh-19rem)] min-h-[28rem] xl:block">
      <Resizable.PaneGroup direction="horizontal" autoSaveId="stonewall.workbench.panes">
        <Resizable.Pane defaultSize={20} minSize={12} class="rounded-lg border p-2">
          <Timeline
            entries={timelineEntries}
            {activations}
            {selectedTurn}
            {selectedKey}
            {connected}
            onselect={selectTurn}
          />
        </Resizable.Pane>
        <Resizable.Handle withHandle class="mx-1.5" />
        <Resizable.Pane defaultSize={50} minSize={25} class="rounded-lg border p-2">
          <Transcript events={transcriptEvents} {selectedTurn} />
        </Resizable.Pane>
        <Resizable.Handle withHandle class="mx-1.5" />
        <Resizable.Pane defaultSize={30} minSize={15} class="rounded-lg border p-2">
          <WorkspacePane
            {selectedTurn}
            {workspace}
            loading={workspaceLoading}
            error={workspaceError}
            {diff}
            {showDiff}
            {activeFile}
            {file}
            {fileLoading}
            onretry={loadWorkspace}
            ontogglediff={toggleDiff}
            onopenfile={openFile}
            oncloseFile={closeFile}
          />
        </Resizable.Pane>
      </Resizable.PaneGroup>
    </div>

    <!-- below the resizable breakpoint the panes stack; nothing is dropped -->
    <div class="flex flex-col gap-3 xl:hidden">
      <div class="max-h-72 rounded-lg border p-2">
        <Timeline
          entries={timelineEntries}
          {activations}
          {selectedTurn}
          {selectedKey}
          {connected}
          onselect={selectTurn}
        />
      </div>
      <div class="max-h-[32rem] rounded-lg border p-2">
        <Transcript events={transcriptEvents} {selectedTurn} />
      </div>
      <div class="max-h-[32rem] rounded-lg border p-2">
        <WorkspacePane
          {selectedTurn}
          {workspace}
          loading={workspaceLoading}
          error={workspaceError}
          {diff}
          {showDiff}
          {activeFile}
          {file}
          {fileLoading}
          onretry={loadWorkspace}
          ontogglediff={toggleDiff}
          onopenfile={openFile}
          oncloseFile={closeFile}
        />
      </div>
    </div>

    <div class="grid gap-3 lg:grid-cols-2">
      <Activations
        {activations}
        active={activeActivation}
        loading={activationsLoading}
        error={activationsError}
        onretry={loadActivations}
        onselect={(a) => (activeActivation = a)}
      />
      <Approvals
        {approvals}
        canIntervene={session.canIntervene}
        {resolving}
        onresolve={resolveApproval}
      />
    </div>

    <p class="text-muted-foreground text-xs">
      Granted quotas are shown above. Live per-agent CPU and memory are not displayed because the
      runtime does not sample them — that is a later runtime/telemetry change, not a gap this view
      fills in with an estimate.
    </p>
  </div>

  {#if confirm}
    <ConfirmDialog
      bind:open={confirmOpen}
      title={confirm.title}
      description={confirm.description}
      confirmLabel={confirm.confirmLabel}
      cancelLabel={confirm.cancelLabel ?? 'Cancel'}
      busy={busy[confirm.key]}
      onconfirm={confirm.run}
    />
  {/if}
{/if}
