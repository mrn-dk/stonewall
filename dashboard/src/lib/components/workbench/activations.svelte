<script>
  // Activation history. Selecting one narrows the timeline and transcript to
  // that run, which is how you answer "what happened the third time this agent
  // woke up" without reading the whole log.
  import { timeOfDay } from '$lib/agents.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import ErrorState from '$lib/components/states/error-state.svelte';

  let { activations, active, loading, error, onretry, onselect } = $props();
</script>

<section class="rounded-lg border p-2.5">
  <div class="flex items-center justify-between gap-2 pb-1.5">
    <h2 class="text-muted-foreground text-xs font-medium tracking-wide uppercase">Activations</h2>
    {#if active}
      <Button variant="ghost" size="xs" onclick={() => onselect(null)}>Clear filter</Button>
    {/if}
  </div>

  {#if error}
    <ErrorState {error} retry={onretry} title="Could not load activations" />
  {:else if loading}
    <LoadingState label="Loading activations" />
  {:else}
    <div class="flex flex-wrap gap-1">
      {#each activations as a (a.id)}
        <button
          type="button"
          class="rounded-md border px-2 py-1 text-left text-xs transition-colors {active === a.id
            ? 'bg-secondary text-secondary-foreground'
            : 'hover:bg-muted'}"
          aria-pressed={active === a.id}
          onclick={() => onselect(active === a.id ? null : a.id)}
        >
          <span class="font-mono">#{a.number}</span>
          <span class="text-muted-foreground">
            {timeOfDay(a.started_at)} → {a.ended_at ? timeOfDay(a.ended_at) : 'running'}
          </span>
          {#if a.end_reason}
            <span class="text-muted-foreground">· {a.end_reason}</span>
          {/if}
        </button>
      {:else}
        <p class="text-muted-foreground py-1 text-sm">
          No activations yet — this agent has not been woken.
        </p>
      {/each}
    </div>
  {/if}
</section>
