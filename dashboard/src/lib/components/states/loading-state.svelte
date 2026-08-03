<script>
  // A pending request. Distinct from "empty" on purpose: an empty result and a
  // request still in flight are different facts, and the operator is entitled
  // to know which one they are looking at.
  import { Spinner } from '$lib/components/ui/spinner/index.js';

  let { label = 'Loading', rows = 0, class: className = '' } = $props();
</script>

{#if rows > 0}
  <div class="space-y-1.5 {className}" role="status" aria-label={label} aria-busy="true">
    {#each Array(rows) as _, i (i)}
      <div class="bg-muted h-6 animate-pulse rounded-md" style="opacity: {1 - i * 0.12}"></div>
    {/each}
  </div>
{:else}
  <div
    class="text-muted-foreground flex items-center gap-2 px-1 py-3 text-sm {className}"
    role="status"
    aria-busy="true"
  >
    <Spinner class="size-3.5" />
    <span>{label}…</span>
  </div>
{/if}
