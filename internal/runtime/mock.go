package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
)

// MockRuntime is an in-process simulation of a Latigo activation. It does not
// run WASM; it stands in for the harness boundary and emits a realistic
// event-log sequence through the EventSink, writes workspace files (so
// checkpointing is exercised), and honors cancellation and turn budgets.
//
// Behaviour is steered by Env keys so tests are deterministic:
//
//	MOCK_TURNS         number of turns to run (default: spec.MaxTurns, else 3)
//	MOCK_WRITE_FILES   "1" writes a file per turn (default "1")
//	MOCK_CRASH_TURN    turn at which to simulate a crash (non-zero exit)
//	MOCK_HANG_TURN     turn at which to block until cancelled
//	MOCK_NO_TOOLS      "1" skips tool intent/result events
//
// It fsyncs workspace writes before emitting the turn-boundary event, so the
// node's checkpoint-at-turn-boundary snapshot is consistent.
type MockRuntime struct{}

func (m *MockRuntime) Name() string { return "mock" }

func envGet(env map[string]string, k string) string {
	if v, ok := env[k]; ok {
		return v
	}
	return os.Getenv(k)
}

func (m *MockRuntime) Run(ctx context.Context, spec InstanceSpec) (string, error) {
	sink := spec.EventSink
	if sink == nil {
		return model.EndCrashed, fmt.Errorf("mock: no event sink")
	}
	turns := 3
	if spec.MaxTurns > 0 {
		turns = spec.MaxTurns
	}
	if v := envGet(spec.Env, "MOCK_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			turns = n
		}
	}
	writeFiles := envGet(spec.Env, "MOCK_WRITE_FILES") != "0"
	noTools := envGet(spec.Env, "MOCK_NO_TOOLS") == "1"
	crashTurn, _ := strconv.Atoi(envGet(spec.Env, "MOCK_CRASH_TURN"))
	hangTurn, _ := strconv.Atoi(envGet(spec.Env, "MOCK_HANG_TURN"))

	// run_start
	if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventRunStart, 0, "", map[string]any{
		"model": spec.Model,
		"goal":  spec.Goal,
		"image": spec.ImageDigest,
	}); err != nil {
		return model.EndCrashed, err
	}

	for turn := 1; turn <= turns; turn++ {
		if err := ctx.Err(); err != nil {
			m.emitEnd(sink, spec, turn-1, model.EndCancelled)
			return model.EndCancelled, err
		}
		if hangTurn > 0 && turn == hangTurn {
			m.emitEnd(sink, spec, turn-1, model.EndCrashed)
			<-ctx.Done()
			return model.EndCancelled, ctx.Err()
		}
		if crashTurn > 0 && turn == crashTurn {
			// Simulate a mid-turn crash: record intent, then die without a result.
			_, _ = sink.Append(spec.AgentID, spec.ActivationID, model.EventToolIntent, turn, mockKey(turn), map[string]any{
				"cmd": "rg", "args": []string{"foo"},
			})
			return model.EndCrashed, fmt.Errorf("mock: simulated crash at turn %d", turn)
		}

		// Model call.
		if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventLLMCall, turn, "", map[string]any{
			"model":             spec.Model,
			"turn":              turn,
			"prompt_tokens":     turn * 10,
			"completion_tokens": turn * 5,
		}); err != nil {
			return model.EndCrashed, err
		}

		// Tool use (governed: recorded intent before dispatch, then result).
		if !noTools {
			idem := mockKey(turn)
			if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventToolIntent, turn, idem, map[string]any{
				"cmd": "rg", "args": []string{"-n", "TODO", "src"},
			}); err != nil {
				return model.EndCrashed, err
			}
			if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventToolResult, turn, idem, map[string]any{
				"exit_code": 0, "stdout": "src/main.go:10:TODO",
			}); err != nil {
				return model.EndCrashed, err
			}
		}

		// Workspace modification: write a file and fsync before turn boundary.
		if writeFiles && spec.WorkspaceDir != "" {
			if err := m.writeWorkspaceFile(spec.WorkspaceDir, turn); err != nil {
				return model.EndCrashed, err
			}
			if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventWorkspaceMod, turn, "", map[string]any{
				"path": fmt.Sprintf("turn_%d.txt", turn),
			}); err != nil {
				return model.EndCrashed, err
			}
		}

		// Turn boundary: emitted only after workspace writes are fsync'd, so the
		// node's checkpoint-at-boundary snapshot is consistent.
		if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventTurnBoundary, turn, "", map[string]any{
			"turn": turn,
		}); err != nil {
			return model.EndCrashed, err
		}
	}

	final := "done"
	if spec.Goal != "" {
		final = "completed: " + spec.Goal
	}
	m.emitEnd(sink, spec, turns, model.EndCompleted)
	if _, err := sink.Append(spec.AgentID, spec.ActivationID, model.EventMessage, turns, "", map[string]any{
		"role":    "assistant",
		"content": final,
	}); err != nil {
		return model.EndCrashed, err
	}
	return model.EndCompleted, nil
}

func (m *MockRuntime) emitEnd(sink EventSink, spec InstanceSpec, lastTurn int, reason string) {
	_, _ = sink.Append(spec.AgentID, spec.ActivationID, model.EventRunEnd, lastTurn, "", map[string]any{
		"reason":    reason,
		"last_turn": lastTurn,
	})
}

func (m *MockRuntime) writeWorkspaceFile(workspaceDir string, turn int) error {
	name := filepath.Join(workspaceDir, fmt.Sprintf("turn_%d.txt", turn))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "turn %d at %s\n", turn, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func mockKey(turn int) string {
	return fmt.Sprintf("mock-tool-%d", turn)
}
