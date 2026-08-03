<script>
  // Confirmation for destructive and state-rewriting actions.
  //
  // Replaces window.confirm(), which could not name the agent, could not name
  // the effect, and could not be styled or made accessible. Nothing is sent
  // until the operator confirms; Escape closes without acting and returns
  // focus to whatever opened the dialog (AlertDialog handles the focus return).
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { buttonVariants } from '$lib/components/ui/button/index.js';

  let {
    open = $bindable(false),
    title,
    /** What will happen, in plain words, naming the specific agent. */
    description,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    destructive = true,
    busy = false,
    onconfirm
  } = $props();
</script>

<AlertDialog.Root bind:open>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{title}</AlertDialog.Title>
      <AlertDialog.Description>{description}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{cancelLabel}</AlertDialog.Cancel>
      <AlertDialog.Action
        class={buttonVariants({ variant: destructive ? 'destructive' : 'default', size: 'sm' })}
        disabled={busy}
        onclick={(e) => {
          // Keep the dialog up while the request is in flight so the operator
          // sees the pending state rather than an interface that looks idle.
          e.preventDefault();
          onconfirm?.();
        }}
      >
        {busy ? 'Working…' : confirmLabel}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
