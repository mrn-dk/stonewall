<script>
  // The session control. It says exactly what is true and no more: a token is
  // stored and sent, and this server does not check it. The old header showed
  // a green "authenticated" dot for a credential nothing had verified.
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { session } from '$lib/session.svelte.js';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import ShieldAlert from '@lucide/svelte/icons/shield-alert';

  let open = $state(false);
  let draft = $state('');

  function onOpenChange(next) {
    open = next;
    if (next) draft = session.token;
  }

  function save() {
    session.set(draft);
    open = false;
  }
</script>

<Dialog.Root bind:open {onOpenChange}>
  <Dialog.Trigger>
    {#snippet child({ props })}
      <Button {...props} variant="ghost" size="sm" class="gap-1.5">
        <KeyRound class="size-3.5" />
        <span class="text-muted-foreground">
          {#if session.status === 'anonymous'}
            No token
          {:else}
            Token set
          {/if}
        </span>
      </Button>
    {/snippet}
  </Dialog.Trigger>

  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header>
      <Dialog.Title>API token</Dialog.Title>
      <Dialog.Description>
        Sent as an <span class="font-mono">Authorization: Bearer</span> header with every request.
      </Dialog.Description>
    </Dialog.Header>

    <div class="grid gap-2">
      <Label for="api-token">Token</Label>
      <Input
        id="api-token"
        type="password"
        bind:value={draft}
        placeholder="optional"
        autocomplete="off"
        onkeydown={(e) => e.key === 'Enter' && save()}
      />
    </div>

    <div
      class="text-muted-foreground flex items-start gap-2 rounded-md border border-dashed p-2.5 text-xs"
    >
      <ShieldAlert class="text-state-pending mt-0.5 size-3.5 shrink-0" />
      <p>
        This server does not authenticate requests. A token is stored and sent, but nothing verifies
        it — so the dashboard cannot tell you that you are signed in, or what your credential is
        allowed to do.
      </p>
    </div>

    {#if session.refusal}
      <p class="text-muted-foreground text-xs">
        An intervention ({session.refusal.action}) was refused at
        {session.refusal.at.toLocaleTimeString()}, so intervention controls are hidden. Setting a
        different token clears that.
      </p>
    {/if}

    <Dialog.Footer>
      {#if session.token}
        <Button variant="ghost" size="sm" onclick={() => { session.clear(); open = false; }}>
          Clear token
        </Button>
      {/if}
      <Button size="sm" onclick={save}>Save</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
