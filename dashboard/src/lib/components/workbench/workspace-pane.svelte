<script>
  // The workspace as it existed at the selected turn, read from that turn's
  // checkpoint through the read-only browse endpoint. Browsing never touches
  // the live workspace — that is `restore`, and it lives on the action bar
  // behind a confirmation.
  import { Button } from '$lib/components/ui/button/index.js';
  import { fmtBytes } from '$lib/agents.js';
  import { buildTree, totalSize } from '$lib/files.js';
  import FileTree from './file-tree.svelte';
  import FileViewer from './file-viewer.svelte';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import ErrorState from '$lib/components/states/error-state.svelte';
  import GitCompare from '@lucide/svelte/icons/git-compare';
  import { SvelteSet } from 'svelte/reactivity';

  let {
    selectedTurn,
    workspace,
    loading,
    error,
    diff,
    showDiff,
    activeFile,
    file,
    fileLoading,
    onretry,
    ontogglediff,
    onopenfile,
    oncloseFile
  } = $props();

  const files = $derived(workspace?.files ?? []);
  const tree = $derived(buildTree(files));
  const fileCount = $derived(files.filter((f) => !f.is_dir).length);

  // Directories start expanded: a checkpoint is usually small, and a tree that
  // opens closed makes you click to discover it is not empty.
  let expanded = $state(new SvelteSet());
  let lastCheckpoint = $state(null);

  $effect(() => {
    const ckpt = workspace?.checkpoint_id ?? null;
    if (ckpt === lastCheckpoint) return;
    lastCheckpoint = ckpt;
    const dirs = new SvelteSet();
    const walk = (nodes) => {
      for (const n of nodes) {
        if (n.type === 'dir') {
          dirs.add(n.path);
          walk(n.sorted);
        }
      }
    };
    walk(tree);
    expanded = dirs;
  });

  function toggle(path) {
    if (expanded.has(path)) expanded.delete(path);
    else expanded.add(path);
  }

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
            <ul class="list-none space-y-0.5 font-mono">
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

      {#if fileCount === 0}
        <p class="text-muted-foreground px-1.5 py-3 text-sm">This checkpoint contains no files.</p>
      {:else}
        <FileTree
          nodes={tree}
          {activeFile}
          {expanded}
          onopen={onopenfile}
          ontoggle={toggle}
        />
        <p class="text-muted-foreground mt-1.5 border-t px-1.5 pt-1.5 text-[0.7rem] tabular-nums">
          {fileCount} file{fileCount === 1 ? '' : 's'} · {fmtBytes(totalSize(files))}
        </p>
      {/if}

      {#if activeFile}
        <FileViewer
          path={activeFile}
          content={file?.content}
          kind={file?.kind}
          imageUrl={file?.imageUrl}
          size={file?.size}
          truncated={file?.truncated}
          loading={fileLoading}
          onclose={oncloseFile}
        />
      {/if}
    {/if}
  </div>
</div>
