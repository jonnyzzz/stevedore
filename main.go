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
		printUsage(os.Stdout)
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

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return
	case "version":
		_, _ = fmt.Printf("stevedore %s\n", buildInfoSummary())
		return
	case "doctor":
		if err := runDoctor(instance); err != nil {
			log.Printf("ERROR: %v", err)
			os.Exit(1)
		}
		return
	case "repo":
		if err := runRepo(instance, args[1:]); err != nil {
			log.Printf("ERROR: %v", err)
			os.Exit(1)
		}
		return
	case "param":
		if err := runParam(instance, args[1:]); err != nil {
			log.Printf("ERROR: %v", err)
			os.Exit(1)
		}
		return
	case "deploy":
		if err := runDeploy(instance, args[1:]); err != nil {
			log.Printf("ERROR: %v", err)
			os.Exit(1)
		}
		return
	case "status":
		if err := runStatus(instance, args[1:]); err != nil {
			log.Printf("ERROR: %v", err)
			os.Exit(1)
		}
		return
	default:
		log.Printf("ERROR: unknown command: %s", args[0])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func runDaemon(instance *stevedore.Instance) {
	if err := instance.EnsureLayout(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}

	db, err := instance.OpenDB()
	if err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
	_ = db.Close()

	printUpstreamWarning()

	log.Printf("Stevedore daemon started (%s), root=%s", buildInfoSummary(), instance.Root)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stevedore daemon stopping")
			return
		case <-ticker.C:
		}
	}
}

func runDoctor(instance *stevedore.Instance) error {
	if err := instance.EnsureLayout(); err != nil {
		return err
	}

	db, err := instance.OpenDB()
	if err != nil {
		return err
	}
	_ = db.Close()

	printUpstreamWarning()

	deployments, err := instance.ListDeployments()
	if err != nil {
		return err
	}

	_, _ = fmt.Printf("stevedore %s\n", buildInfoSummary())
	_, _ = fmt.Printf("root: %s\n", instance.Root)
	_, _ = fmt.Printf("db: %s\n", instance.DBPath())
	_, _ = fmt.Printf("deployments: %d\n", len(deployments))
	return nil
}

func runRepo(instance *stevedore.Instance, args []string) error {
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

		_, _ = fmt.Printf("Repository registered: %s\n", deployment)
		_, _ = fmt.Printf("\nAdd this public key as a read-only Deploy Key:\n\n%s\n\n", publicKey)

		// Show GitHub deploy key URL if it's a GitHub repository
		if deployKeyURL := githubDeployKeyURL(url); deployKeyURL != "" {
			_, _ = fmt.Printf("GitHub Deploy Keys URL:\n  %s\n\n", deployKeyURL)
			_, _ = fmt.Printf("Steps:\n")
			_, _ = fmt.Printf("  1. Open the URL above in your browser\n")
			_, _ = fmt.Printf("  2. Click 'Add deploy key'\n")
			_, _ = fmt.Printf("  3. Title: stevedore-%s\n", deployment)
			_, _ = fmt.Printf("  4. Paste the public key above\n")
			_, _ = fmt.Printf("  5. Leave 'Allow write access' unchecked (read-only)\n")
			_, _ = fmt.Printf("  6. Click 'Add key'\n")
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
		_, _ = fmt.Println(publicKey)
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
			_, _ = fmt.Println(d)
		}
		return nil

	default:
		return fmt.Errorf("repo: unknown subcommand: %s", args[0])
	}
}

func runParam(instance *stevedore.Instance, args []string) error {
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
		_, _ = fmt.Print(string(value))
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
			_, _ = fmt.Println(n)
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

func runDeploy(instance *stevedore.Instance, args []string) error {
	if len(args) == 0 {
		return errors.New("deploy: missing subcommand (sync|up|down)")
	}

	ctx := context.Background()

	switch args[0] {
	case "sync":
		if len(args) != 2 {
			return errors.New("usage: deploy sync <deployment>")
		}
		deployment := args[1]

		_, _ = fmt.Printf("Syncing repository for %s...\n", deployment)
		result, err := instance.GitCloneLocal(ctx, deployment)
		if err != nil {
			return err
		}
		_, _ = fmt.Printf("Repository synced: %s@%s\n", result.Branch, shortCommit(result.Commit))
		return nil

	case "up":
		if len(args) != 2 {
			return errors.New("usage: deploy up <deployment>")
		}
		deployment := args[1]

		_, _ = fmt.Printf("Deploying %s...\n", deployment)
		result, err := instance.Deploy(ctx, deployment, stevedore.ComposeConfig{})
		if err != nil {
			return err
		}
		_, _ = fmt.Printf("Deployed: %s (compose file: %s)\n", result.ProjectName, result.ComposeFile)
		if len(result.Services) > 0 {
			_, _ = fmt.Printf("Services: %s\n", strings.Join(result.Services, ", "))
		}
		return nil

	case "down":
		if len(args) != 2 {
			return errors.New("usage: deploy down <deployment>")
		}
		deployment := args[1]

		_, _ = fmt.Printf("Stopping %s...\n", deployment)
		if err := instance.Stop(ctx, deployment, stevedore.ComposeConfig{}); err != nil {
			return err
		}
		_, _ = fmt.Printf("Stopped: %s\n", deployment)
		return nil

	default:
		return fmt.Errorf("deploy: unknown subcommand: %s", args[0])
	}
}

func runStatus(instance *stevedore.Instance, args []string) error {
	ctx := context.Background()

	if len(args) == 0 {
		// List all deployments with status
		deployments, err := instance.ListDeployments()
		if err != nil {
			return err
		}
		if len(deployments) == 0 {
			_, _ = fmt.Println("No deployments found")
			return nil
		}

		for _, d := range deployments {
			status, err := instance.GetDeploymentStatus(ctx, d)
			if err != nil {
				_, _ = fmt.Printf("%-20s  ERROR: %v\n", d, err)
				continue
			}
			healthMark := "✓"
			if !status.Healthy {
				healthMark = "✗"
			}
			_, _ = fmt.Printf("%-20s  %s  %s\n", d, healthMark, status.Message)
		}
		return nil
	}

	// Show detailed status for specific deployment
	deployment := args[0]
	status, err := instance.GetDeploymentStatus(ctx, deployment)
	if err != nil {
		return err
	}

	_, _ = fmt.Printf("Deployment: %s\n", status.Deployment)
	_, _ = fmt.Printf("Project:    %s\n", status.ProjectName)
	_, _ = fmt.Printf("Healthy:    %v\n", status.Healthy)
	_, _ = fmt.Printf("Status:     %s\n", status.Message)

	if len(status.Containers) > 0 {
		_, _ = fmt.Println("\nContainers:")
		for _, c := range status.Containers {
			healthInfo := ""
			if c.Health != stevedore.HealthNone {
				healthInfo = fmt.Sprintf(" [%s]", c.Health)
			}
			_, _ = fmt.Printf("  %-20s  %-12s  %s%s\n", c.Service, c.ID, c.Status, healthInfo)
		}
	}

	return nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  stevedore -d              # run daemon")
	_, _ = fmt.Fprintln(w, "  stevedore doctor")
	_, _ = fmt.Fprintln(w, "  stevedore version")
	_, _ = fmt.Fprintln(w, "  stevedore status [<deployment>]")
	_, _ = fmt.Fprintln(w, "  stevedore repo add <deployment> <git-url> [--branch <branch>]")
	_, _ = fmt.Fprintln(w, "  stevedore repo key <deployment>")
	_, _ = fmt.Fprintln(w, "  stevedore repo list")
	_, _ = fmt.Fprintln(w, "  stevedore deploy sync <deployment>")
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
