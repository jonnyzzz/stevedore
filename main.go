package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jonnyzzz/stevedore/internal/stevedore"
)

var (
	Version   = "dev"
	GitRemote = "unknown"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	log.SetFlags(0)

	instance := stevedore.NewInstance(getEnvDefault("STEVEDORE_ROOT", stevedore.DefaultRoot))

	args := os.Args[1:]
	if len(args) == 0 {
		printUsageTo(os.Stdout)
		return
	}

	if args[0] == "-d" || args[0] == "--daemon" {
		if len(args) != 1 {
			log.Printf("ERROR: -d/--daemon cannot be combined with other arguments")
			os.Exit(2)
		}
		runDaemon(instance)
		return
	}

	// Execute command and handle exit code
	output, exitCode := executeCommand(instance, args)
	if output != "" {
		fmt.Print(output)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// executeCommand executes a CLI command and returns output and exit code.
// This is used both by main() for direct execution and by the daemon for remote execution.
func executeCommand(instance *stevedore.Instance, args []string) (output string, exitCode int) {
	var buf strings.Builder

	if len(args) == 0 {
		printUsageTo(&buf)
		return buf.String(), 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsageTo(&buf)
		return buf.String(), 0

	case "version":
		buf.WriteString(fmt.Sprintf("stevedore %s\n", buildInfoSummary()))
		return buf.String(), 0

	case "doctor":
		if err := runDoctorTo(instance, &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "repo":
		if err := runRepoTo(instance, args[1:], &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "param":
		if err := runParamTo(instance, args[1:], &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "deploy":
		if err := runDeployTo(instance, args[1:], &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "status":
		if err := runStatusTo(instance, args[1:], &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "check":
		if err := runCheckTo(instance, args[1:], &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	case "self-update":
		if err := runSelfUpdateTo(instance, &buf); err != nil {
			buf.WriteString(fmt.Sprintf("ERROR: %v\n", err))
			return buf.String(), 1
		}
		return buf.String(), 0

	default:
		buf.WriteString(fmt.Sprintf("ERROR: unknown command: %s\n", args[0]))
		printUsageTo(&buf)
		return buf.String(), 2
	}
}

func runDaemon(instance *stevedore.Instance) {
	if err := instance.EnsureLayout(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}

	// Ensure admin key exists
	if err := instance.EnsureAdminKey(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}

	db, err := instance.OpenDB()
	if err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// Get admin key for HTTP API
	adminKey, err := instance.GetAdminKey()
	if err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}

	printUpstreamWarning()

	log.Printf("Stevedore daemon started (%s), root=%s", buildInfoSummary(), instance.Root)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	daemon := stevedore.NewDaemon(instance, db, stevedore.DaemonConfig{
		AdminKey:   adminKey,
		ListenAddr: getEnvDefault("STEVEDORE_LISTEN_ADDR", ":42107"),
		Version:    Version,
		Build:      GitCommit,
	})

	// Set the executor so API can run CLI commands
	daemon.SetExecutor(func(args []string) (string, int, error) {
		output, exitCode := executeCommand(instance, args)
		if exitCode != 0 {
			return output, exitCode, fmt.Errorf("command failed with exit code %d", exitCode)
		}
		return output, exitCode, nil
	})

	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("ERROR: daemon exited: %v", err)
		os.Exit(1)
	}

	log.Printf("Stevedore daemon stopped")
}

func runDoctorTo(instance *stevedore.Instance, w io.Writer) error {
	if err := instance.EnsureLayout(); err != nil {
		return err
	}

	db, err := instance.OpenDB()
	if err != nil {
		return err
	}
	_ = db.Close()

	deployments, err := instance.ListDeployments()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "stevedore %s\n", buildInfoSummary())
	_, _ = fmt.Fprintf(w, "root: %s\n", instance.Root)
	_, _ = fmt.Fprintf(w, "db: %s\n", instance.DBPath())
	_, _ = fmt.Fprintf(w, "deployments: %d\n", len(deployments))

	// Check if daemon is running and verify version
	adminKey, err := instance.GetAdminKey()
	if err != nil {
		_, _ = fmt.Fprintf(w, "daemon: cannot read admin key (%v)\n", err)
		return nil
	}

	client := stevedore.NewClient(
		"http://localhost:42107",
		adminKey,
		Version,
		GitCommit,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := client.Health(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(w, "daemon: not running or unreachable\n")
		return nil
	}

	_, _ = fmt.Fprintf(w, "daemon: running (version %s, build %s)\n", health.Version, health.Build)

	// Check version compatibility
	if health.Version != Version || health.Build != GitCommit {
		_, _ = fmt.Fprintf(w, "\n⚠️  VERSION MISMATCH DETECTED\n")
		_, _ = fmt.Fprintf(w, "   CLI:    version=%s, build=%s\n", Version, GitCommit)
		_, _ = fmt.Fprintf(w, "   Daemon: version=%s, build=%s\n", health.Version, health.Build)
		_, _ = fmt.Fprintf(w, "\n   Stevedore binaries must match exactly.\n")
		_, _ = fmt.Fprintf(w, "   Please reinstall stevedore or restart the daemon.\n")
	} else {
		_, _ = fmt.Fprintf(w, "daemon: version match ✓\n")
	}

	return nil
}

func runRepoTo(instance *stevedore.Instance, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("repo: missing subcommand (add|key|list)")
	}

	switch args[0] {
	case "add":
		branch, remaining, err := consumeStringFlag(args[1:], "--branch", "main")
		if err != nil {
			return err
		}
		if len(remaining) != 2 {
			return errors.New("usage: repo add <deployment> <git-url> [--branch <branch>]")
		}
		deployment := remaining[0]
		url := remaining[1]

		publicKey, err := instance.AddRepo(deployment, stevedore.RepoSpec{
			URL:    url,
			Branch: branch,
		})
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(w, "Repository registered: %s\n", deployment)
		_, _ = fmt.Fprintf(w, "\nAdd this public key as a read-only Deploy Key:\n\n%s\n\n", publicKey)

		// Show GitHub deploy key URL if it's a GitHub repository
		if deployKeyURL := githubDeployKeyURL(url); deployKeyURL != "" {
			_, _ = fmt.Fprintf(w, "GitHub Deploy Keys URL:\n  %s\n\n", deployKeyURL)
			_, _ = fmt.Fprintf(w, "Steps:\n")
			_, _ = fmt.Fprintf(w, "  1. Open the URL above in your browser\n")
			_, _ = fmt.Fprintf(w, "  2. Click 'Add deploy key'\n")
			_, _ = fmt.Fprintf(w, "  3. Title: stevedore-%s\n", deployment)
			_, _ = fmt.Fprintf(w, "  4. Paste the public key above\n")
			_, _ = fmt.Fprintf(w, "  5. Leave 'Allow write access' unchecked (read-only)\n")
			_, _ = fmt.Fprintf(w, "  6. Click 'Add key'\n")
		}
		return nil

	case "key":
		if len(args) != 2 {
			return errors.New("usage: repo key <deployment>")
		}
		publicKey, err := instance.RepoPublicKey(args[1])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(w, publicKey)
		return nil

	case "list":
		if len(args) != 1 {
			return errors.New("usage: repo list")
		}
		deployments, err := instance.ListDeployments()
		if err != nil {
			return err
		}
		for _, d := range deployments {
			_, _ = fmt.Fprintln(w, d)
		}
		return nil

	default:
		return fmt.Errorf("repo: unknown subcommand: %s", args[0])
	}
}

func runDeployTo(instance *stevedore.Instance, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("deploy: missing subcommand (sync|up|down)")
	}

	ctx := context.Background()

	switch args[0] {
	case "sync":
		// Parse --no-clean flag
		cleanEnabled := true
		remaining := args[1:]
		var deployment string
		for _, arg := range remaining {
			if arg == "--no-clean" {
				cleanEnabled = false
			} else {
				deployment = arg
			}
		}
		if deployment == "" {
			return errors.New("usage: deploy sync <deployment> [--no-clean]")
		}

		_, _ = fmt.Fprintf(w, "Syncing repository for %s...\n", deployment)
		result, err := instance.GitSyncClean(ctx, deployment, cleanEnabled)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "Repository synced: %s@%s\n", result.Branch, shortCommit(result.Commit))
		return nil

	case "up":
		if len(args) != 2 {
			return errors.New("usage: deploy up <deployment>")
		}
		deployment := args[1]

		_, _ = fmt.Fprintf(w, "Deploying %s...\n", deployment)
		result, err := instance.Deploy(ctx, deployment, stevedore.ComposeConfig{})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "Deployed: %s (compose file: %s)\n", result.ProjectName, result.ComposeFile)
		if len(result.Services) > 0 {
			_, _ = fmt.Fprintf(w, "Services: %s\n", strings.Join(result.Services, ", "))
		}
		return nil

	case "down":
		if len(args) != 2 {
			return errors.New("usage: deploy down <deployment>")
		}
		deployment := args[1]

		_, _ = fmt.Fprintf(w, "Stopping %s...\n", deployment)
		if err := instance.Stop(ctx, deployment, stevedore.ComposeConfig{}); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "Stopped: %s\n", deployment)
		return nil

	default:
		return fmt.Errorf("deploy: unknown subcommand: %s", args[0])
	}
}

func runStatusTo(instance *stevedore.Instance, args []string, w io.Writer) error {
	ctx := context.Background()

	if len(args) == 0 {
		// List all deployments with status
		deployments, err := instance.ListDeployments()
		if err != nil {
			return err
		}
		if len(deployments) == 0 {
			_, _ = fmt.Fprintln(w, "No deployments found")
			return nil
		}

		for _, d := range deployments {
			status, err := instance.GetDeploymentStatus(ctx, d)
			if err != nil {
				_, _ = fmt.Fprintf(w, "%-20s  ERROR: %v\n", d, err)
				continue
			}
			healthMark := "✓"
			if !status.Healthy {
				healthMark = "✗"
			}
			_, _ = fmt.Fprintf(w, "%-20s  %s  %s\n", d, healthMark, status.Message)
		}
		return nil
	}

	// Show detailed status for specific deployment
	deployment := args[0]
	status, err := instance.GetDeploymentStatus(ctx, deployment)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Deployment: %s\n", status.Deployment)
	_, _ = fmt.Fprintf(w, "Project:    %s\n", status.ProjectName)
	_, _ = fmt.Fprintf(w, "Healthy:    %v\n", status.Healthy)
	_, _ = fmt.Fprintf(w, "Status:     %s\n", status.Message)

	if len(status.Containers) > 0 {
		_, _ = fmt.Fprintln(w, "\nContainers:")
		for _, c := range status.Containers {
			healthInfo := ""
			if c.Health != stevedore.HealthNone {
				healthInfo = fmt.Sprintf(" [%s]", c.Health)
			}
			_, _ = fmt.Fprintf(w, "  %-20s  %-12s  %s%s\n", c.Service, c.ID, c.Status, healthInfo)
		}
	}

	return nil
}

func runCheckTo(instance *stevedore.Instance, args []string, w io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: check <deployment>")
	}

	ctx := context.Background()
	deployment := args[0]

	result, err := instance.GitCheckRemote(ctx, deployment)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Deployment: %s\n", deployment)
	_, _ = fmt.Fprintf(w, "Branch:     %s\n", result.Branch)
	_, _ = fmt.Fprintf(w, "Current:    %s\n", shortCommit(result.CurrentCommit))
	_, _ = fmt.Fprintf(w, "Remote:     %s\n", shortCommit(result.RemoteCommit))
	if result.HasChanges {
		_, _ = fmt.Fprintln(w, "Status:     Updates available")
	} else {
		_, _ = fmt.Fprintln(w, "Status:     Up to date")
	}

	return nil
}

func runSelfUpdateTo(instance *stevedore.Instance, w io.Writer) error {
	ctx := context.Background()

	_, _ = fmt.Fprintln(w, "Starting self-update...")

	updated, err := instance.TriggerSelfUpdate(ctx, GitCommit)
	if err != nil {
		return err
	}

	if updated {
		_, _ = fmt.Fprintln(w, "Self-update initiated. Container will be replaced shortly.")
	} else {
		_, _ = fmt.Fprintln(w, "Already up to date.")
	}

	return nil
}

func runParamTo(instance *stevedore.Instance, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("param: missing subcommand (set|get|list)")
	}

	switch args[0] {
	case "set":
		if len(args) < 3 {
			return errors.New("usage: param set <deployment> <name> <value> | param set <deployment> <name> --stdin")
		}
		deployment := args[1]
		name := args[2]

		var value []byte
		if len(args) >= 4 && args[3] != "--stdin" {
			value = []byte(strings.Join(args[3:], " "))
		} else {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			value = []byte(strings.TrimRight(string(b), "\n"))
		}

		if err := instance.SetParameter(deployment, name, value); err != nil {
			return err
		}
		return nil

	case "get":
		if len(args) != 3 {
			return errors.New("usage: param get <deployment> <name>")
		}
		value, err := instance.GetParameter(args[1], args[2])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(w, string(value))
		return nil

	case "list":
		if len(args) != 2 {
			return errors.New("usage: param list <deployment>")
		}
		names, err := instance.ListParameters(args[1])
		if err != nil {
			return err
		}
		for _, n := range names {
			_, _ = fmt.Fprintln(w, n)
		}
		return nil

	default:
		return fmt.Errorf("param: unknown subcommand: %s", args[0])
	}
}

func printUpstreamWarning() {
	repo := strings.TrimSpace(os.Getenv("STEVEDORE_SOURCE_REPO"))
	ref := strings.TrimSpace(os.Getenv("STEVEDORE_SOURCE_REF"))

	if ref != "main" {
		return
	}
	if repo == "" {
		return
	}

	if strings.Contains(repo, "github.com/jonnyzzz/stevedore") || strings.Contains(repo, "git@github.com:jonnyzzz/stevedore") {
		log.Printf("WARNING: This Stevedore instance appears to be installed from upstream main (%s). Fork recommended.", repo)
	}
}

func consumeStringFlag(args []string, flagName string, defaultValue string) (string, []string, error) {
	value := defaultValue
	remaining := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		if args[i] != flagName {
			remaining = append(remaining, args[i])
			continue
		}
		if i+1 >= len(args) {
			return "", nil, fmt.Errorf("%s requires a value", flagName)
		}
		value = args[i+1]
		i++
	}

	return value, remaining, nil
}

func getEnvDefault(name string, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return defaultValue
}

func printUsageTo(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  stevedore -d              # run daemon")
	_, _ = fmt.Fprintln(w, "  stevedore doctor")
	_, _ = fmt.Fprintln(w, "  stevedore version")
	_, _ = fmt.Fprintln(w, "  stevedore status [<deployment>]")
	_, _ = fmt.Fprintln(w, "  stevedore check <deployment>   # check for git updates")
	_, _ = fmt.Fprintln(w, "  stevedore self-update          # update stevedore itself")
	_, _ = fmt.Fprintln(w, "  stevedore repo add <deployment> <git-url> [--branch <branch>]")
	_, _ = fmt.Fprintln(w, "  stevedore repo key <deployment>")
	_, _ = fmt.Fprintln(w, "  stevedore repo list")
	_, _ = fmt.Fprintln(w, "  stevedore deploy sync <deployment> [--no-clean]")
	_, _ = fmt.Fprintln(w, "  stevedore deploy up <deployment>")
	_, _ = fmt.Fprintln(w, "  stevedore deploy down <deployment>")
	_, _ = fmt.Fprintln(w, "  stevedore param set <deployment> <name> <value> | ... --stdin")
	_, _ = fmt.Fprintln(w, "  stevedore param get <deployment> <name>")
	_, _ = fmt.Fprintln(w, "  stevedore param list <deployment>")
}

func buildInfoSummary() string {
	version := strings.TrimSpace(Version)
	if version == "" || version == "unknown" {
		version = "dev"
	}

	remote := strings.TrimSpace(GitRemote)
	commit := strings.TrimSpace(GitCommit)
	buildDate := strings.TrimSpace(BuildDate)

	remoteOK := remote != "" && remote != "unknown"
	commitOK := commit != "" && commit != "unknown"
	buildDateOK := buildDate != "" && buildDate != "unknown"

	var meta []string
	switch {
	case remoteOK && commitOK:
		meta = append(meta, fmt.Sprintf("%s@%s", remote, shortCommit(commit)))
	case remoteOK:
		meta = append(meta, remote)
	case commitOK:
		meta = append(meta, shortCommit(commit))
	}
	if buildDateOK {
		meta = append(meta, buildDate)
	}

	if len(meta) == 0 {
		return version
	}

	return fmt.Sprintf("%s (%s)", version, strings.Join(meta, " "))
}

func shortCommit(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// githubDeployKeyURL extracts the GitHub repository path from various URL formats
// and returns the deploy keys settings URL, or empty string if not a GitHub URL.
func githubDeployKeyURL(repoURL string) string {
	repoURL = strings.TrimSpace(repoURL)

	var owner, repo string

	switch {
	case strings.HasPrefix(repoURL, "git@github.com:"):
		// git@github.com:owner/repo.git
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}

	case strings.HasPrefix(repoURL, "ssh://git@github.com/"):
		// ssh://git@github.com/owner/repo.git
		path := strings.TrimPrefix(repoURL, "ssh://git@github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}

	case strings.HasPrefix(repoURL, "https://github.com/"):
		// https://github.com/owner/repo.git or https://github.com/owner/repo
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}
	}

	if owner == "" || repo == "" {
		return ""
	}

	return fmt.Sprintf("https://github.com/%s/%s/settings/keys", owner, repo)
}
