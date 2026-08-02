package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mrn-dk/stonewall/internal/model"
)

// WasmerRuntime invokes the wasmer CLI to run a Latigo WASIX instance. It
// applies granted capabilities as runtime flags and never runs anything the
// grant does not allow.
//
// Binary discovery: defaults to "wasmer" on PATH; overridden by WasmerBinary
// or the WASMER_BINARY env var.
type WasmerRuntime struct {
	WasmerBinary string
	// ImageRoot maps an image digest to a directory containing the unpacked
	// WASIX image (latigo.wasm plus /tools). A real deployment pulls images by
	// digest into this root; the node agent resolves the path before Run.
	ImageRoot string
}

func (w *WasmerRuntime) Name() string { return "wasmer" }

func (w *WasmerRuntime) binaryPath() string {
	if w.WasmerBinary != "" {
		return w.WasmerBinary
	}
	if env := os.Getenv("WASMER_BINARY"); env != "" {
		return env
	}
	return "wasmer"
}

// Run shells out to wasmer. It blocks until the instance exits or the context
// is cancelled. Event ingestion is handled out-of-band by the node's ingest
// server (Latigo posts events to ControlEndpoint); this function only supervises
// the process and translates exit/cancellation into an end reason.
func (w *WasmerRuntime) Run(ctx context.Context, spec InstanceSpec) (string, error) {
	wasmPath := w.resolveWasm(spec)
	if _, err := os.Stat(wasmPath); err != nil {
		return model.EndCrashed, fmt.Errorf("wasmer: guest wasm not found at %s: %w", wasmPath, err)
	}

	args := []string{"run", wasmPath}
	// Filesystem grants: preopen directories. /workspace is the agent volume;
	// /tools is read-only from the image.
	if spec.WorkspaceDir != "" {
		args = append(args, "--dir", spec.WorkspaceDir)
	}
	if spec.ToolsDir != "" {
		args = append(args, "--dir", spec.ToolsDir+":/tools:ro")
	}
	// Network grants: WASIX networking is enabled via --net with an allow-list.
	// Wasmer's WASIX net model grants network access; fine-grained endpoint
	// allow-listing is enforced by the egress perimeter (control plane) and by
	// only injecting credentials for approved endpoints. An empty net grant
	// omits --net entirely, giving the instance no network.
	if hasNetGrant(spec) {
		args = append(args, "--net")
	}

	cmd := exec.CommandContext(ctx, w.binaryPath(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(spec)

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return model.EndCancelled, ctx.Err()
		}
		if _, ok := err.(*exec.ExitError); ok {
			return model.EndCrashed, fmt.Errorf("wasmer: guest exited with error: %w", err)
		}
		return model.EndCrashed, fmt.Errorf("wasmer: run failed: %w", err)
	}
	return model.EndCompleted, nil
}

// resolveWasm finds the guest wasm for the image digest.
func (w *WasmerRuntime) resolveWasm(spec InstanceSpec) string {
	if w.ImageRoot != "" && spec.ImageDigest != "" {
		return filepath.Join(w.ImageRoot, spec.ImageDigest, "latigo.wasm")
	}
	if p := os.Getenv("LATIGO_WASM"); p != "" {
		return p
	}
	return "latigo.wasm"
}

func hasNetGrant(spec InstanceSpec) bool {
	g, _ := spec.Grants["net"].([]string)
	return len(g) > 0
}

// buildEnv assembles the LATIGO_* configuration env vars injected into the
// sandbox. Secrets are never injected (the egress perimeter holds them).
func buildEnv(spec InstanceSpec) []string {
	env := os.Environ()
	set := func(k, v string) {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	set("LATIGO_GOAL", spec.Goal)
	set("LATIGO_MODEL", spec.Model)
	if spec.MaxTurns > 0 {
		set("LATIGO_MAX_TURNS", fmt.Sprintf("%d", spec.MaxTurns))
	}
	set("LATIGO_CONTROL_ENDPOINT", spec.ControlEndpoint)
	set("LATIGO_AGENT_ID", spec.AgentID)
	set("LATIGO_ACTIVATION_ID", spec.ActivationID)
	// Command allow-list is communicated so the in-sandbox shell can enforce it;
	// the security boundary remains the sandbox (spec §2.4).
	if cmds, ok := spec.Grants["cmd"].([]string); ok {
		set("LATIGO_CMD_ALLOWLIST", strings.Join(cmds, ","))
	}
	for k, v := range spec.Env {
		set(k, v)
	}
	return env
}
