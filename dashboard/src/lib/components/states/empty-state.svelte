<script>
  // An empty result, reached and confirmed. Says what is absent, and where an
  // action would populate it, offers that action.
  import * as Empty from '$lib/components/ui/empty/index.js';

  let { title, description = '', icon, children, class: className = '' } = $props();
  const Icon = $derived(icon);
</script>

<Empty.Root class="border {className}">
  <Empty.Header>
    {#if Icon}
      <Empty.Media variant="icon">
        <Icon />
      </Empty.Media>
    {/if}
    <Empty.Title>{title}</Empty.Title>
    {#if description}
      <Empty.Description>{description}</Empty.Description>
    {/if}
  </Empty.Header>
  {#if children}
    <Empty.Content>
      {@render children()}
    </Empty.Content>
  {/if}
</Empty.Root>
