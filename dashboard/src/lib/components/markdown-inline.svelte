<script>
  // Renders inline markdown tokens as real elements. Recursive, and with no
  // {@html} anywhere: agent text reaches the DOM only as a text node.
  import MarkdownInline from './markdown-inline.svelte';

  let { tokens } = $props();
</script>

{#each tokens as token, i (i)}
  {#if token.type === 'text'}{token.value}{:else if token.type === 'code'}<code
      class="bg-muted rounded px-1 py-px font-mono text-[0.85em]">{token.value}</code
    >{:else if token.type === 'strong'}<strong class="font-semibold"
      ><MarkdownInline tokens={token.children} /></strong
    >{:else if token.type === 'em'}<em class="italic"
      ><MarkdownInline tokens={token.children} /></em
    >{:else if token.type === 'del'}<del class="line-through"
      ><MarkdownInline tokens={token.children} /></del
    >{:else if token.type === 'link'}<a
      href={token.href}
      target="_blank"
      rel="noopener noreferrer nofollow"
      class="underline underline-offset-2">{#if token.children.length}<MarkdownInline
          tokens={token.children}
        />{:else}{token.href}{/if}</a
    >{/if}{/each}
