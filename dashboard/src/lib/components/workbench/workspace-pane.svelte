<script>
  // The workspace as it existed at the selected turn, read from that turn's
  // checkpoint through the read-only browse endpoint. Browsing never touches
  // the live workspace — that is `restore`, and it lives on the action bar
  // behind a confirmation.
  import { Button } from '$lib/components/ui/button/index.js';
  import { fmtBytes } from '$lib/agents.js';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import ErrorState from '$lib/components/states/error-state.svelte';
  import FileText from '@lucide/svelte/icons/file-text';
  import Folder from '@lucide/svelte/icons/folder';
  import GitCompare from '@lucide/svelte/icons/git-compare';
  import X from '@lucide/svelte/icons/x';

  let {
    selectedTurn,
    workspace,
    loading,
    error,
    diff,
    showDiff,
    activeFile,
    fileContent,
    fileLoading,
    onretry,
    ontogglediff,
    onopenfile,
    oncloseFile
  } = $props();

  const diffRows = $derived(
    diff && !diff.none && !diff.error
      ? [
          ...diff.added.map((p) => ['added', p]),
          ...diff.changed.map((p) => ['changed', p]),
          ...diff.removed.map((p) => ['removed', p])
        ]
      : []
  );

  const diffTone = {
    added: 'text-diff-added',
    changed: 'text-diff-changed',
    removed: 'text-diff-removed'
  };
  const diffMark = { added: '+', changed: '~', removed: '-' };
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex items-center justify-between gap-2 px-1 pb-1.5">
    <h2 class="text-muted-foreground text-xs font-medium tracking-wide uppercase">
      Workspace
      {#if selectedTurn != null}
        <span class="normal-case">@ turn {selectedTurn}</span>
      {/if}
    </h2>
    {#if workspace && !workspace._none && selectedTurn != null}
      <Button
        variant={showDiff ? 'secondary' : 'ghost'}
        size="xs"
        aria-pressed={showDiff}
        onclick={ontogglediff}
      >
        <GitCompare data-icon="inline-start" />
        diff
      </Button>
    {/if}
  </div>

  <div class="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-0.5 pb-1">
    {#if error}
      <ErrorState {error} retry={onretry} title="Could not read the workspace" />
    {:else if loading}
      <LoadingState label="Reading checkpoint" rows={4} />
    {:else if !workspace}
      <p class="text-muted-foreground px-1.5 py-3 text-sm">
        Select a turn in the timeline to see the workspace as it was then.
      </p>
    {:else if workspace._none}
      <p class="text-muted-foreground px-1.5 py-3 text-sm">
        No checkpoint at this turn. The checkpoint policy decides which turns produce one.
      </p>
    {:else}
      {#if showDiff}
        <div class="mb-2 rounded-md border p-2 text-xs">
          {#if diff?.error}
            <span class="text-state-failed">Diff failed: {diff.error}</span>
          {:else if diff?.none}
            <span class="text-muted-foreground">No previous checkpoint to compare against.</span>
          {:else if diffRows.length === 0}
            <span class="text-muted-foreground">No file changes since the previous checkpoint.</span>
          {:else}
            <ul class="space-y-0.5 font-mono">
              {#each diffRows as [kind, path] (kind + path)}
                <li class={diffTone[kind]}>
                  <span aria-hidden="true">{diffMark[kind]}</span>
                  <span class="sr-only">{kind}</span>
                  {path}
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}

      <ul class="space-y-px">
        {#each workspace.files ?? [] as f (f.path)}
          <li>
            {#if f.is_dir}
              <span class="text-muted-foreground flex items-center gap-1.5 px-1.5 py-0.5 text-xs">
                <Folder class="size-3 shrink-0" />
                <span class="truncate font-mono">{f.path}</span>
              </span>
            {:else}
              <button
                type="button"
                class="flex w-full items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left text-xs transition-colors {activeFile ===
                f.path
                  ? 'bg-secondary text-secondary-foreground'
                  : 'hover:bg-muted'}"
                aria-pressed={activeFile === f.path}
                onclick={() => onopenfile(f.path)}
              >
                <FileText class="text-muted-foreground size-3 shrink-0" />
                <span class="truncate font-mono">{f.path}</span>
                <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
                  {fmtBytes(f.size)}
                </span>
              </button>
            {/if}
          </li>
        {:else}
          <li class="text-muted-foreground px-1.5 py-3 text-sm">
            This checkpoint contains no files.
          </li>
        {/each}
      </ul>

      {#if activeFile}
        <div class="mt-2 rounded-md border">
          <div class="flex items-center gap-2 border-b px-2 py-1">
            <span class="truncate font-mono text-xs">{activeFile}</span>
            <Button
              variant="ghost"
              size="icon-xs"
              class="ml-auto"
              onclick={oncloseFile}
              aria-label="Close file"
            >
              <X />
            </Button>
          </div>
          {#if fileLoading}
            <LoadingState label="Reading file" class="px-2" />
          {:else}
            <pre
              class="bg-muted/50 scrollbar-thin max-h-80 overflow-auto p-2 font-mono text-xs break-words whitespace-pre-wrap">{fileContent ??
                ''}</pre>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</div>
