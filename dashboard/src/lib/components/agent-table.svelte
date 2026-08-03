<script>
  // The fleet table. Opening the workbench is the only per-row navigation, by
  // design — depth lives in the workbench, not here.
  //
  // Rows are links rather than click handlers on <tr>, so they are focusable,
  // keyboard-activatable, and openable in a new tab without any extra code.
  import * as Table from '$lib/components/ui/table/index.js';
  import AgentStateBadge from '$lib/components/agent-state-badge.svelte';
  import { grantsSummary, relTime } from '$lib/agents.js';

  let { agents } = $props();

  const columns = ['id', 'state', 'goal', 'image', 'grants', 'isolation', 'model', 'act.', 'last'];
</script>

<div class="overflow-x-auto rounded-lg border">
  <Table.Root>
    <Table.Header>
      <Table.Row class="hover:bg-transparent">
        {#each columns as col (col)}
          <Table.Head
            class="text-muted-foreground h-8 text-xs font-medium tracking-wide uppercase whitespace-nowrap"
          >
            {col}
          </Table.Head>
        {/each}
      </Table.Row>
    </Table.Header>
    <Table.Body>
      {#each agents as a (a.id)}
        <Table.Row class="group">
          <Table.Cell class="py-1.5 font-mono text-xs whitespace-nowrap">
            <a
              href="/agents/{a.id}"
              class="hover:underline focus-visible:underline"
              aria-label="Open workbench for agent {a.id}"
            >
              {a.id}
            </a>
          </Table.Cell>
          <Table.Cell class="py-1.5"><AgentStateBadge state={a.state} /></Table.Cell>
          <Table.Cell class="text-foreground max-w-[20rem] truncate py-1.5" title={a.goal}>
            {a.goal || '–'}
          </Table.Cell>
          <Table.Cell class="text-muted-foreground max-w-[14rem] truncate py-1.5 font-mono text-xs">
            {a.image}
          </Table.Cell>
          <Table.Cell
            class="text-muted-foreground max-w-[16rem] truncate py-1.5 font-mono text-xs"
            title={grantsSummary(a)}
          >
            {grantsSummary(a) || '–'}
          </Table.Cell>
          <Table.Cell class="text-muted-foreground py-1.5 whitespace-nowrap">
            {a.isolation}
          </Table.Cell>
          <Table.Cell class="text-muted-foreground py-1.5 whitespace-nowrap">
            {a.model || '–'}
          </Table.Cell>
          <Table.Cell class="text-muted-foreground py-1.5 tabular-nums">
            {a.activation_count}
          </Table.Cell>
          <Table.Cell class="text-muted-foreground py-1.5 tabular-nums whitespace-nowrap">
            {relTime(a.updated_at)}
          </Table.Cell>
        </Table.Row>
      {/each}
    </Table.Body>
  </Table.Root>
</div>
