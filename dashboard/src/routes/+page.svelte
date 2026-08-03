<script>
  // Fleet overview — the door you walk through to reach a workbench.
  //
  // Deliberately light: counts, node resources, a filterable cursor-paged
  // table. Depth belongs in the workbench. The filter and query live in the
  // URL and are applied by the API, so this page never loads the whole fleet
  // in order to search it.
  import { api } from '$lib/api.js';
  import { page } from '$app/state';
  import { startPolling } from '$lib/live.svelte.js';
  import { toasts } from '$lib/toasts.svelte.js';
  import NodeStatsStrip from '$lib/components/node-stats-strip.svelte';
  import AgentTable from '$lib/components/agent-table.svelte';
  import FleetFilters from '$lib/components/fleet-filters.svelte';
  import CreateAgentDialog from '$lib/components/create-agent-dialog.svelte';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import EmptyState from '$lib/components/states/empty-state.svelte';
  import ErrorState from '$lib/components/states/error-state.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import Bot from '@lucide/svelte/icons/bot';
  import SearchX from '@lucide/svelte/icons/search-x';

  const PAGE_SIZE = 100;

  let agents = $state([]);
  let next = $state('');
  let loading = $state(true);
  let loadingMore = $state(false);
  let err = $state(null);

  let stats = $state(null);
  let statsError = $state(null);

  let createOpen = $state(false);

  // The URL is the single source of truth for what is being listed.
  const activeState = $derived(page.url.searchParams.get('state') ?? '');
  const query = $derived(page.url.searchParams.get('q') ?? '');
  const filtered = $derived(Boolean(activeState || query));

  // Counts describe the rows actually loaded, and the label says so ("loaded")
  // rather than implying a fleet-wide total the API never returned.
  const counts = $derived(
    agents.reduce(
      (c, a) => {
        c[a.state] = (c[a.state] || 0) + 1;
        c.total += 1;
        return c;
      },
      { total: 0 }
    )
  );

  // A stamp per request: a slow earlier response must never overwrite a newer
  // one when the operator keeps typing.
  let seq = 0;
  // Rows the operator has pulled in with "load more". A background refresh has
  // to fetch at least this many or it would silently throw those pages away.
  let loadedCount = $state(PAGE_SIZE);

  $effect(() => {
    void activeState;
    void query;
    loadedCount = PAGE_SIZE;
    load();
  });

  $effect(() => {
    loadStats();
    // Refresh often enough that the fleet feels live, rarely enough that an
    // idle dashboard is not a load source. Polling because the API has no
    // fleet-wide event stream — per-agent SSE only.
    return startPolling(refresh, 4000);
  });

  /** Foreground load: may show a loading state and may surface an error. */
  async function load() {
    const mine = ++seq;
    loading = true;
    err = null;
    try {
      const res = await api.listAgents({ state: activeState, q: query, limit: PAGE_SIZE });
      if (mine !== seq) return;
      agents = res.agents || [];
      next = res.next_cursor || '';
    } catch (e) {
      if (mine !== seq) return;
      err = e;
    } finally {
      if (mine === seq) loading = false;
    }
  }

  /**
   * Background refresh. Deliberately unlike `load`: it never sets `loading`,
   * so a populated table does not flash; it re-requests everything the
   * operator has paged in; and on failure it leaves the last good data alone.
   * A blip in a poll is not a reason to replace a working view with an error.
   */
  async function refresh() {
    const mine = ++seq;
    try {
      const [list, node] = await Promise.all([
        api.listAgents({ state: activeState, q: query, limit: loadedCount }),
        api.nodeStats().catch(() => null)
      ]);
      if (mine !== seq) return;
      agents = list.agents || [];
      next = list.next_cursor || '';
      err = null;
      if (node) {
        stats = node;
        statsError = null;
      }
    } catch {
      // Intentionally silent: keep showing what we have.
    }
  }

  async function loadStats() {
    try {
      stats = await api.nodeStats();
      statsError = null;
    } catch (e) {
      stats = null;
      statsError = e;
    }
  }

  async function more() {
    if (!next || loadingMore) return;
    loadingMore = true;
    try {
      const res = await api.listAgents({
        state: activeState,
        q: query,
        after: next,
        limit: PAGE_SIZE
      });
      agents = [...agents, ...(res.agents || [])];
      next = res.next_cursor || '';
      loadedCount = agents.length;
    } catch (e) {
      toasts.error('Could not load more agents', e, { retry: more });
    } finally {
      loadingMore = false;
    }
  }
</script>

<svelte:head><title>Fleet — Stonewall</title></svelte:head>

<section class="mx-auto flex max-w-[110rem] flex-col gap-4">
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div>
      <h1 class="text-lg font-semibold tracking-tight">Fleet</h1>
      <p class="text-muted-foreground text-sm">Open an agent to reach its workbench.</p>
    </div>
    <CreateAgentDialog bind:open={createOpen} />
  </div>

  <div class="rounded-lg border p-3">
    <NodeStatsStrip {counts} {stats} {statsError} />
  </div>

  <FleetFilters state={activeState} q={query} />

  {#if err}
    <ErrorState error={err} retry={load} title="Could not load agents" />
  {:else if loading}
    <LoadingState label="Loading agents" rows={6} />
  {:else if agents.length === 0 && filtered}
    <EmptyState
      icon={SearchX}
      title="No agents match this filter"
      description="No agent matches the current state filter and search query."
    >
      <Button variant="outline" size="sm" href="/">Clear filters</Button>
    </EmptyState>
  {:else if agents.length === 0}
    <EmptyState
      icon={Bot}
      title="No agents yet"
      description="Nothing is running on this node. Create an agent to get started."
    >
      <Button size="sm" onclick={() => (createOpen = true)}>New agent</Button>
    </EmptyState>
  {:else}
    <AgentTable {agents} />

    <div class="flex items-center justify-between gap-3">
      <p class="text-muted-foreground text-xs">
        {agents.length} agent{agents.length === 1 ? '' : 's'} loaded{next ? ', more available' : ''}
      </p>
      {#if next}
        <Button variant="outline" size="sm" onclick={more} disabled={loadingMore}>
          {loadingMore ? 'Loading…' : 'Load more'}
        </Button>
      {/if}
    </div>
  {/if}
</section>
