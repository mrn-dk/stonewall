<script>
  // Renders a markdown block tree. Paired with markdown-inline.svelte; see
  // $lib/markdown.js for why this renders a token tree rather than HTML.
  import Markdown from './markdown.svelte';
  import MarkdownInline from './markdown-inline.svelte';
  import { parseMarkdown } from '$lib/markdown.js';

  let { source = null, blocks = null, class: className = '' } = $props();

  const tree = $derived(blocks ?? parseMarkdown(source));

  const headingClass = {
    1: 'text-base font-semibold mt-3 mb-1 first:mt-0',
    2: 'text-sm font-semibold mt-3 mb-1 first:mt-0',
    3: 'text-sm font-semibold mt-2 mb-1 first:mt-0',
    4: 'text-sm font-medium mt-2 mb-0.5 first:mt-0',
    5: 'text-xs font-medium mt-2 mb-0.5 first:mt-0',
    6: 'text-xs font-medium text-muted-foreground mt-2 mb-0.5 first:mt-0'
  };
</script>

<div class="text-sm leading-relaxed {className}">
  {#each tree as block, i (i)}
    {#if block.type === 'paragraph'}
      <p class="my-1.5 break-words whitespace-pre-wrap first:mt-0 last:mb-0">
        <MarkdownInline tokens={block.children} />
      </p>
    {:else if block.type === 'heading'}
      <svelte:element this={`h${block.depth}`} class={headingClass[block.depth]}>
        <MarkdownInline tokens={block.children} />
      </svelte:element>
    {:else if block.type === 'code'}
      <div class="bg-muted/60 my-1.5 overflow-hidden rounded-md border">
        {#if block.lang}
          <div
            class="text-muted-foreground border-b px-2 py-0.5 font-mono text-[0.7rem] tracking-wide"
          >
            {block.lang}
          </div>
        {/if}
        <pre
          class="scrollbar-thin overflow-x-auto p-2 font-mono text-xs leading-relaxed">{block.value}</pre>
      </div>
    {:else if block.type === 'list'}
      {#if block.ordered}
        <ol class="my-1.5 list-decimal space-y-0.5 pl-5">
          {#each block.items as item, j (j)}
            <li><MarkdownInline tokens={item} /></li>
          {/each}
        </ol>
      {:else}
        <ul class="my-1.5 list-disc space-y-0.5 pl-5">
          {#each block.items as item, j (j)}
            <li><MarkdownInline tokens={item} /></li>
          {/each}
        </ul>
      {/if}
    {:else if block.type === 'blockquote'}
      <blockquote class="text-muted-foreground my-1.5 border-l-2 pl-2.5">
        <Markdown blocks={block.children} />
      </blockquote>
    {:else if block.type === 'hr'}
      <hr class="my-2" />
    {/if}
  {/each}
</div>
