<script>
  // The conversation, in one of two readings of the same events.
  //
  //   chat   — folded back into an exchange, message content as Markdown.
  //   events — the durable log as recorded, one row per event.
  //
  // Neither is a summary: chat is a lens, and anything it declines to
  // interpret is still there in the events view. Keeping both is what lets the
  // transcript be readable without becoming a thing you cannot audit.
  //
  // Note on escaping: there is no {@html} in either view. Message content goes
  // through the token-tree renderer in $lib/markdown.js, so agent-controlled
  // text reaches the DOM only as text nodes.
  import * as Collapsible from '$lib/components/ui/collapsible/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import ChatTranscript from './chat-transcript.svelte';
  import { prefs } from '$lib/prefs.svelte.js';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import MessageSquare from '@lucide/svelte/icons/message-square';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import Terminal from '@lucide/svelte/icons/terminal';
  import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
  import FilePen from '@lucide/svelte/icons/file-pen';
  import MessagesSquare from '@lucide/svelte/icons/messages-square';
  import List from '@lucide/svelte/icons/list';

  let { events, selectedTurn } = $props();

  const PREVIEW_LIMIT = 600;

  const kinds = {
    message: { icon: MessageSquare, tone: 'text-foreground' },
    llm_call: { icon: Sparkles, tone: 'text-state-terminal' },
    tool_intent: { icon: Terminal, tone: 'text-state-pending' },
    tool_result: { icon: CornerDownLeft, tone: 'text-muted-foreground' },
    workspace_modified: { icon: FilePen, tone: 'text-state-running' }
  };

  function asMessage(payload) {
    let m = payload;
    if (typeof m === 'string') {
      try {
        m = JSON.parse(m);
      } catch {
        return { role: '', content: payload };
      }
    }
    const content = m?.content ?? m?.body ?? '';
    return {
      role: m?.role ?? '',
      content: typeof content === 'string' ? content : JSON.stringify(content, null, 2)
    };
  }

  function asText(payload) {
    if (payload === undefined || payload === null) return '';
    if (typeof payload === 'string') return payload;
    try {
      return JSON.stringify(payload, null, 2);
    } catch {
      return String(payload);
    }
  }

  const visible = $derived(
    events.filter((e) => selectedTurn == null || (e.turn ?? 0) <= selectedTurn)
  );

  const views = [
    { id: 'chat', label: 'Chat', icon: MessagesSquare },
    { id: 'events', label: 'Events', icon: List }
  ];
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex items-center gap-2 px-1 pb-1.5">
    <h2 class="text-muted-foreground text-xs font-medium tracking-wide uppercase">
      Transcript
      {#if selectedTurn != null}
        <span class="normal-case">· through turn {selectedTurn}</span>
      {/if}
    </h2>

    <div
      class="bg-muted/60 ml-auto flex shrink-0 items-center gap-0.5 rounded-md border p-0.5"
      role="group"
      aria-label="Transcript view"
    >
      {#each views as view (view.id)}
        {@const active = prefs.transcriptView === view.id}
        {@const Icon = view.icon}
        <Button
          variant={active ? 'secondary' : 'ghost'}
          size="xs"
          class="h-5 gap-1 px-1.5 {active ? '' : 'text-muted-foreground'}"
          aria-pressed={active}
          onclick={() => prefs.setTranscriptView(view.id)}
        >
          <Icon class="size-3" />
          {view.label}
        </Button>
      {/each}
    </div>
  </div>

  {#if prefs.transcriptView === 'chat'}
    <ChatTranscript {events} {selectedTurn} />
  {:else}
    <div class="scrollbar-thin min-h-0 flex-1 space-y-1 overflow-y-auto px-0.5 pb-1">
      {#each visible as e (e.seq)}
        {@const meta = kinds[e.kind] ?? { icon: MessageSquare, tone: 'text-muted-foreground' }}
        {@const Icon = meta.icon}
        {@const msg = e.kind === 'message' ? asMessage(e.payload) : null}
        {@const body = msg ? msg.content : asText(e.payload)}
        {@const long = body.length > PREVIEW_LIMIT}

        <article class="rounded-md border p-2">
          <header class="flex items-baseline gap-1.5 text-xs">
            <Icon class="size-3 shrink-0 self-center {meta.tone}" />
            <span class="font-medium">{e.kind}</span>
            {#if msg?.role}
              <span class="text-muted-foreground">· {msg.role}</span>
            {/if}
            <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">
              seq {e.seq} · turn {e.turn}
            </span>
          </header>

          {#if body}
            {#if long}
              <Collapsible.Root>
                <pre
                  class="text-foreground mt-1 font-mono text-xs break-words whitespace-pre-wrap">{body.slice(
                    0,
                    PREVIEW_LIMIT
                  )}…</pre>
                <Collapsible.Content>
                  <pre
                    class="text-foreground mt-1 font-mono text-xs break-words whitespace-pre-wrap">{body.slice(
                      PREVIEW_LIMIT
                    )}</pre>
                </Collapsible.Content>
                <Collapsible.Trigger>
                  {#snippet child({ props })}
                    <Button {...props} variant="ghost" size="xs" class="text-muted-foreground mt-1">
                      <ChevronRight data-icon="inline-start" />
                      Show all {body.length.toLocaleString()} characters
                    </Button>
                  {/snippet}
                </Collapsible.Trigger>
              </Collapsible.Root>
            {:else}
              <pre
                class="text-foreground mt-1 font-mono text-xs break-words whitespace-pre-wrap">{body}</pre>
            {/if}
          {/if}
        </article>
      {:else}
        <p class="text-muted-foreground px-1.5 py-3 text-sm">No conversation events yet.</p>
      {/each}
    </div>
  {/if}
</div>
