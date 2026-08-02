// session.svelte.js — what the dashboard actually knows about its credentials.
//
// The honest position, which the UI is required to reflect (spec: "Honest
// session and authorization surface"): this server does not authenticate. A
// token is stored and sent as `Authorization: Bearer`, and nothing verifies
// it. So there are three states, and "authenticated" is not one of them:
//
//   'anonymous'  no token stored; requests go out unauthenticated
//   'unverified' a token is stored and sent, but no server has confirmed it
//   'refused'    an intervention came back 403, so intervention controls are
//                hidden — inferred from a refusal, not from a declared scope
//
// The distinction matters at the UI edge: 'refused' means "a request was
// refused", not "your credential is read-only". We do not know the latter.

const KEY = 'stonewall.token';

function stored() {
  if (typeof localStorage === 'undefined') return '';
  return localStorage.getItem(KEY) || '';
}

class Session {
  token = $state(stored());
  /** Set when an intervention is refused; null while nothing has been refused. */
  refusal = $state(/** @type {{ at: Date, action: string } | null} */ (null));

  /** @returns {'anonymous' | 'unverified' | 'refused'} */
  get status() {
    if (this.refusal) return 'refused';
    return this.token ? 'unverified' : 'anonymous';
  }

  /**
   * Whether intervention controls should be rendered at all. Controls are
   * hidden rather than disabled, so this gates rendering, not a `disabled`
   * attribute.
   */
  get canIntervene() {
    return this.refusal === null;
  }

  /** @param {string} token */
  set(token) {
    this.token = token.trim();
    if (typeof localStorage === 'undefined') return;
    if (this.token) localStorage.setItem(KEY, this.token);
    else localStorage.removeItem(KEY);
  }

  clear() {
    this.set('');
    // A new credential deserves a fresh verdict: whatever was refused was
    // refused for the previous one.
    this.refusal = null;
  }

  /**
   * Record that the server refused an intervention. Called from the 403 path,
   * never guessed ahead of time.
   * @param {string} action
   */
  noteRefusal(action) {
    this.refusal = { at: new Date(), action };
  }
}

export const session = new Session();

/** Read-only accessor for the transport layer in api.js. */
export function authToken() {
  return session.token;
}
