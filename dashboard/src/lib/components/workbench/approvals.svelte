<script>
  // Pending approvals. Resolution controls are rendered only when the session
  // has not been refused — hidden, not disabled, per the intervention rules.
  import { Button } from '$lib/components/ui/button/index.js';
  import Check from '@lucide/svelte/icons/check';
  import Ban from '@lucide/svelte/icons/ban';

  let { approvals, canIntervene, resolving, onresolve } = $props();
</script>

<section class="rounded-lg border p-2.5">
  <h2 class="text-muted-foreground pb-1.5 text-xs font-medium tracking-wide uppercase">
    Approvals
  </h2>

  <ul class="space-y-1">
    {#each approvals as a (a.seq)}
      {@const id = a.payload?.approval_id ?? ''}
      {@const decision = a.payload?.decision}
      <li class="flex flex-wrap items-center gap-2 text-xs">
        <span class="font-mono">{id || `seq ${a.seq}`}</span>
        {#if decision}
          <span
            class={decision === 'approved' ? 'text-state-running' : 'text-state-failed'}
          >
            {decision}
          </span>
        {:else}
          <span class="text-state-pending">pending</span>
          {#if canIntervene}
            <span class="flex gap-1">
              <Button
                variant="outline"
                size="xs"
                disabled={resolving[id]}
                onclick={() => onresolve(id, 'approved')}
              >
                <Check data-icon="inline-start" />
                Approve
              </Button>
              <Button
                variant="outline"
                size="xs"
                disabled={resolving[id]}
                onclick={() => onresolve(id, 'denied')}
              >
                <Ban data-icon="inline-start" />
                Deny
              </Button>
            </span>
          {/if}
        {/if}
      </li>
    {:else}
      <li class="text-muted-foreground text-sm">Nothing is waiting on an approval.</li>
    {/each}
  </ul>
</section>
