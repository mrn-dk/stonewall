<script>
  // The transcript as a conversation.
  //
  // The event log is a record, not a reading experience: a single exchange
  // arrives as an llm_call, a tool_intent, a tool_result, and a
  // workspace_modified, all as sibling rows. This view folds that back into
  // what actually happened — a message, a tool call with its result attached,
  // a note that a file changed — and renders message content as Markdown.
  //
  // It is a lens over the same events, not a second source. Nothing is
  // invented, and anything it cannot interpret stays visible in the raw view.
  import * as Collapsible from '$lib/components/ui/collapsible/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import Markdown from '$lib/components/markdown.svelte';
  import { timeOfDay } from '$lib/agents.js';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Terminal from '@lucide/svelte/icons/terminal';
  import FilePen from '@lucide/svelte/icons/file-pen';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import User from '@lucide/svelte/icons/user';

  let { events, selectedTurn } = $props();

  const visible = $derived(
    events.filter((e) => selectedTurn == null || (e.turn ?? 0) <= selectedTurn)
  );

  function messageOf(payload) {
    let m = payload;
    if (typeof m === 'string') {
      try {
        m = JSON.parse(m);
      } catch {
        return { role: 'assistant', content: payload };
      }
    }
    const content = m?.content ?? m?.body ?? '';
    return {
      role: m?.role || 'assistant',
      content: typeof content === 'string' ? content : JSON.stringify(content, null, 2)
    };
  }

  const commandOf = (p) => [p?.cmd, ...(p?.args ?? [])].filter(Boolean).join(' ');

  // Fold the event stream into conversational items, pairing each tool_intent
  // with the tool_result that answers it.
  const items = $derived.by(() => {
    const out = [];
    /** @type {Map<string, object>} */
    const pendingTools = new Map();

    for (const e of visible) {
      switch (e.kind) {
        case 'message': {
          const { role, content } = messageOf(e.payload);
          out.push({ type: 'message', key: e.seq, role, content, turn: e.turn, at: e.occurred_at });
          break;
        }
        case 'llm_call': {
          const { content } = messageOf(e.payload);
          // The mock records only token counts; a real model call may carry
          // content. Render whichever is actually there rather than pretending.
          out.push({
            type: 'llm',
            key: e.seq,
            content,
            model: e.payload?.model,
            promptTokens: e.payload?.prompt_tokens,
            completionTokens: e.payload?.completion_tokens,
            turn: e.turn,
            at: e.occurred_at
          });
          break;
        }
        case 'tool_intent': {
          const item = {
            type: 'tool',
            key: e.seq,
            command: commandOf(e.payload),
            payload: e.payload,
            result: null,
            turn: e.turn,
            at: e.occurred_at
          };
          // Pair on the idempotency key the log already carries; fall back to
          // "the most recent unanswered call" when there is none.
          pendingTools.set(e.idempotency_key ?? `seq-${e.seq}`, item);
          out.push(item);
          break;
        }
        case 'tool_result': {
          const key = e.idempotency_key ?? '';
          let target = pendingTools.get(key);
          if (!target) {
            target = [...pendingTools.values()].reverse().find((t) => !t.result);
          }
          if (target) {
            target.result = e.payload;
            pendingTools.delete(key);
          } else {
            out.push({
              type: 'tool',
              key: e.seq,
              command: '(result with no matching call)',
              result: e.payload,
              turn: e.turn,
              at: e.occurred_at
            });
          }
          break;
        }
        case 'workspace_modified': {
          out.push({
            type: 'file',
            key: e.seq,
            path: e.payload?.path ?? '',
            turn: e.turn,
            at: e.occurred_at
          });
          break;
        }
      }
    }
    return out;
  });

  const outputOf = (result) =>
    [result?.stdout, result?.stderr].filter(Boolean).join('\n').trimEnd();
</script>

<div class="scrollbar-thin min-h-0 flex-1 space-y-2 overflow-y-auto px-0.5 pb-1">
  {#each items as item (item.key)}
    {#if item.type === 'message'}
      {@const mine = item.role === 'user'}
      <div class="flex gap-2 {mine ? 'flex-row-reverse' : ''}">
        <div
          class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border {mine
            ? 'bg-secondary'
            : 'bg-muted'}"
          aria-hidden="true"
        >
          {#if mine}
            <User class="size-3" />
          {:else}
            <Sparkles class="text-state-terminal size-3" />
          {/if}
        </div>
        <div class="min-w-0 max-w-[85%] {mine ? 'text-right' : ''}">
          <div
            class="rounded-lg border px-2.5 py-1.5 text-left {mine
              ? 'bg-secondary/60'
              : 'bg-card'}"
          >
            <Markdown source={item.content} />
          </div>
          <p class="text-muted-foreground mt-0.5 text-[0.7rem] tabular-nums">
            {item.role} · turn {item.turn} · {timeOfDay(item.at)}
          </p>
        </div>
      </div>
    {:else if item.type === 'llm'}
      <div class="flex gap-2">
        <div
          class="bg-muted mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border"
          aria-hidden="true"
        >
          <Sparkles class="text-state-terminal size-3" />
        </div>
        <div class="min-w-0 flex-1">
          {#if item.content}
            <div class="bg-card rounded-lg border px-2.5 py-1.5">
              <Markdown source={item.content} />
            </div>
          {:else}
            <p class="text-muted-foreground text-xs italic">
              model call — no content recorded in the log
            </p>
          {/if}
          <p class="text-muted-foreground mt-0.5 text-[0.7rem] tabular-nums">
            {item.model ?? 'model'}
            {#if item.promptTokens != null}
              · {item.promptTokens}→{item.completionTokens} tokens
            {/if}
            · turn {item.turn} · {timeOfDay(item.at)}
          </p>
        </div>
      </div>
    {:else if item.type === 'tool'}
      {@const output = outputOf(item.result)}
      {@const failed = item.result && item.result.exit_code !== 0}
      <div class="ml-7">
        <Collapsible.Root>
          <div
            class="rounded-lg border {failed ? 'border-state-failed/40' : ''} overflow-hidden"
          >
            <div class="bg-muted/40 flex items-center gap-1.5 px-2 py-1">
              <Terminal class="text-muted-foreground size-3 shrink-0" />
              <code class="truncate font-mono text-xs">{item.command}</code>
              {#if item.result}
                <span
                  class="ml-auto shrink-0 font-mono text-[0.7rem] {failed
                    ? 'text-state-failed'
                    : 'text-state-running'}"
                >
                  exit {item.result.exit_code}
                </span>
              {:else}
                <span class="text-muted-foreground ml-auto shrink-0 text-[0.7rem]">running…</span>
              {/if}
            </div>
            {#if output}
              <Collapsible.Content>
                <pre
                  class="scrollbar-thin max-h-56 overflow-auto border-t p-2 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">{output}</pre>
              </Collapsible.Content>
              <Collapsible.Trigger>
                {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="ghost"
                    size="xs"
                    class="text-muted-foreground w-full justify-start rounded-none border-t"
                  >
                    <ChevronRight data-icon="inline-start" />
                    output
                  </Button>
                {/snippet}
              </Collapsible.Trigger>
            {/if}
          </div>
        </Collapsible.Root>
      </div>
    {:else if item.type === 'file'}
      <div class="text-muted-foreground ml-7 flex items-center gap-1.5 text-xs">
        <FilePen class="text-state-running size-3 shrink-0" />
        wrote <code class="bg-muted rounded px-1 font-mono">{item.path}</code>
      </div>
    {/if}
  {:else}
    <p class="text-muted-foreground px-1.5 py-3 text-sm">No conversation events yet.</p>
  {/each}
</div>
