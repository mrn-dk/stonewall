<script>
  import '../app.css';
  import AppShell from '$lib/components/app-shell.svelte';
  import { startTheme } from '$lib/theme.svelte.js';

  startTheme();

  let { children } = $props();
</script>

<AppShell>
  {@render children()}
</AppShell>
