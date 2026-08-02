<script>
  import { api, session } from '$lib/api.js';
  import '../app.css';

  let token = session().token;
  $: session(token);

  function setToken(e) {
    token = e.currentTarget.value.trim();
  }
  function clearToken() {
    token = '';
    session(null);
  }
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
          on:input={setToken}
          aria-label="API token"
        />
      {/if}
      <button class="theme" onclick={toggleTheme} title="toggle theme" aria-label="toggle theme">◐</button>
    </div>
  </header>

  <main>
    <slot />
  </main>
</div>

<script context="module">
  function toggleTheme() {
    const t = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = t;
    localStorage.setItem('stonewall.theme', t);
  }
  if (typeof localStorage !== 'undefined') {
    const saved = localStorage.getItem('stonewall.theme');
    if (saved) document.documentElement.dataset.theme = saved;
  }
</script>

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
