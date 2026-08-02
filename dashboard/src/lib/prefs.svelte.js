// prefs.svelte.js — small view preferences that should survive a reload.
//
// One owner per localStorage key is the rule here: theme lives in
// theme.svelte.js, the token in session.svelte.js, and view preferences here.
// Components read these; they do not touch localStorage themselves.

const KEY = 'stonewall.prefs';

function load() {
  if (typeof localStorage === 'undefined') return {};
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '{}') ?? {};
  } catch {
    return {};
  }
}

class Prefs {
  #saved = load();

  /** 'chat' reads the transcript as a conversation; 'events' shows the log. */
  transcriptView = $state(this.#saved.transcriptView === 'events' ? 'events' : 'chat');

  /** @param {'chat' | 'events'} view */
  setTranscriptView(view) {
    this.transcriptView = view;
    this.#persist();
  }

  #persist() {
    if (typeof localStorage === 'undefined') return;
    try {
      localStorage.setItem(KEY, JSON.stringify({ transcriptView: this.transcriptView }));
    } catch {
      // A full or blocked storage is not a reason to break the view.
    }
  }
}

export const prefs = new Prefs();
