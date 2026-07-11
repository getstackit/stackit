// Package main replays deterministic command scenarios through Stackit's real TUIs.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/output"
)

func main() {
	flags := flag.NewFlagSet("st-tui", flag.ExitOnError)
	delay := flags.Duration("delay", 700*time.Millisecond, "delay between action events")
	flags.Usage = func() { printUsage(flags.Output()) }
	if err := flags.Parse(os.Args[1:]); err != nil {
		return
	}

	args := flags.Args()
	if len(args) != 2 || args[0] != "sync" {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	scenario, found := stack.LookupSyncScenario(args[1])
	if !found {
		_, _ = fmt.Fprintf(os.Stderr, "unknown sync scenario %q\n\n", args[1])
		printUsage(os.Stderr)
		os.Exit(2)
	}

	out := output.NewDefaultOutput()
	runner, handler := stack.NewSyncUI(out, output.NewNullLogger())
	if runner != nil {
		defer runner.Cleanup()
	}
	scenario.Replay(handler, *delay)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: st-tui [--delay duration] sync <scenario>")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Sync scenarios:")
	for _, name := range stack.SyncScenarioNames() {
		scenario, _ := stack.LookupSyncScenario(name)
		_, _ = fmt.Fprintf(w, "  %-10s %s\n", name, scenario.Description)
	}
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Use --delay 0 to replay a scenario without pauses.")
}
