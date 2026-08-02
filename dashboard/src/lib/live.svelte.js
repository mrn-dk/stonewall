// live.svelte.js — keeping a view current without the operator reloading it.
//
// Two rules make a background refresh safe, and both are easy to get wrong:
//
//   1. It must never overlap itself. A slow response plus a short interval
//      otherwise queues requests faster than they drain.
//   2. It must stop when nobody is looking. A backgrounded tab that keeps
//      polling is a tab that quietly burns the operator's battery and the
//      node's request budget for nothing.
//
// What a refresh *does* with its result is the caller's business — see the
// callers for the third rule, that a background refresh must not disturb what
// is already on screen.

/**
 * Runs `tick` on an interval while the document is visible. Returns a stop
 * function; intended to be called from `$effect` so it unwinds on unmount.
 *
 * @param {() => Promise<void> | void} tick
 * @param {number} intervalMs
 */
export function startPolling(tick, intervalMs) {
  let timer = null;
  let running = false;
  let stopped = false;

  async function run() {
    // Skip this beat rather than stacking on top of a request still in flight.
    if (running || stopped || document.hidden) return;
    running = true;
    try {
      await tick();
    } finally {
      running = false;
    }
  }

  function start() {
    if (timer !== null || stopped) return;
    timer = setInterval(run, intervalMs);
  }

  function pause() {
    if (timer === null) return;
    clearInterval(timer);
    timer = null;
  }

  function onVisibility() {
    if (document.hidden) {
      pause();
    } else {
      // Catch up immediately on return; a tab brought forward should not show
      // stale data for another whole interval.
      start();
      run();
    }
  }

  document.addEventListener('visibilitychange', onVisibility);
  if (!document.hidden) start();

  return () => {
    stopped = true;
    pause();
    document.removeEventListener('visibilitychange', onVisibility);
  };
}

/**
 * Coalesces a burst of triggers into one call, trailing-edge. Used where many
 * events arrive at once (a run emitting several turns) but only one refetch is
 * warranted.
 *
 * @param {() => void} fn
 * @param {number} waitMs
 */
export function coalesce(fn, waitMs) {
  let timer;
  const wrapped = () => {
    clearTimeout(timer);
    timer = setTimeout(fn, waitMs);
  };
  wrapped.cancel = () => clearTimeout(timer);
  return wrapped;
}
