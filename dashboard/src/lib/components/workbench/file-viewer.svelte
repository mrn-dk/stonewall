<script>
  // Renders a file's contents rather than dumping them.
  //
  // Three cases, and it matters that they are distinguished: text gets line
  // numbers (so "the error is on line 40" is a thing you can act on), images
  // are shown as images, and anything that decodes to binary says so plainly
  // instead of spraying replacement characters down the pane.
  import { Button } from '$lib/components/ui/button/index.js';
  import { fmtBytes } from '$lib/agents.js';
  import { iconFor } from '$lib/files.js';
  import LoadingState from '$lib/components/states/loading-state.svelte';
  import X from '@lucide/svelte/icons/x';
  import FileWarning from '@lucide/svelte/icons/file-warning';

  let { path, content, loading, kind, imageUrl, size, truncated, onclose } = $props();

  const Icon = $derived(iconFor(path ?? ''));
  const lines = $derived(kind === 'text' && content ? content.split('\n') : []);
  // Right-align the gutter to the widest line number so the code column does
  // not shift as the file scrolls past line 99.
  const gutterWidth = $derived(`${String(lines.length).length}ch`);
</script>

<div class="mt-2 flex min-h-0 flex-col rounded-md border">
  <div class="flex items-center gap-1.5 border-b px-2 py-1">
    <Icon class="text-muted-foreground size-3 shrink-0" />
    <span class="truncate font-mono text-xs" title={path}>{path}</span>
    <span class="text-muted-foreground ml-auto shrink-0 text-xs tabular-nums">
      {#if kind === 'text' && lines.length}
        {lines.length} lines ·
      {/if}
      {fmtBytes(size)}
    </span>
    <Button variant="ghost" size="icon-xs" onclick={onclose} aria-label="Close file">
      <X />
    </Button>
  </div>

  {#if loading}
    <LoadingState label="Reading file" class="px-2" />
  {:else if kind === 'image' && imageUrl}
    <div class="bg-muted/40 scrollbar-thin max-h-80 overflow-auto p-3">
      <img
        src={imageUrl}
        alt={path}
        class="mx-auto max-w-full"
        style="image-rendering: auto"
      />
    </div>
  {:else if kind === 'binary'}
    <div class="text-muted-foreground flex items-start gap-2 p-3 text-xs">
      <FileWarning class="text-state-pending mt-0.5 size-3.5 shrink-0" />
      <p>
        This file is not valid UTF-8 text, so it is not rendered. It is stored intact in the
        checkpoint — the workbench simply has nothing useful to show for {fmtBytes(size)} of binary.
      </p>
    </div>
  {:else if content !== null && content !== undefined}
    <div class="scrollbar-thin max-h-80 overflow-auto">
      <table class="w-full border-collapse font-mono text-xs">
        <tbody>
          {#each lines as line, i (i)}
            <tr class="hover:bg-muted/50">
              <td
                class="text-muted-foreground/60 bg-muted/30 sticky left-0 border-r px-2 text-right align-top tabular-nums select-none"
                style="width: {gutterWidth}"
              >
                {i + 1}
              </td>
              <td class="w-full px-2 align-top break-words whitespace-pre-wrap">{line}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if truncated}
      <p class="text-muted-foreground border-t px-2 py-1 text-xs">
        Truncated for display — the checkpoint holds the whole file.
      </p>
    {/if}
  {/if}
</div>
