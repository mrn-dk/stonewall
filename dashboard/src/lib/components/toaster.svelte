<script>
  // Renders the outcome queue. Live region so a screen reader hears an outcome
  // it did not cause; failures stay put until dismissed so the request id can
  // actually be read and quoted.
  import { toasts } from '$lib/toasts.svelte.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Info from '@lucide/svelte/icons/info';
  import X from '@lucide/svelte/icons/x';

  const icons = { success: CircleCheck, error: TriangleAlert, info: Info };
  const tones = {
    success: 'border-state-running/40',
    error: 'border-destructive/40',
    info: 'border-border'
  };
</script>

<div
  class="pointer-events-none fixed right-3 bottom-3 z-50 flex w-[min(24rem,calc(100vw-1.5rem))] flex-col gap-2"
  role="region"
  aria-label="Notifications"
>
  {#each toasts.items as t (t.id)}
    {@const Icon = icons[t.variant]}
    <div
      class="bg-popover text-popover-foreground pointer-events-auto flex items-start gap-2 rounded-lg border p-2.5 shadow-lg {tones[
        t.variant
      ]}"
      role={t.variant === 'error' ? 'alert' : 'status'}
    >
      <Icon
        class="mt-0.5 size-4 shrink-0 {t.variant === 'success'
          ? 'text-state-running'
          : t.variant === 'error'
            ? 'text-destructive'
            : 'text-muted-foreground'}"
      />
      <div class="min-w-0 flex-1">
        <p class="text-sm font-medium">{t.title}</p>
        {#if t.description}
          <p class="text-muted-foreground mt-0.5 text-sm break-words">{t.description}</p>
        {/if}
        {#if t.requestId}
          <p class="text-muted-foreground mt-1 font-mono text-xs">request {t.requestId}</p>
        {/if}
        {#if t.retry || t.href}
          <div class="mt-1.5 flex gap-1.5">
            {#if t.retry}
              <Button
                variant="outline"
                size="xs"
                onclick={() => {
                  toasts.dismiss(t.id);
                  t.retry();
                }}>Retry</Button
              >
            {/if}
            {#if t.href}
              <Button variant="outline" size="xs" href={t.href} onclick={() => toasts.dismiss(t.id)}>
                {t.hrefLabel ?? 'Open'}
              </Button>
            {/if}
          </div>
        {/if}
      </div>
      <Button
        variant="ghost"
        size="icon-xs"
        onclick={() => toasts.dismiss(t.id)}
        aria-label="Dismiss notification"
      >
        <X />
      </Button>
    </div>
  {/each}
</div>
