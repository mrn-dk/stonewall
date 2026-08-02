<script>
  // The operational spine, drawn as a spine.
  //
  // The raw log is noisy: a turn and the checkpoint it produced arrive as two
  // separate events, and a run contributes a start and an end around them. Two
  // things fix that, and both are grouping rather than styling:
  //
  //   - a checkpoint is folded into the turn that produced it, because that is
  //     what it is — the marker recorded at that turn — rather than a sibling
  //     row that pushes turns apart;
  //   - run boundaries become labelled dividers, so the rail reads as runs
  //     containing turns instead of a flat stream of event kinds.
  //
  // What is left is one node per turn on a continuous rail: the thing the
  // operator actually selects.
  import { timeOfDay } from '$lib/agents.js';
  import Diamond from '@lucide/svelte/icons/diamond';
  import Play from '@lucide/svelte/icons/play';
  import Square from '@lucide/svelte/icons/square';
  import WifiOff from '@lucide/svelte/icons/wifi-off';

  let { entries, selectedTurn, connected, onselect } = $props();

  // Fold the flat event list into rows: one per turn (carrying its checkpoint,
  // if any) and one per run boundary.
  const rows = $derived.by(() => {
    const out = [];
    const turnIndex = new Map();

    for (const e of entries) {
      if (e.kind === 'turn') {
        const row = {
          type: 'turn',
          key: `turn-${e.seq}`,
          turn: e.turn,
          at: e.occurred_at,
          checkpoint: null
        };
        turnIndex.set(e.turn, row);
        out.push(row);
      } else if (e.kind === 'checkpoint') {
        const row = turnIndex.get(e.turn);
        if (row) {
          // Folded onto its turn.
          row.checkpoint = e;
        } else {
          // A checkpoint with no turn event in view (an explicit checkpoint,
          // or a filtered activation) still deserves its own node.
          out.push({
            type: 'turn',
            key: `ckpt-${e.seq}`,
            turn: e.turn,
            at: e.occurred_at,
            checkpoint: e,
            checkpointOnly: true
          });
        }
      } else {
        out.push({
          type: 'boundary',
          key: `${e.kind}-${e.seq}`,
          kind: e.kind,
          at: e.occurred_at
        });
      }
    }
    return out;
  });

  const turnCount = $derived(rows.filter((r) => r.type === 'turn').length);
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex items-center justify-between gap-2 px-1 pb-1.5">
    <h2 class="text-muted-foreground text-xs font-medium tracking-wide uppercase">
      Timeline
      {#if turnCount}
        <span class="normal-case">· {turnCount} turn{turnCount === 1 ? '' : 's'}</span>
      {/if}
    </h2>
    {#if !connected}
      <span class="text-state-failed inline-flex items-center gap-1 text-xs" role="status">
        <WifiOff class="size-3" />
        reconnecting
      </span>
    {/if}
  </div>

  <div class="scrollbar-thin min-h-0 flex-1 overflow-y-auto pb-1">
    {#if rows.length === 0}
      <p class="text-muted-foreground px-1.5 py-3 text-sm">
        No turns yet. The timeline fills in as the agent runs.
      </p>
    {:else}
      <!-- The rail: a single continuous line the nodes sit on. -->
      <ol class="relative ml-3 list-none border-l pl-0">
        {#each rows as row (row.key)}
          {#if row.type === 'boundary'}
            {@const started = row.kind === 'run_start'}
            {@const Icon = started ? Play : Square}
            <li class="relative py-1.5 pl-4">
              <span
                class="bg-background text-muted-foreground absolute top-1/2 -left-[7px] flex size-3.5 -translate-y-1/2 items-center justify-center rounded-full border"
              >
                <Icon class="size-2" />
              </span>
              <span
                class="text-muted-foreground flex items-baseline gap-1.5 text-[0.7rem] tracking-wide uppercase"
              >
                {started ? 'run started' : 'run ended'}
                <span class="ml-auto tabular-nums normal-case">{timeOfDay(row.at)}</span>
              </span>
            </li>
          {:else}
            {@const selected = selectedTurn === row.turn}
            <li class="relative">
              <button
                type="button"
                class="group flex w-full items-center gap-2 rounded-md py-1 pr-1.5 pl-4 text-left text-xs transition-colors {selected
                  ? 'bg-secondary text-secondary-foreground'
                  : 'hover:bg-muted'}"
                aria-pressed={selected}
                aria-label="Turn {row.turn}{row.checkpoint ? ', has checkpoint' : ''}"
                onclick={() => onselect(row.turn)}
              >
                <!-- The node on the rail. Filled when selected, so the current
                     position reads at a glance down the whole column. -->
                <span
                  class="absolute -left-[5px] size-2.5 rounded-full border transition-colors {selected
                    ? 'border-foreground bg-foreground'
                    : 'border-border bg-background group-hover:border-foreground/50'}"
                  aria-hidden="true"
                ></span>

                <span class="font-medium tabular-nums">
                  {row.checkpointOnly ? 'checkpoint' : 'turn'}
                  {row.turn}
                </span>

                {#if row.checkpoint && !row.checkpointOnly}
                  <!-- Labelled rather than a bare glyph: a blue diamond means
                       nothing to someone opening the workbench for the first
                       time, and the legend at the foot only helps if you look
                       down there. -->
                  <span class="text-muted-foreground flex shrink-0 items-center gap-1">
                    <Diamond class="text-state-terminal size-2.5" />
                    <span class="text-[0.7rem]">checkpoint</span>
                  </span>
                {/if}

                <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
                  {timeOfDay(row.at)}
                </span>
              </button>
            </li>
          {/if}
        {/each}
      </ol>
    {/if}
  </div>
</div>
