<script>
  // Configuration and granted authority.
  //
  // The command allow-list carries its stated limitation wherever it is shown:
  // it controls which binaries run, not what they do once running. Granting an
  // interpreter is granting the image. The sandbox is the boundary, not this
  // list — and the UI says so rather than implying otherwise by presenting a
  // tidy list of permissions.
  import { broadCommandGrants } from '$lib/agents.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';

  let { agent } = $props();

  const fs = $derived(Object.entries(agent.grants?.fs || {}));
  const net = $derived(agent.grants?.net || []);
  const cmd = $derived(agent.grants?.cmd || []);
  const broad = $derived(broadCommandGrants(agent));
</script>

<div class="grid gap-3 rounded-lg border p-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
  <dl class="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1.5 text-sm">
    <dt class="text-muted-foreground text-xs tracking-wide uppercase">Goal</dt>
    <dd class="min-w-0 break-words">{agent.goal || '–'}</dd>

    <dt class="text-muted-foreground text-xs tracking-wide uppercase">Model</dt>
    <dd class="font-mono text-xs">{agent.model || '–'}</dd>

    <dt class="text-muted-foreground text-xs tracking-wide uppercase">Isolation</dt>
    <dd class="font-mono text-xs">{agent.isolation}</dd>

    <dt class="text-muted-foreground text-xs tracking-wide uppercase">Checkpoint</dt>
    <dd class="font-mono text-xs">{agent.checkpoint}</dd>
  </dl>

  <div class="flex flex-col gap-2">
    <div class="grid grid-cols-[2.5rem_minmax(0,1fr)] items-start gap-x-2 gap-y-1.5 text-xs">
      <span class="text-muted-foreground pt-0.5 tracking-wide uppercase">fs</span>
      <div class="flex flex-wrap gap-1">
        {#each fs as [path, mode] (path)}
          <span class="bg-muted rounded border px-1.5 py-px font-mono">
            {path}<span class="text-muted-foreground">:{mode}</span>
          </span>
        {:else}
          <span class="text-muted-foreground">none</span>
        {/each}
      </div>

      <span class="text-muted-foreground pt-0.5 tracking-wide uppercase">net</span>
      <div class="flex flex-wrap gap-1">
        {#each net as endpoint (endpoint)}
          <span class="bg-muted rounded border px-1.5 py-px font-mono">{endpoint}</span>
        {:else}
          <span class="text-muted-foreground">none</span>
        {/each}
      </div>

      <span class="text-muted-foreground pt-0.5 tracking-wide uppercase">cmd</span>
      <div class="flex flex-wrap gap-1">
        {#each cmd as c (c)}
          {#if broad.includes(c)}
            <Tooltip.Provider>
              <Tooltip.Root>
                <Tooltip.Trigger>
                  <span
                    class="border-state-pending/40 bg-state-pending/10 text-state-pending inline-flex items-center gap-1 rounded border px-1.5 py-px font-mono"
                  >
                    <TriangleAlert class="size-3" />
                    {c}
                  </span>
                </Tooltip.Trigger>
                <Tooltip.Content class="max-w-xs">
                  {c} can run anything else in the image — allowing it is effectively allowing everything.
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          {:else}
            <span class="bg-muted rounded border px-1.5 py-px font-mono">{c}</span>
          {/if}
        {:else}
          <span class="text-muted-foreground">none</span>
        {/each}
      </div>
    </div>

    {#if broad.length}
      <p class="text-muted-foreground flex items-start gap-1.5 text-xs">
        <TriangleAlert class="text-state-pending mt-0.5 size-3.5 shrink-0" />
        <span>
          Broad command grant. The allow-list controls which binaries run, not what they do once
          running — {broad.join(', ')}
          {broad.length === 1 ? 'is' : 'are'} effectively everything in the image. The security boundary
          is the sandbox.
        </span>
      </p>
    {/if}
  </div>
</div>
