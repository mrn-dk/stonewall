<script>
  // The operational spine: run boundaries, turn boundaries, checkpoints.
  //
  // Selecting a turn here is what syncs the other two panes — turn is the join
  // key across the workbench, so this list is a set of radio-like controls over
  // one selection, not a log to read.
  import { timeOfDay } from '$lib/agents.js';
  import Play from '@lucide/svelte/icons/play';
  import Square from '@lucide/svelte/icons/square';
  import CornerDownRight from '@lucide/svelte/icons/corner-down-right';
  import Diamond from '@lucide/svelte/icons/diamond';
  import WifiOff from '@lucide/svelte/icons/wifi-off';

  let { entries, selectedTurn, connected, onselect } = $props();

  const icons = {
    run_start: Play,
    run_end: Square,
    turn: CornerDownRight,
    checkpoint: Diamond
  };

  function label(e) {
    if (e.kind === 'checkpoint') return `checkpoint @ turn ${e.turn}`;
    if (e.kind === 'turn') return `turn ${e.turn}`;
    return e.kind.replace('_', ' ');
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex items-center justify-between gap-2 px-1 pb-1.5">
    <h2 class="text-muted-foreground text-xs font-medium tracking-wide uppercase">Timeline</h2>
    {#if !connected}
      <span class="text-state-failed inline-flex items-center gap-1 text-xs" role="status">
        <WifiOff class="size-3" />
        reconnecting
      </span>
    {/if}
  </div>

  <div class="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-0.5 pb-1">
    {#each entries as e (e.seq)}
      {@const Icon = icons[e.kind] ?? CornerDownRight}
      {@const selected = e.turn != null && selectedTurn === e.turn}
      <button
        type="button"
        class="flex w-full items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-xs transition-colors {selected
          ? 'bg-secondary text-secondary-foreground font-medium'
          : 'hover:bg-muted text-foreground'}"
        aria-pressed={selected}
        disabled={e.turn == null}
        onclick={() => e.turn != null && onselect(e.turn)}
      >
        <Icon
          class="size-3 shrink-0 {e.kind === 'checkpoint'
            ? 'text-state-terminal'
            : 'text-muted-foreground'}"
        />
        <span class="truncate">{label(e)}</span>
        <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
          {timeOfDay(e.occurred_at)}
        </span>
      </button>
    {:else}
      <p class="text-muted-foreground px-1.5 py-3 text-sm">
        No turns yet. Events appear here as the agent runs.
      </p>
    {/each}
  </div>
</div>
