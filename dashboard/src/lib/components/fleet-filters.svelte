<script>
  // State chips and text search.
  //
  // Both write to the URL (?state=&q=) rather than to local component state,
  // which is what makes a filtered view linkable and reload-proof. The page
  // derives its request from the URL, so there is one source of truth and the
  // back button behaves.
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { AGENT_STATES } from '$lib/agents.js';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';

  let { state: activeState = '', q = '' } = $props();

  // `typed` holds only in-flight keystrokes; null means "show what the URL
  // says". That way the input stays responsive while the URL update is
  // debounced, and an external change (back button, cleared filters) is adopted
  // automatically instead of being clobbered by a stale local copy.
  let typed = $state(/** @type {string | null} */ (null));
  const draft = $derived(typed ?? q);
  let timer;

  function apply(next, { replace = true } = {}) {
    const sp = new URLSearchParams(page.url.searchParams);
    for (const [k, v] of Object.entries(next)) {
      if (v) sp.set(k, v);
      else sp.delete(k);
    }
    const query = sp.toString();
    goto(query ? `?${query}` : page.url.pathname, {
      replaceState: replace,
      keepFocus: true,
      noScroll: true
    });
  }

  function onInput(e) {
    const value = e.currentTarget.value;
    typed = value;
    clearTimeout(timer);
    timer = setTimeout(() => {
      apply({ q: value.trim() });
      // Hand control back to the URL now that it reflects this keystroke.
      typed = null;
    }, 250);
  }

  function clearQuery() {
    clearTimeout(timer);
    typed = null;
    apply({ q: '' });
  }

  function clearAll() {
    clearTimeout(timer);
    typed = null;
    apply({ state: '', q: '' });
  }

  const hasFilter = $derived(Boolean(activeState || q));
</script>

<div class="flex flex-wrap items-center gap-2">
  <div class="relative w-full sm:w-72">
    <Search
      class="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2"
    />
    <Input
      type="search"
      value={draft}
      oninput={onInput}
      placeholder="Search goal or image…"
      aria-label="Search agents by goal or image"
      class="h-7 pr-7 pl-7"
    />
    {#if draft}
      <button
        type="button"
        onclick={clearQuery}
        aria-label="Clear search"
        class="text-muted-foreground hover:text-foreground absolute top-1/2 right-1.5 -translate-y-1/2 rounded-sm"
      >
        <X class="size-3.5" />
      </button>
    {/if}
  </div>

  <div class="flex flex-wrap items-center gap-1" role="group" aria-label="Filter by state">
    <Button
      variant={activeState ? 'ghost' : 'secondary'}
      size="xs"
      aria-pressed={!activeState}
      onclick={() => apply({ state: '' })}
    >
      all
    </Button>
    {#each AGENT_STATES as s (s)}
      <Button
        variant={activeState === s ? 'secondary' : 'ghost'}
        size="xs"
        aria-pressed={activeState === s}
        onclick={() => apply({ state: activeState === s ? '' : s })}
      >
        {s}
      </Button>
    {/each}
  </div>

  {#if hasFilter}
    <Button
      variant="ghost"
      size="xs"
      class="text-muted-foreground"
      onclick={clearAll}
    >
      Clear filters
    </Button>
  {/if}
</div>
