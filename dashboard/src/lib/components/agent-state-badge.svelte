<script>
  // One rendering of agent state for the whole app. The dot carries the colour
  // and the word carries the meaning, so the badge does not rely on colour
  // alone to say what state an agent is in.
  import { STATE_TONE } from '$lib/agents.js';

  let { state, class: className = '' } = $props();

  const tones = {
    running: 'text-state-running border-state-running/40 bg-state-running/10',
    parked: 'text-state-parked border-state-parked/40 bg-state-parked/10',
    pending: 'text-state-pending border-state-pending/40 bg-state-pending/10',
    terminal: 'text-state-terminal border-state-terminal/40 bg-state-terminal/10',
    failed: 'text-state-failed border-state-failed/40 bg-state-failed/10'
  };
  const dots = {
    running: 'bg-state-running',
    parked: 'bg-state-parked',
    pending: 'bg-state-pending',
    terminal: 'bg-state-terminal',
    failed: 'bg-state-failed'
  };

  const tone = $derived(STATE_TONE[state] ?? 'parked');
</script>

<span
  class="inline-flex items-center gap-1.5 rounded-full border px-1.5 py-px text-xs font-medium {tones[
    tone
  ]} {className}"
>
  <span
    class="size-1.5 rounded-full {dots[tone]} {state === 'running' ? 'animate-pulse' : ''}"
    aria-hidden="true"
  ></span>
  {state}
</span>
