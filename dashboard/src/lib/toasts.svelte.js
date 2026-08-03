// toasts.svelte.js — the outcome channel.
//
// Every intervention reports an explicit outcome here (spec: "Intervention
// with explicit outcomes"), so an operation never completes silently. Failures
// carry the request identifier the API returned and a retry action, and they
// do NOT auto-dismiss: a request id you cannot read is a request id you cannot
// quote in a bug report.

let nextId = 0;

class Toasts {
  items = $state(
    /** @type {Array<{
     *   id: number,
     *   variant: 'success' | 'error' | 'info',
     *   title: string,
     *   description?: string,
     *   requestId?: string,
     *   href?: string,
     *   hrefLabel?: string,
     *   retry?: () => void
     * }>} */ ([])
  );

  /** @param {Omit<Toasts['items'][number], 'id'>} toast */
  push(toast) {
    const id = ++nextId;
    this.items = [...this.items, { ...toast, id }];
    // Successes are transient; failures stay until dismissed.
    if (toast.variant === 'success' || toast.variant === 'info') {
      setTimeout(() => this.dismiss(id), 5000);
    }
    return id;
  }

  /** @param {number} id */
  dismiss(id) {
    this.items = this.items.filter((t) => t.id !== id);
  }

  /**
   * @param {string} title
   * @param {{ description?: string, href?: string, hrefLabel?: string }} [opts]
   */
  success(title, opts = {}) {
    return this.push({ variant: 'success', title, ...opts });
  }

  /**
   * @param {string} title
   * @param {unknown} err
   * @param {{ retry?: () => void }} [opts]
   */
  error(title, err, opts = {}) {
    const e = /** @type {{ message?: string, request_id?: string }} */ (err ?? {});
    return this.push({
      variant: 'error',
      title,
      description: e.message,
      requestId: e.request_id,
      retry: opts.retry
    });
  }
}

export const toasts = new Toasts();
