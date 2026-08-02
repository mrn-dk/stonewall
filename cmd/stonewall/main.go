// Command stonewall is the single-node orchestrator.
//
// It runs the control-plane API and the node agent (scheduler + runtime) in one
// process. A real deployment separates API and node roles; the single binary is
// the build-order step-3 shape (spec §4, build order 3).
//
// Subcommands:
//
//	serve   run the control plane and node scheduler together (default)
//	once    run a single activation for an agent by id, then exit
//	migrate open (create) the data store and exit
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mrn-dk/stonewall/internal/api"
	"github.com/mrn-dk/stonewall/internal/config"
	"github.com/mrn-dk/stonewall/internal/node"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "once":
		cmdOnce(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: stonewall {serve|once|migrate} [flags]")
}

func mustConfig() *config.Config {
	c, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		log.Fatal(err)
	}
	return c
}

func mustStore(c *config.Config) *store.Store {
	s, err := store.Open(c.DataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	return s
}

func makeRuntime(c *config.Config) runtime.Runtime {
	switch c.Runtime {
	case "wasmer":
		return &runtime.WasmerRuntime{WasmerBinary: c.WasmerBin, ImageRoot: c.ImageRoot}
	default:
		return &runtime.MockRuntime{}
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	_ = fs.Parse(args)
	c := mustConfig()
	s := mustStore(c)
	defer s.Close()
	rt := makeRuntime(c)
	n := node.New(s, rt, c.ToolsDir, c.HTTPAddr, c.Node)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := n.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("node scheduler stopped: %v", err)
		}
	}()

	srv := api.New(c.HTTPAddr, s, n)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("http: %v", err)
		}
	}()
	<-ctx.Done()
	log.Println("shutting down...")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

func cmdOnce(args []string) {
	fs := flag.NewFlagSet("once", flag.ExitOnError)
	agentID := fs.String("agent", "", "agent id to run one activation for")
	_ = fs.Parse(args)
	if *agentID == "" {
		fmt.Fprintln(os.Stderr, "once: -agent is required")
		os.Exit(2)
	}
	c := mustConfig()
	s := mustStore(c)
	defer s.Close()
	rt := makeRuntime(c)
	n := node.New(s, rt, c.ToolsDir, c.HTTPAddr, c.Node)
	reason, err := n.RunActivation(context.Background(), *agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "activation ended: %s: %v\n", reason, err)
		os.Exit(1)
	}
	fmt.Printf("activation ended: %s\n", reason)
}

func cmdMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	_ = fs.Parse(args)
	c := mustConfig()
	s := mustStore(c)
	defer s.Close()
	fmt.Println("migrations applied")
}
