<script>
  // Navigation and contextual actions from the keyboard.
  //
  // Agent lookup goes through the same server-side query the fleet view uses,
  // so the palette finds agents that were never on screen — a palette that only
  // searched loaded rows would be a search box that lies.
  import * as Command from '$lib/components/ui/command/index.js';
  import { palette } from '$lib/palette.svelte.js';
  import { api } from '$lib/api.js';
  import { goto } from '$app/navigation';
  import Server from '@lucide/svelte/icons/server';
  import Bot from '@lucide/svelte/icons/bot';

  let query = $state('');
  let matches = $state([]);
  let searching = $state(false);
  let searchFailed = $state(false);

  // Debounced so typing does not issue a request per keystroke, and stamped so
  // a slow earlier response cannot overwrite a newer one.
  let seq = 0;
  let timer;

  $effect(() => {
    const q = query.trim();
    clearTimeout(timer);
    if (!palette.open) return;
    if (!q) {
      matches = [];
      searching = false;
      searchFailed = false;
      return;
    }
    searching = true;
    const mine = ++seq;
    timer = setTimeout(async () => {
      try {
        const res = await api.listAgents({ q, limit: 8 });
        if (mine !== seq) return;
        matches = res.agents || [];
        searchFailed = false;
      } catch {
        if (mine !== seq) return;
        matches = [];
        searchFailed = true;
      } finally {
        if (mine === seq) searching = false;
      }
    }, 200);
    return () => clearTimeout(timer);
  });

  function onKeydown(e) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      palette.toggle();
    }
  }

  function run(fn) {
    palette.open = false;
    query = '';
    fn();
  }

  const groups = $derived([...new Set(palette.actions.map((a) => a.group ?? 'Actions'))]);
</script>

<svelte:window onkeydown={onKeydown} />

<Command.Dialog
  bind:open={palette.open}
  title="Command palette"
  description="Search agents or run an action"
  shouldFilter={false}
>
  <Command.Input placeholder="Search agents, or run an action…" bind:value={query} />
  <Command.List>
    {#if searching}
      <Command.Loading>Searching…</Command.Loading>
    {/if}

    {#if query.trim() && !searching && matches.length === 0}
      <Command.Empty>
        {searchFailed ? 'Agent search failed.' : `No agent matches “${query.trim()}”.`}
      </Command.Empty>
    {/if}

    {#if matches.length > 0}
      <Command.Group heading="Agents">
        {#each matches as a (a.id)}
          <Command.Item
            value={a.id}
            onSelect={() => run(() => goto(`/agents/${a.id}`))}
            keywords={[a.goal ?? '', a.image ?? '']}
          >
            <Bot class="size-3.5" />
            <span class="font-mono">{a.id}</span>
            <span class="text-muted-foreground truncate">{a.goal || a.image}</span>
          </Command.Item>
        {/each}
      </Command.Group>
    {/if}

    {#each groups as group (group)}
      {@const actions = palette.actions.filter((a) => (a.group ?? 'Actions') === group)}
      {#if actions.length}
        <Command.Group heading={group}>
          {#each actions as action (action.id)}
            {@const Icon = action.icon}
            <Command.Item value={action.id} onSelect={() => run(action.run)}>
              {#if Icon}<Icon class="size-3.5" />{/if}
              {action.label}
            </Command.Item>
          {/each}
        </Command.Group>
      {/if}
    {/each}

    <Command.Group heading="Go to">
      <Command.Item value="fleet" onSelect={() => run(() => goto('/'))}>
        <Server class="size-3.5" />
        Fleet overview
      </Command.Item>
    </Command.Group>
  </Command.List>
</Command.Dialog>
