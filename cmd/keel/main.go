// Keel CLI — subcommands: init, serve, status.
//
// State lives globally at ~/.keel/state.db so one daemon serves every repo
// the user works in. Each tool call carries repo_root, so there is no
// "current repo" ambiguity at the server layer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/smaan712gb/keel/internal/locks"
	keelmcp "github.com/smaan712gb/keel/internal/mcp"
	"github.com/smaan712gb/keel/internal/plans"
	"github.com/smaan712gb/keel/internal/store"
)

const usage = `keel — the coordination plane for AI coding agents

Usage:
  keel init                                      Prepare state dir and print MCP config
  keel serve                                     Run the MCP server over stdio
  keel status [--repo=PATH] [--json]             Show active locks + plans
  keel acquire <repo> <file> <agent> [flags]     Try to take a whole-file lock
  keel release <lock_id> <agent>                 Release a held lock
  keel version

Acquire flags:
  --plan=TEXT        one-sentence plan (surfaced to sibling agents)
  --eta=SECONDS      self-estimated time until release
  --lease=SECONDS    lease duration (default 300, max 3600)
  --json             print JSON instead of human-readable

Exit codes for acquire: 0 acquired, 2 busy, 1 error.

State: ~/.keel/state.db (SQLite).
Docs:  https://github.com/smaan712gb/keel
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		must(cmdInit())
	case "serve":
		must(cmdServe())
	case "status":
		must(cmdStatus(os.Args[2:]))
	case "acquire":
		os.Exit(cmdAcquire(os.Args[2:]))
	case "release":
		must(cmdRelease(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("keel %s\n", keelmcp.Version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// --- init ---

func cmdInit() error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return fmt.Errorf("init state db: %w", err)
	}
	_ = db.Close()

	exe, err := os.Executable()
	if err != nil {
		exe = "keel"
	}
	// Use forward slashes in printed configs — works on every platform.
	exe = filepath.ToSlash(exe)

	fmt.Printf("Keel state dir ready: %s\n\n", dir)
	fmt.Println("Register the MCP server with your agents:")
	fmt.Println()
	fmt.Println("  Claude Code (one-liner):")
	fmt.Printf("    claude mcp add keel -- %s serve\n\n", exe)
	fmt.Println("  Claude Code (.mcp.json, committed to repo):")
	fmt.Printf(`    {
      "mcpServers": {
        "keel": { "command": %q, "args": ["serve"] }
      }
    }
`, exe)
	fmt.Println()
	fmt.Println("  Cursor (~/.cursor/mcp.json):")
	fmt.Printf(`    {
      "mcpServers": {
        "keel": { "command": %q, "args": ["serve"] }
      }
    }
`, exe)
	fmt.Println()
	fmt.Println("Then run `keel status` from any repo to see active locks.")
	return nil
}

// --- serve ---

func cmdServe() error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer db.Close()

	lm := locks.New(db)
	ps := plans.New(db)
	srv := keelmcp.New(lm, ps)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Reaper: every 30s, mark expired leases as released. Non-fatal if it hiccups.
	go reaperLoop(ctx, lm)

	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

func reaperLoop(ctx context.Context, lm *locks.Manager) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := lm.Reap(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "reaper: %v\n", err)
			}
		}
	}
}

// --- status ---

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo root (defaults to cwd)")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	repo := *repoFlag
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repo = cwd
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}

	db, err := openState()
	if err != nil {
		return err
	}
	defer db.Close()
	dir, _ := stateDir()

	ctx := context.Background()
	ls, err := locks.New(db).ListActive(ctx, repo)
	if err != nil {
		return err
	}
	pl, err := plans.New(db).ListActive(ctx, repo, "")
	if err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{"repo_root": repo, "locks": ls, "plans": pl}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Repo: %s\n", repo)
	fmt.Printf("State: %s\n\n", filepath.Join(dir, "state.db"))

	if len(ls) == 0 {
		fmt.Println("Active locks: (none)")
	} else {
		fmt.Printf("Active locks (%d):\n", len(ls))
		for _, l := range ls {
			eta := ""
			if l.ETASeconds != nil {
				eta = fmt.Sprintf(" eta=%ds", *l.ETASeconds)
			}
			fmt.Printf("  %s  %-14s  %s%s  lease=%s  — %s\n",
				shortID(l.LockID), l.AgentID, l.FilePath, eta,
				fmtDur(time.Until(l.LeaseExpiresAt)), truncate(l.PlanSummary, 60))
		}
	}
	fmt.Println()
	if len(pl) == 0 {
		fmt.Println("Active plans: (none)")
	} else {
		fmt.Printf("Active plans (%d):\n", len(pl))
		for _, p := range pl {
			fmt.Printf("  %s  %-14s  files=%v  — %s\n",
				shortID(p.PlanID), p.AgentID, p.Files, truncate(p.Summary, 60))
		}
	}
	return nil
}

// --- acquire / release (thin wrappers for scripting + the demo) ---

func cmdAcquire(args []string) int {
	fs := flag.NewFlagSet("acquire", flag.ExitOnError)
	plan := fs.String("plan", "", "one-sentence plan")
	eta := fs.Int("eta", 0, "self-estimated seconds until release")
	lease := fs.Int("lease", 0, "lease duration in seconds (default 300)")
	jsonOut := fs.Bool("json", false, "print JSON")
	rest := parseMixed(fs, args)
	if len(rest) != 3 {
		fmt.Fprintln(os.Stderr, "usage: keel acquire <repo> <file> <agent> [flags]")
		return 1
	}
	repo, err := filepath.Abs(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	db, err := openState()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()

	var etaPtr *int
	if *eta > 0 {
		etaPtr = eta
	}
	res, err := locks.New(db).Acquire(context.Background(), locks.AcquireParams{
		RepoRoot:     repo,
		FilePath:     rest[1],
		AgentID:      rest[2],
		PlanSummary:  *plan,
		ETASeconds:   etaPtr,
		LeaseSeconds: *lease,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	} else {
		printAcquire(res)
	}
	if res.Status == locks.StatusBusy {
		return 2
	}
	return 0
}

func cmdRelease(args []string) error {
	fs := flag.NewFlagSet("release", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	rest := parseMixed(fs, args)
	if len(rest) != 2 {
		return errors.New("usage: keel release <lock_id> <agent>")
	}
	db, err := openState()
	if err != nil {
		return err
	}
	defer db.Close()
	ok, err := locks.New(db).Release(context.Background(), rest[0], rest[1])
	if err != nil {
		return err
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]bool{"released": ok}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if ok {
		fmt.Printf("released %s\n", shortID(rest[0]))
	} else {
		fmt.Println("not released (unknown lock, wrong agent, or already released)")
	}
	return nil
}

func printAcquire(res *locks.AcquireResult) {
	if res.Status == locks.StatusAcquired {
		fmt.Printf("ACQUIRED  lock=%s  lease=%s\n", shortID(res.LockID), fmtDur(time.Until(res.LeaseExpiresAt)))
		return
	}
	fmt.Printf("BUSY      held by %s", res.Holder.AgentID)
	if res.Holder.PlanSummary != "" {
		fmt.Printf(" — %q", res.Holder.PlanSummary)
	}
	if res.Holder.ETASeconds != nil {
		fmt.Printf("  eta=%ds", *res.Holder.ETASeconds)
	}
	fmt.Printf("  lease=%s\n", fmtDur(time.Until(res.Holder.LeaseExpiresAt)))
}

// parseMixed lets flags and positional args interleave — Go's stdlib flag package
// stops parsing at the first non-flag, which makes `keel acquire . foo agent --plan=x`
// break in surprising ways. This loop accepts the natural order.
func parseMixed(fs *flag.FlagSet, args []string) []string {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return positionals
		}
		if fs.NArg() == 0 {
			return positionals
		}
		positionals = append(positionals, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func openState() (*store.DB, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(dir, "state.db"))
}

// --- helpers ---

func stateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".keel"), nil
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var _ = runtime.GOOS // keep runtime import if future cross-platform branches land
