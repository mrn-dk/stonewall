<script>
  // Create an agent. The API has always supported this; until now the only way
  // to reach it from the dashboard was to leave and use curl.
  //
  // Grants are entered in the same shorthand the rest of the UI displays them
  // in, so what an operator types matches what they will later read back.
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Select from '$lib/components/ui/select/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Textarea } from '$lib/components/ui/textarea/index.js';
  import { api } from '$lib/api.js';
  import { toasts } from '$lib/toasts.svelte.js';
  import { session } from '$lib/session.svelte.js';
  import { ISOLATION_MODES, CHECKPOINT_POLICIES, BROAD_COMMANDS } from '$lib/agents.js';
  import { goto } from '$app/navigation';
  import Plus from '@lucide/svelte/icons/plus';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';

  let { open = $bindable(false), variant = 'default', size = 'sm' } = $props();

  let image = $state('');
  let goal = $state('');
  let model = $state('');
  let isolation = $state('dedicated');
  let checkpoint = $state('on_write');
  let fsGrants = $state('/workspace:rw');
  let netGrants = $state('');
  let cmdGrants = $state('');
  let busy = $state(false);

  /** Parses "/workspace:rw /tools:ro" into { "/workspace": "rw", ... }. */
  function parseFs(input) {
    const out = {};
    for (const part of input.split(/[\s,]+/).filter(Boolean)) {
      const idx = part.lastIndexOf(':');
      if (idx <= 0) continue;
      const path = part.slice(0, idx);
      const mode = part.slice(idx + 1);
      if (mode === 'ro' || mode === 'rw') out[path] = mode;
    }
    return out;
  }

  const list = (input) => input.split(/[\s,]+/).filter(Boolean);

  const cmdList = $derived(list(cmdGrants));
  const broad = $derived(cmdList.filter((c) => BROAD_COMMANDS.includes(c)));
  const valid = $derived(image.trim().length > 0);

  function reset() {
    image = '';
    goal = '';
    model = '';
    isolation = 'dedicated';
    checkpoint = 'on_write';
    fsGrants = '/workspace:rw';
    netGrants = '';
    cmdGrants = '';
  }

  async function submit(e) {
    e?.preventDefault();
    if (!valid || busy) return;
    busy = true;
    const body = {
      image: image.trim(),
      goal: goal.trim(),
      model: model.trim(),
      isolation,
      checkpoint,
      grants: { fs: parseFs(fsGrants), net: list(netGrants), cmd: cmdList }
    };
    try {
      const created = await api.createAgent(body);
      toasts.success(`Agent ${created.id} created`);
      open = false;
      reset();
      goto(`/agents/${created.id}`);
    } catch (err) {
      if (err.status === 403) {
        session.noteRefusal('create agent');
        open = false;
        toasts.error('Create refused', err);
      } else {
        toasts.error('Could not create agent', err, { retry: () => submit() });
      }
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Trigger>
    {#snippet child({ props })}
      <Button {...props} {variant} {size}>
        <Plus data-icon="inline-start" />
        New agent
      </Button>
    {/snippet}
  </Dialog.Trigger>

  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>New agent</Dialog.Title>
      <Dialog.Description>
        Everything not granted here does not exist from inside the sandbox.
      </Dialog.Description>
    </Dialog.Header>

    <form class="grid gap-3" onsubmit={submit}>
      <div class="grid gap-1.5">
        <Label for="create-image">Image <span class="text-destructive">*</span></Label>
        <Input
          id="create-image"
          bind:value={image}
          placeholder="acme/agent-host:1.4"
          required
          autocomplete="off"
        />
      </div>

      <div class="grid gap-1.5">
        <Label for="create-goal">Goal</Label>
        <Textarea id="create-goal" bind:value={goal} rows={2} placeholder="summarise the repo" />
      </div>

      <div class="grid gap-3 sm:grid-cols-3">
        <div class="grid gap-1.5">
          <Label for="create-model">Model</Label>
          <Input id="create-model" bind:value={model} placeholder="gpt-4o" autocomplete="off" />
        </div>
        <div class="grid gap-1.5">
          <Label for="create-isolation">Isolation</Label>
          <Select.Root type="single" bind:value={isolation}>
            <Select.Trigger id="create-isolation" class="w-full">{isolation}</Select.Trigger>
            <Select.Content>
              {#each ISOLATION_MODES as mode (mode)}
                <Select.Item value={mode} label={mode} />
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
        <div class="grid gap-1.5">
          <Label for="create-checkpoint">Checkpoint</Label>
          <Select.Root type="single" bind:value={checkpoint}>
            <Select.Trigger id="create-checkpoint" class="w-full">{checkpoint}</Select.Trigger>
            <Select.Content>
              {#each CHECKPOINT_POLICIES as policy (policy)}
                <Select.Item value={policy} label={policy} />
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
      </div>

      <fieldset class="grid gap-2 rounded-lg border p-2.5">
        <legend class="px-1 text-xs tracking-wide uppercase">Grants</legend>
        <div class="grid gap-1.5">
          <Label for="create-fs" class="text-xs">fs — path:ro|rw, space separated</Label>
          <Input id="create-fs" bind:value={fsGrants} class="font-mono" autocomplete="off" />
        </div>
        <div class="grid gap-1.5">
          <Label for="create-net" class="text-xs">net — endpoint allow-list</Label>
          <Input
            id="create-net"
            bind:value={netGrants}
            class="font-mono"
            placeholder="mortise.internal"
            autocomplete="off"
          />
        </div>
        <div class="grid gap-1.5">
          <Label for="create-cmd" class="text-xs">cmd — command allow-list</Label>
          <Input
            id="create-cmd"
            bind:value={cmdGrants}
            class="font-mono"
            placeholder="rg git"
            autocomplete="off"
          />
        </div>
        {#if broad.length}
          <p class="text-state-pending flex items-start gap-1.5 text-xs">
            <TriangleAlert class="mt-0.5 size-3.5 shrink-0" />
            <span>
              {broad.join(', ')}
              {broad.length === 1 ? 'is' : 'are'} effectively everything in the image — the allow-list
              controls which binaries run, not what they do. The security boundary is the sandbox.
            </span>
          </p>
        {/if}
      </fieldset>

      <Dialog.Footer>
        <Button type="button" variant="ghost" size="sm" onclick={() => (open = false)}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={!valid || busy}>
          {busy ? 'Creating…' : 'Create agent'}
        </Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
