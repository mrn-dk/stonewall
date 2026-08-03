<script>
  // A directory node and its children. Recursive: a folder renders a
  // <FileTree> for its own contents.
  import FileTree from './file-tree.svelte';
  import { iconFor } from '$lib/files.js';
  import { fmtBytes } from '$lib/agents.js';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Folder from '@lucide/svelte/icons/folder';
  import FolderOpen from '@lucide/svelte/icons/folder-open';

  let { nodes, activeFile, onopen, expanded, ontoggle, depth = 0 } = $props();

  // Indent with padding rather than nested margins so long paths stay readable
  // at depth and the row's hit area still spans the full pane.
  const pad = (d) => `padding-left: ${0.25 + d * 0.75}rem`;
</script>

<ul class="list-none">
  {#each nodes as node (node.path)}
    <li>
      {#if node.type === 'dir'}
        {@const isOpen = expanded.has(node.path)}
        <button
          type="button"
          class="hover:bg-muted flex w-full items-center gap-1 rounded-md py-0.5 pr-1.5 text-left text-xs transition-colors"
          style={pad(depth)}
          aria-expanded={isOpen}
          onclick={() => ontoggle(node.path)}
        >
          <ChevronRight
            class="text-muted-foreground size-3 shrink-0 transition-transform {isOpen
              ? 'rotate-90'
              : ''}"
          />
          {#if isOpen}
            <FolderOpen class="text-muted-foreground size-3 shrink-0" />
          {:else}
            <Folder class="text-muted-foreground size-3 shrink-0" />
          {/if}
          <span class="truncate font-mono">{node.name}</span>
          <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
            {node.sorted.length}
          </span>
        </button>
        {#if isOpen}
          <FileTree
            nodes={node.sorted}
            {activeFile}
            {onopen}
            {expanded}
            {ontoggle}
            depth={depth + 1}
          />
        {/if}
      {:else}
        {@const Icon = iconFor(node.path)}
        <button
          type="button"
          class="flex w-full items-center gap-1 rounded-md py-0.5 pr-1.5 text-left text-xs transition-colors {activeFile ===
          node.path
            ? 'bg-secondary text-secondary-foreground'
            : 'hover:bg-muted'}"
          style={pad(depth + 1)}
          aria-pressed={activeFile === node.path}
          onclick={() => onopen(node.path)}
        >
          <Icon class="text-muted-foreground size-3 shrink-0" />
          <span class="truncate font-mono">{node.name}</span>
          <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
            {fmtBytes(node.size)}
          </span>
        </button>
      {/if}
    </li>
  {/each}
</ul>
