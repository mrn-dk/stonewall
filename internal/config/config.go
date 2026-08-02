// Package config loads Stonewall's configuration from environment and flags.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mrn-dk/stonewall/internal/node"
)

// Config is the top-level runtime configuration.
type Config struct {
	DataDir   string
	HTTPAddr  string
	Runtime   string // "mock" (default) | "wasmer"
	WasmerBin string
	ImageRoot string // wasmer: directory of unpacked images by digest
	ToolsDir  string // ro tools mount for the image (optional)
	MaxTurns  int

	Node node.Config
}

// FromEnv builds configuration from the environment, applying defaults.
func FromEnv() (*Config, error) {
	c := &Config{
		DataDir:   envStr("STONEWALL_DATA_DIR", "./.stonewall-data"),
		HTTPAddr:  envStr("STONEWALL_HTTP_ADDR", ":8080"),
		Runtime:   envStr("STONEWALL_RUNTIME", "mock"),
		WasmerBin: envStr("WASMER_BINARY", "wasmer"),
		ImageRoot: envStr("STONEWALL_IMAGE_ROOT", ""),
		ToolsDir:  envStr("STONEWALL_TOOLS_DIR", ""),
		MaxTurns:  envInt("STONEWALL_MAX_TURNS", 25),
	}
	c.Node.MaxConcurrent = envInt("STONEWALL_MAX_CONCURRENT", 4)
	c.Node.PollInterval = envDur("STONEWALL_POLL_INTERVAL", 500*time.Millisecond)
	c.Node.CheckpointIntervalTurns = envInt("STONEWALL_CHECKPOINT_INTERVAL_TURNS", 5)
	c.Node.CrashThreshold = envInt("STONEWALL_CRASH_THRESHOLD", 5)
	c.Node.NodeBreakerThreshold = envInt("STONEWALL_NODE_BREAKER_THRESHOLD", 10)
	c.Node.NodeBreakerWindow = envDur("STONEWALL_NODE_BREAKER_WINDOW", 5*time.Minute)
	c.Node.NodeBreakerCooldown = envDur("STONEWALL_NODE_BREAKER_COOLDOWN", 30*time.Second)
	c.Node.ActivationTimeout = envDur("STONEWALL_ACTIVATION_TIMEOUT", 10*time.Minute)
	return c, nil
}

// Validate checks the configuration is coherent.
func (c *Config) Validate() error {
	switch c.Runtime {
	case "mock", "wasmer":
	default:
		return fmt.Errorf("unknown runtime %q (want mock|wasmer)", c.Runtime)
	}
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	return nil
}

func envStr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}
func envInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func envDur(k string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(k); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
