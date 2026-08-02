<script>
  // A failed request. Carries the request identifier the API returned — the
  // one thing that makes a report actionable — and a retry that re-issues only
  // this request, so one failed panel does not force a whole-page reload.
  import { Button } from '$lib/components/ui/button/index.js';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';

  let { error, retry = null, title = 'Request failed', class: className = '' } = $props();
</script>

<div
  class="border-destructive/30 bg-destructive/5 flex flex-col gap-2 rounded-lg border p-3 {className}"
  role="alert"
>
  <div class="flex items-start gap-2">
    <TriangleAlert class="text-destructive mt-0.5 size-4 shrink-0" />
    <div class="min-w-0 flex-1">
      <p class="text-foreground text-sm font-medium">{title}</p>
      {#if error?.message}
        <p class="text-muted-foreground mt-0.5 text-sm break-words">{error.message}</p>
      {/if}
      {#if error?.request_id}
        <p class="text-muted-foreground mt-1 font-mono text-xs">
          request {error.request_id}
        </p>
      {/if}
    </div>
  </div>
  {#if retry}
    <div>
      <Button variant="outline" size="xs" onclick={retry}>
        <RotateCw data-icon="inline-start" />
        Retry
      </Button>
    </div>
  {/if}
</div>
