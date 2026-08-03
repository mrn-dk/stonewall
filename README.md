# stonewall

Distributed orchestration for durable WASM agents.

Stonewall is the control plane that runs **Latigo** — an agent that lives inside
a WASM sandbox (WASIX) — across a fleet of host nodes. It owns everything outside
a single agent: scheduling, the durable event log, content-addressed workspaces,
capability grants, crash recovery, and the control-plane API you drive it from.

## The idea

- **Isolation comes from the runtime, not the agent.** Latigo is an ordinary
  WASIX program; it does not implement its own sandbox. Stonewall runs it under a
  WASIX-capable runtime (Wasmer) and grants it capabilities — filesystem,
  network, commands — per instance. Anything not granted does not exist from
  inside the sandbox.
- **State lives outside the instance.** An agent's conversation and workspace are
  kept in a durable log and a content-addressed volume, so instances are
  disposable. An agent is a log plus a workspace; when it is idle it costs
  nothing, and when its host dies it resumes elsewhere from its last durable
  turn.
- **Agents survive crashes and migrate freely.** The event log is append-only and
  write-ahead; workspaces are checkpointed at turn boundaries. Resume loads the
  transcript, restores the workspace, and continues — on any host with the same
  image.
- **Forking is structural, not copied.** Because state is external, a fork is a
  parent pointer in the log plus a copy-on-write view of the workspace — no
  memory snapshot. Agents form a DAG with shared history, so setup is paid once
  and reused across thousands of task forks.
- **One surface, one perimeter.** Everything the agent can do reaches it through
  a shell with a command allow-list; everything that leaves the sandbox passes
  one governed egress perimeter.

## Quick start

```
make build
./bin/stonewall serve
```

Create an agent, send it a message, and follow its work:

```
curl :8080/v1/agents -d '{
  "image": "acme/agent-host:1.4",
  "goal": "summarise the repo",
  "model": "gpt-4o",
  "grants": {"fs": {"/workspace": "rw"}, "net": ["mortise.internal"], "cmd": ["rg"]}
}'

curl :8080/v1/agents/<id>/messages  -d '{"body": "go"}'
curl -N :8080/v1/agents/<id>/events
```

Fork an agent at a turn boundary to explore alternatives without losing the
original, or restore it to an earlier checkpoint:

```
curl :8080/v1/agents/<id>/fork     -d '{"at_turn": 12}'
curl :8080/v1/agents/<id>/restore  -d '{"checkpoint_id": "<ckpt>"}'
```

## Dashboard

`make build` compiles the operator dashboard into the same binary, so opening
`http://localhost:8080/` serves it — no separate static host, and no request
leaves the origin, so it works on an isolated network.

It is an ordinary client of the API above, with no privileged path. The landing
page is a light fleet overview (counts, node resources, a cursor-paged table
with server-side state and text filtering). The depth is in the **workbench** at
`/agents/<id>`: a timeline, the conversation transcript, and the workspace as it
existed at a chosen turn, all keyed by turn — plus send-input, cancel, fork,
restore, checkpoint, and delete. Press `⌘K` / `Ctrl-K` for the command palette.

`go build ./...` without the `dashboard` tag omits the UI; the `/dashboard`
handler then returns 503 and the API is unaffected.

Working on the dashboard itself:

```
cd dashboard
npm install
npm run dev        # vite dev server, proxying /v1 to localhost:8080
npm run check      # svelte-check
npm run contrast   # WCAG AA audit of the design tokens
```

## Components

| Project | Role |
|---|---|
| **Mortise** | OpenAI-compatible AI gateway fronting inference fleets |
| **Latigo** | The agent — a WASIX program run inside the sandbox |
| **Stonewall** | Orchestrator: schedules Latigo across hosts, owns durability |

## License

MIT.
