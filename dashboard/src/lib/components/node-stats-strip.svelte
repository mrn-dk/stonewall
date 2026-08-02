<script>
  // Fleet counts and node resources.
  //
  // Everything here is a number the API actually returns. There is deliberately
  // no per-agent CPU or memory gauge: the runtime does not sample per-agent
  // usage, and a plausible-looking bar over unmeasured data would be a lie the
  // operator cannot detect.
  import { fmtBytes } from '$lib/agents.js';
  import Cpu from '@lucide/svelte/icons/cpu';
  import MemoryStick from '@lucide/svelte/icons/memory-stick';
  import HardDrive from '@lucide/svelte/icons/hard-drive';

  let { counts, stats = null, statsError = null } = $props();

  const terminal = $derived(
    (counts.completed || 0) + (counts.failed || 0) + (counts.cancelled || 0)
  );

  const pct = (used, total) => (total ? Math.min(100, (used / total) * 100) : 0);
</script>

<div class="flex flex-wrap items-stretch gap-x-6 gap-y-3">
  <div class="flex items-baseline gap-5">
    {#each [['running', counts.running || 0, 'text-state-running'], ['parked', counts.parked || 0, 'text-state-parked'], ['terminal', terminal, 'text-state-terminal'], ['loaded', counts.total || 0, 'text-foreground']] as [label, value, tone] (label)}
      <div class="flex flex-col">
        <span class="text-xl leading-none font-semibold {tone}">{value}</span>
        <span class="text-muted-foreground mt-1 text-xs tracking-wide uppercase">{label}</span>
      </div>
    {/each}
  </div>

  <div class="bg-border w-px self-stretch" aria-hidden="true"></div>

  {#if stats}
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2">
      <div class="flex items-center gap-2">
        <Cpu class="text-muted-foreground size-3.5" />
        <div class="flex flex-col">
          <span class="text-sm leading-none font-medium">{stats.cpu_usage_percent.toFixed(0)}%</span>
          <span class="text-muted-foreground mt-1 text-xs">node cpu</span>
        </div>
      </div>

      {#each [[MemoryStick, 'memory', stats.memory_bytes, stats.memory_total_bytes], [HardDrive, 'storage', stats.storage_bytes, stats.storage_total_bytes]] as [Icon, label, used, total] (label)}
        <div class="flex items-center gap-2">
          <Icon class="text-muted-foreground size-3.5" />
          <div class="flex min-w-28 flex-col">
            <span class="text-sm leading-none font-medium">
              {fmtBytes(used)}
              <span class="text-muted-foreground font-normal">/ {fmtBytes(total)}</span>
            </span>
            <div class="bg-muted mt-1.5 h-1 overflow-hidden rounded-full">
              <div class="bg-foreground/40 h-full rounded-full" style="width: {pct(used, total)}%"></div>
            </div>
            <span class="text-muted-foreground mt-1 text-xs">node {label}</span>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <div class="text-muted-foreground flex items-center text-sm">
      {statsError ? 'Node stats unavailable.' : 'Node stats loading…'}
    </div>
  {/if}
</div>
