// palette.svelte.js — the command palette's open state and its contextual
// actions.
//
// The palette itself knows nothing about agents. A screen that has actions
// registers them while it is mounted, which keeps two rules cheap to honour:
// an action is offered only where it is valid, and an action the session
// cannot perform is never registered in the first place — the palette must not
// surface something whose visible control is hidden.

class Palette {
  open = $state(false);
  /**
   * @type {Array<{
   *   id: string,
   *   label: string,
   *   group?: string,
   *   keywords?: string,
   *   icon?: unknown,
   *   run: () => void
   * }>}
   */
  actions = $state([]);

  toggle() {
    this.open = !this.open;
  }

  /** @param {Palette['actions']} actions */
  setActions(actions) {
    this.actions = actions;
  }

  clearActions() {
    this.actions = [];
  }
}

export const palette = new Palette();

/**
 * Registers contextual actions for as long as the calling component is alive.
 * Call inside `$effect` so the actions track the current context and are
 * withdrawn on navigation.
 *
 * @param {() => Palette['actions']} get
 */
export function registerPaletteActions(get) {
  $effect(() => {
    palette.setActions(get());
    return () => palette.clearActions();
  });
}
