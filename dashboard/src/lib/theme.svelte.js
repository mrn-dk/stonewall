// theme.svelte.js — the resolved light/dark theme.
//
// Three-state model: the operator's preference is 'system' (the default),
// 'light', or 'dark'; the *resolved* theme is only ever 'light' or 'dark' and
// is what lands on <html data-theme>, which is the single switch the token
// layer in app.css reads.
//
// An explicit preference persists under 'stonewall.theme' — the same key the
// pre-refresh dashboard used, so an operator's stored choice survives the
// upgrade. Absence of the key means "follow the OS", and in that state the
// theme tracks OS changes live rather than only at load.
//
// The first paint is handled by the inline script in app.html; this module
// takes ownership from there.

const KEY = 'stonewall.theme';

const query = () =>
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null;

function storedPreference() {
  if (typeof localStorage === 'undefined') return 'system';
  const saved = localStorage.getItem(KEY);
  return saved === 'light' || saved === 'dark' ? saved : 'system';
}

class Theme {
  /** @type {'system' | 'light' | 'dark'} */
  preference = $state(storedPreference());
  /** Tracks the OS setting so 'system' stays live rather than load-time only. */
  systemPrefersDark = $state(query()?.matches ?? false);

  /** @type {'light' | 'dark'} */
  get resolved() {
    if (this.preference === 'system') return this.systemPrefersDark ? 'dark' : 'light';
    return this.preference;
  }

  /** @param {'system' | 'light' | 'dark'} next */
  set(next) {
    this.preference = next;
    if (typeof localStorage === 'undefined') return;
    if (next === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, next);
  }

  /** Cycles light -> dark -> system, for the header's single toggle control. */
  cycle() {
    this.set(this.resolved === 'dark' ? 'light' : this.preference === 'system' ? 'dark' : 'system');
  }
}

export const theme = new Theme();

/** Applies the resolved theme to <html> and keeps it in step with the OS. */
export function startTheme() {
  const mq = query();
  const onChange = (/** @type {MediaQueryListEvent} */ e) => (theme.systemPrefersDark = e.matches);
  mq?.addEventListener('change', onChange);

  $effect(() => {
    document.documentElement.dataset.theme = theme.resolved;
  });

  return () => mq?.removeEventListener('change', onChange);
}
