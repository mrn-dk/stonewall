<script>
  import { page } from '$app/state';
  import { palette } from '$lib/palette.svelte.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import ThemeToggle from '$lib/components/theme-toggle.svelte';
  import SessionDialog from '$lib/components/session-dialog.svelte';
  import CommandPalette from '$lib/components/command-palette.svelte';
  import Toaster from '$lib/components/toaster.svelte';
  import Search from '@lucide/svelte/icons/search';

  let { children } = $props();

  const onFleet = $derived(page.url.pathname === '/');
</script>

<div class="flex min-h-screen flex-col">
  <header
    class="bg-background/95 supports-[backdrop-filter]:bg-background/80 sticky top-0 z-30 flex h-11 shrink-0 items-center gap-3 border-b px-3 backdrop-blur"
  >
    <a
      href="/"
      class="text-foreground rounded-sm text-sm font-semibold tracking-tight lowercase hover:opacity-80"
    >
      stonewall
    </a>

    <nav class="flex items-center gap-1" aria-label="Main">
      <a
        href="/"
        class="rounded-md px-2 py-1 text-sm transition-colors {onFleet
          ? 'bg-muted text-foreground'
          : 'text-muted-foreground hover:text-foreground'}"
        aria-current={onFleet ? 'page' : undefined}
      >
        Fleet
      </a>
    </nav>

    <div class="ml-auto flex items-center gap-1">
      <Button
        variant="outline"
        size="sm"
        class="text-muted-foreground gap-2 font-normal"
        onclick={() => palette.toggle()}
      >
        <Search class="size-3.5" />
        <span class="hidden sm:inline">Search</span>
        <kbd
          class="bg-muted text-muted-foreground hidden rounded border px-1 font-mono text-[0.65rem] sm:inline"
        >
          ⌘K
        </kbd>
      </Button>
      <SessionDialog />
      <ThemeToggle />
    </div>
  </header>

  <main class="min-h-0 flex-1 p-3">
    {@render children()}
  </main>
</div>

<CommandPalette />
<Toaster />
