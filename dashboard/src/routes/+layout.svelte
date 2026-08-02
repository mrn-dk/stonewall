<script>
  import '../app.css';

  let token = $state(localStorage.getItem('stonewall.token') ?? '');

  function setToken(e) {
    token = e.currentTarget.value.trim();
    if (token) localStorage.setItem('stonewall.token', token);
    else localStorage.removeItem('stonewall.token');
  }
  function clearToken() {
    token = '';
    localStorage.removeItem('stonewall.token');
  }
  function toggleTheme() {
    const t = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = t;
    localStorage.setItem('stonewall.theme', t);
  }
  // Restore saved theme on first paint.
  if (typeof localStorage !== 'undefined') {
    const saved = localStorage.getItem('stonewall.theme');
    if (saved) document.documentElement.dataset.theme = saved;
  }

  let { children } = $props();
</script>

<div class="app">
  <header>
    <a class="brand" href="/">stonewall</a>
    <nav>
      <a href="/">fleet</a>
    </nav>
    <div class="session">
      {#if token}
        <span class="badge" title="authenticated">●</span>
        <button onclick={clearToken} class="link">sign out</button>
      {:else}
        <input
          type="password"
          placeholder="API token (optional)"
          value={token}
          oninput={setToken}
          aria-label="API token"
        />
      {/if}
      <button class="theme" onclick={toggleTheme} title="toggle theme" aria-label="toggle theme">◐</button>
    </div>
  </header>

  <main>
    {@render children()}
  </main>
</div>

<style>
  .app { min-height: 100vh; display: flex; flex-direction: column; }
  header {
    display: flex; align-items: center; gap: 1rem;
    padding: 0.6rem 1rem; border-bottom: 1px solid var(--border);
    background: var(--bg); position: sticky; top: 0; z-index: 10;
  }
  .brand { font-weight: 700; text-decoration: none; color: var(--fg); letter-spacing: -0.02em; }
  nav { display: flex; gap: 0.75rem; }
  nav a { color: var(--muted); text-decoration: none; }
  nav a:hover { color: var(--fg); }
  .session { margin-left: auto; display: flex; align-items: center; gap: 0.5rem; }
  .session input {
    background: var(--panel); color: var(--fg); border: 1px solid var(--border);
    border-radius: 4px; padding: 0.25rem 0.5rem; width: 14rem; font-size: 0.85rem;
  }
  .badge { color: var(--green); }
  .link, .theme {
    background: none; border: 1px solid var(--border); color: var(--muted);
    border-radius: 4px; cursor: pointer; padding: 0.15rem 0.5rem; font-size: 0.85rem;
  }
  .link:hover, .theme:hover { color: var(--fg); border-color: var(--fg); }
  main { flex: 1; padding: 1rem; }
</style>
