<script>
  import { theme } from '$lib/theme.svelte.js';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import Sun from '@lucide/svelte/icons/sun';
  import Moon from '@lucide/svelte/icons/moon';
  import Monitor from '@lucide/svelte/icons/monitor';

  const options = [
    { value: 'light', label: 'Light', icon: Sun },
    { value: 'dark', label: 'Dark', icon: Moon },
    { value: 'system', label: 'System', icon: Monitor }
  ];
</script>

<DropdownMenu.Root>
  <DropdownMenu.Trigger>
    {#snippet child({ props })}
      <Button {...props} variant="ghost" size="icon-sm" aria-label="Theme ({theme.preference})">
        {#if theme.resolved === 'dark'}
          <Moon />
        {:else}
          <Sun />
        {/if}
      </Button>
    {/snippet}
  </DropdownMenu.Trigger>
  <DropdownMenu.Content align="end" class="w-36">
    {#each options as opt (opt.value)}
      {@const Icon = opt.icon}
      <DropdownMenu.CheckboxItem
        checked={theme.preference === opt.value}
        onCheckedChange={() => theme.set(opt.value)}
      >
        <Icon class="size-3.5" />
        {opt.label}
      </DropdownMenu.CheckboxItem>
    {/each}
  </DropdownMenu.Content>
</DropdownMenu.Root>
