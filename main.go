package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marco-hoyer/github-sync/config"
	"github.com/marco-hoyer/github-sync/github"
	gsync "github.com/marco-hoyer/github-sync/sync"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	verbose      bool
	instanceArg  string
	orgArg       string
	withBranches bool
	workers      int
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "github-sync",
		Short: "Sync GitHub repositories to local filesystem",
		Long: `A CLI tool to sync all repositories from GitHub instances
into the local filesystem using git worktrees.`,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is ~/.github_sync)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVarP(&instanceArg, "instance", "i", "", "specific GitHub instance alias")
	rootCmd.PersistentFlags().StringVarP(&orgArg, "org", "o", "", "specific organization to sync")

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync repositories from GitHub",
		RunE:  runSync,
	}
	syncCmd.Flags().BoolVarP(&withBranches, "branches", "b", false, "also sync all branches as worktrees")
	syncCmd.Flags().IntVarP(&workers, "workers", "w", 0, "number of parallel workers (default from config or 10)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available instances, orgs, or repos",
	}

	listInstancesCmd := &cobra.Command{
		Use:   "instances",
		Short: "List configured GitHub instances",
		RunE:  runListInstances,
	}

	listOrgsCmd := &cobra.Command{
		Use:   "orgs",
		Short: "List organizations",
		RunE:  runListOrgs,
	}

	listReposCmd := &cobra.Command{
		Use:   "repos",
		Short: "List repositories",
		RunE:  runListRepos,
	}

	listCmd.AddCommand(listInstancesCmd, listOrgsCmd, listReposCmd)

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create example config file",
		RunE:  runInit,
	}

	branchCmd := &cobra.Command{
		Use:   "branch <branch-name>",
		Short: "Create a worktree for a branch in the current repo",
		Args:  cobra.ExactArgs(1),
		RunE:  runBranch,
	}

	pushCmd := &cobra.Command{
		Use:   "push",
		Short: "Commit all changes and push to remote",
		RunE:  runPush,
	}

	rootCmd.AddCommand(syncCmd, listCmd, initCmd, branchCmd, pushCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, error) {
	return config.Load(cfgFile)
}

type syncJob struct {
	repo   github.Repository
	syncer *gsync.Syncer
	client *github.Client
}

type syncResult struct {
	repo   string
	action gsync.SyncAction
	err    error
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()

	instances := cfg.Instances
	if instanceArg != "" {
		inst, err := cfg.GetInstance(instanceArg)
		if err != nil {
			return err
		}
		instances = []config.GitHubInstance{*inst}
	}

	for _, inst := range instances {
		fmt.Printf("Syncing from %s...\n", inst.Alias)

		client, err := github.NewClient(ctx, inst.BaseURL, inst.Token)
		if err != nil {
			return fmt.Errorf("failed to create client for %s: %w", inst.Alias, err)
		}

		syncer := gsync.NewSyncer(cfg.RootDir, inst.Alias, inst.Token, verbose)

		var repos []github.Repository

		if orgArg != "" {
			repos, err = client.ListOrgRepos(orgArg)
			if err != nil {
				return fmt.Errorf("failed to list repos for org %s: %w", orgArg, err)
			}
		} else {
			// Get all orgs the user has access to
			orgs, err := client.ListOrganizations()
			if err != nil {
				fmt.Printf("Warning: could not list organizations: %v\n", err)
			}

			// Get repos from each org
			for _, org := range orgs {
				orgRepos, err := client.ListOrgRepos(org)
				if err != nil {
					fmt.Printf("Warning: could not list repos for %s: %v\n", org, err)
					continue
				}
				repos = append(repos, orgRepos...)
			}

			// Also get user repos (personal repos)
			userRepos, err := client.ListUserRepos()
			if err != nil {
				fmt.Printf("Warning: could not list user repos: %v\n", err)
			} else {
				repos = append(repos, userRepos...)
			}
		}

		// Deduplicate repos by full name
		seen := make(map[string]bool)
		var uniqueRepos []github.Repository
		for _, repo := range repos {
			key := repo.Owner + "/" + repo.Name
			if !seen[key] {
				seen[key] = true
				uniqueRepos = append(uniqueRepos, repo)
			}
		}

		// Filter out archived repos
		var activeRepos []github.Repository
		for _, repo := range uniqueRepos {
			if repo.Archived {
				if verbose {
					fmt.Printf("Skipping archived repo: %s/%s\n", repo.Owner, repo.Name)
				}
				continue
			}
			activeRepos = append(activeRepos, repo)
		}

		fmt.Printf("Found %d repositories (excluding archived)\n", len(activeRepos))

		// Create job channel and results channel
		jobs := make(chan syncJob, len(activeRepos))
		results := make(chan syncResult, len(activeRepos))

		// Start worker pool
		var wg sync.WaitGroup
		numWorkers := workers
		if numWorkers == 0 {
			numWorkers = cfg.Workers
		}
		if numWorkers == 0 {
			numWorkers = 10 // default
		}
		if numWorkers > len(activeRepos) {
			numWorkers = len(activeRepos)
		}

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					repoName := fmt.Sprintf("%s/%s", job.repo.Owner, job.repo.Name)

					action, err := job.syncer.SyncRepository(job.repo)
					if err != nil {
						results <- syncResult{repo: repoName, action: action, err: fmt.Errorf("sync failed: %w", err)}
						continue
					}

					// Fetch remote branches for worktree sync and/or cleanup
					branches, err := job.client.ListBranches(job.repo.Owner, job.repo.Name)
					if err != nil {
						if verbose {
							fmt.Printf("Warning: could not list branches for %s: %v\n", repoName, err)
						}
					} else {
						var branchNames []string
						for _, branch := range branches {
							branchNames = append(branchNames, branch.Name)
						}

						// Sync worktrees for each branch if requested
						if withBranches {
							for _, branch := range branches {
								if err := job.syncer.SyncWorktree(job.repo, branch.Name); err != nil {
									if verbose {
										fmt.Printf("Warning: worktree for %s branch %s: %v\n", repoName, branch.Name, err)
									}
								}
							}
						}

						// Always cleanup stale worktrees for deleted branches
						if err := job.syncer.CleanupStaleWorktrees(job.repo, branchNames); err != nil {
							if verbose {
								fmt.Printf("Warning: cleanup stale worktrees for %s: %v\n", repoName, err)
							}
						}
					}

					results <- syncResult{repo: repoName, action: action, err: nil}
				}
			}()
		}

		// Send jobs
		for _, repo := range activeRepos {
			jobs <- syncJob{repo: repo, syncer: syncer, client: client}
		}
		close(jobs)

		// Wait for workers to finish in a goroutine
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		var clonedCount, updatedCount, unchangedCount, skippedCount, failedCount int
		for result := range results {
			if result.err != nil {
				failedCount++
				fmt.Printf("Error syncing %s: %v\n", result.repo, result.err)
			} else {
				switch result.action {
				case gsync.ActionCloned:
					clonedCount++
					if verbose {
						fmt.Printf("Cloned %s\n", result.repo)
					}
				case gsync.ActionUpdated:
					updatedCount++
					if verbose {
						fmt.Printf("Updated %s\n", result.repo)
					}
				case gsync.ActionUnchanged:
					unchangedCount++
					if verbose {
						fmt.Printf("Unchanged %s\n", result.repo)
					}
				case gsync.ActionSkipped:
					skippedCount++
					if verbose {
						fmt.Printf("Skipped %s\n", result.repo)
					}
				}
			}
		}

		// Print summary stats as a table
		total := clonedCount + updatedCount + unchangedCount + skippedCount + failedCount
		fmt.Printf("\n")
		fmt.Printf("┌─────────────────────────────────┐\n")
		fmt.Printf("│ Sync stats for %-16s │\n", inst.Alias)
		fmt.Printf("├───────────────────┬─────────────┤\n")
		fmt.Printf("│ Cloned            │ %11d │\n", clonedCount)
		fmt.Printf("│ Updated           │ %11d │\n", updatedCount)
		fmt.Printf("│ Unchanged         │ %11d │\n", unchangedCount)
		if skippedCount > 0 {
			fmt.Printf("│ Skipped           │ %11d │\n", skippedCount)
		}
		if failedCount > 0 {
			fmt.Printf("│ Failed            │ %11d │\n", failedCount)
		}
		fmt.Printf("├───────────────────┼─────────────┤\n")
		fmt.Printf("│ Total             │ %11d │\n", total)
		fmt.Printf("└───────────────────┴─────────────┘\n")
	}

	fmt.Println("\nSync complete!")
	return nil
}

func runListInstances(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Println("Configured GitHub instances:")
	for _, inst := range cfg.Instances {
		baseURL := inst.BaseURL
		if baseURL == "" {
			baseURL = "https://api.github.com"
		}
		fmt.Printf("  - %s (%s)\n", inst.Alias, baseURL)
	}

	return nil
}

func runListOrgs(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()

	instances := cfg.Instances
	if instanceArg != "" {
		inst, err := cfg.GetInstance(instanceArg)
		if err != nil {
			return err
		}
		instances = []config.GitHubInstance{*inst}
	}

	for _, inst := range instances {
		fmt.Printf("\nOrganizations for %s:\n", inst.Alias)

		client, err := github.NewClient(ctx, inst.BaseURL, inst.Token)
		if err != nil {
			return err
		}

		orgs, err := client.ListOrganizations()
		if err != nil {
			return err
		}

		for _, org := range orgs {
			fmt.Printf("  - %s\n", org)
		}
	}

	return nil
}

func runListRepos(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()

	instances := cfg.Instances
	if instanceArg != "" {
		inst, err := cfg.GetInstance(instanceArg)
		if err != nil {
			return err
		}
		instances = []config.GitHubInstance{*inst}
	}

	for _, inst := range instances {
		fmt.Printf("\nRepositories for %s:\n", inst.Alias)

		client, err := github.NewClient(ctx, inst.BaseURL, inst.Token)
		if err != nil {
			return err
		}

		var repos []github.Repository

		if orgArg != "" {
			repos, err = client.ListOrgRepos(orgArg)
			if err != nil {
				return err
			}
		} else {
			orgs, err := client.ListOrganizations()
			if err != nil {
				fmt.Printf("Warning: could not list organizations: %v\n", err)
			}

			for _, org := range orgs {
				orgRepos, err := client.ListOrgRepos(org)
				if err != nil {
					continue
				}
				repos = append(repos, orgRepos...)
			}
		}

		for _, repo := range repos {
			status := ""
			if repo.Archived {
				status = " (archived)"
			}
			fmt.Printf("  - %s/%s [%s]%s\n", repo.Owner, repo.Name, repo.DefaultBranch, status)
		}
	}

	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config file already exists at %s", configPath)
	}

	exampleConfig := `# GitHub Sync Configuration
root_dir: ~/github-repos

# Number of parallel sync workers (default: 10)
workers: 10

instances:
  # GitHub.com
  - alias: github
    base_url: https://api.github.com
    token: ghp_your_personal_access_token_here

  # GitHub Enterprise (example)
  # - alias: work
  #   base_url: https://github.mycompany.com/api/v3
  #   token: ghp_your_enterprise_token_here
`

	if err := os.WriteFile(configPath, []byte(exampleConfig), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Created example config at %s\n", configPath)
	fmt.Println("Please edit it with your GitHub token(s).")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println(exampleConfig)

	return nil
}

func runBranch(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Verify we're in a git repository
	if _, err := os.Stat(filepath.Join(cwd, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("not in a git repository (no .git directory found)")
	}

	// Get the repo directory name and parent directory
	repoName := filepath.Base(cwd)
	parentDir := filepath.Dir(cwd)

	// Sanitize branch name for directory
	sanitizedBranch := sanitizeBranchName(branchName)
	worktreePath := filepath.Join(parentDir, fmt.Sprintf("%s-%s", repoName, sanitizedBranch))

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// Fetch to make sure we have the branch
	fmt.Fprintf(os.Stderr, "Fetching latest changes...\n")
	fetchCmd := exec.Command("git", "fetch", "--all")
	fetchCmd.Dir = cwd
	if verbose {
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
	}
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Ensure current branch is up to date with remote
	currentBranchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	currentBranchCmd.Dir = cwd
	currentBranchOutput, err := currentBranchCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	currentBranch := strings.TrimSpace(string(currentBranchOutput))

	// Check for uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = cwd
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get git status: %w", err)
	}

	if len(statusOutput) == 0 {
		// Clean working directory, safe to pull
		fmt.Fprintf(os.Stderr, "Updating current branch '%s'...\n", currentBranch)
		pullCmd := exec.Command("git", "pull", "--ff-only")
		pullCmd.Dir = cwd
		if verbose {
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
		}
		if err := pullCmd.Run(); err != nil {
			// Non-fatal: warn but continue (might be on a branch without upstream)
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not pull current branch: %v\n", err)
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "Warning: current branch has uncommitted changes, skipping pull\n")
	}

	// Check if branch exists locally or remotely
	branchExists := false
	checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = cwd
	if err := checkCmd.Run(); err == nil {
		branchExists = true
	} else {
		// Check if it exists as a remote branch
		checkRemoteCmd := exec.Command("git", "rev-parse", "--verify", "origin/"+branchName)
		checkRemoteCmd.Dir = cwd
		if err := checkRemoteCmd.Run(); err == nil {
			branchExists = true
		}
	}

	// Create the worktree
	fmt.Fprintf(os.Stderr, "Creating worktree for branch '%s' at %s...\n", branchName, worktreePath)

	var stdout, stderr bytes.Buffer
	var gitCmd *exec.Cmd
	if branchExists {
		gitCmd = exec.Command("git", "worktree", "add", worktreePath, branchName)
	} else {
		// Branch doesn't exist, create it with -b
		fmt.Fprintf(os.Stderr, "Branch '%s' does not exist, creating new branch...\n", branchName)
		gitCmd = exec.Command("git", "worktree", "add", "-b", branchName, worktreePath)
	}
	gitCmd.Dir = cwd
	if verbose {
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
	} else {
		gitCmd.Stdout = &stdout
		gitCmd.Stderr = &stderr
	}

	if err := gitCmd.Run(); err != nil {
		errOutput := strings.TrimSpace(stderr.String())
		if errOutput == "" {
			errOutput = strings.TrimSpace(stdout.String())
		}
		if errOutput != "" {
			return fmt.Errorf("failed to create worktree: %s", errOutput)
		}
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	// Symlink IDE settings from main repo
	symlinkIDESettings(cwd, worktreePath)

	// Print only the path to stdout so it can be used with: cd $(github-sync branch X)
	fmt.Println(worktreePath)
	return nil
}

func symlinkIDESettings(mainRepoPath, worktreePath string) {
	ideDirs := []string{".idea", ".vscode", ".venv"}

	for _, dir := range ideDirs {
		srcPath := filepath.Join(mainRepoPath, dir)
		dstPath := filepath.Join(worktreePath, dir)

		// Check if source exists in main repo
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		// Check if destination already exists
		if _, err := os.Stat(dstPath); err == nil {
			continue
		}

		// Create relative symlink
		relPath, err := filepath.Rel(worktreePath, srcPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create relative path for %s: %v\n", dir, err)
			continue
		}

		if err := os.Symlink(relPath, dstPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not symlink %s: %v\n", dir, err)
		} else {
			fmt.Fprintf(os.Stderr, "Symlinked %s from main repo\n", dir)
		}
	}
}

func sanitizeBranchName(branch string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	return replacer.Replace(branch)
}

func runPush(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Verify we're in a git repository
	checkCmd := exec.Command("git", "rev-parse", "--git-dir")
	checkCmd.Dir = cwd
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("not in a git repository")
	}

	// Get current branch name
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = cwd
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	// Check if there are any changes to commit
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = cwd
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get git status: %w", err)
	}

	if len(statusOutput) == 0 {
		fmt.Println("No changes to commit.")
		// Still try to push in case there are unpushed commits
		fmt.Println("Pushing existing commits...")
		pushCmd := exec.Command("git", "push", "-u", "origin", branchName)
		pushCmd.Dir = cwd
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("failed to push: %w", err)
		}
		fmt.Println("Done!")
		return nil
	}

	// Show what will be committed
	fmt.Println("Changes to be committed:")
	fmt.Println(string(statusOutput))

	// Create commit message template from branch name
	messageTemplate := branchNameToCommitMessage(branchName)

	// Ask user to refine the commit message
	fmt.Printf("Commit message [%s]: ", messageTemplate)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	input = strings.TrimSpace(input)

	commitMessage := messageTemplate
	if input != "" {
		commitMessage = input
	}

	// Stage all changes
	fmt.Println("Staging changes...")
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = cwd
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Commit
	fmt.Println("Committing...")
	gitCommitCmd := exec.Command("git", "commit", "-m", commitMessage)
	gitCommitCmd.Dir = cwd
	gitCommitCmd.Stdout = os.Stdout
	gitCommitCmd.Stderr = os.Stderr
	if err := gitCommitCmd.Run(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Push
	fmt.Println("Pushing...")
	gitPushCmd := exec.Command("git", "push", "-u", "origin", branchName)
	gitPushCmd.Dir = cwd
	gitPushCmd.Stdout = os.Stdout
	gitPushCmd.Stderr = os.Stderr
	if err := gitPushCmd.Run(); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	// Create PR if not on main/master branch
	if branchName != "main" && branchName != "master" {
		// Check if PR already exists for this branch
		checkPRCmd := exec.Command("gh", "pr", "view", branchName)
		checkPRCmd.Dir = cwd
		if err := checkPRCmd.Run(); err == nil {
			fmt.Println("PR already exists for this branch.")
		} else {
			fmt.Println("Creating pull request...")
			prCmd := exec.Command("gh", "pr", "create", "--title", commitMessage, "--body", "")
			prCmd.Dir = cwd
			prCmd.Stdout = os.Stdout
			prCmd.Stderr = os.Stderr
			prCmd.Stdin = os.Stdin
			if err := prCmd.Run(); err != nil {
				fmt.Printf("Warning: failed to create PR: %v\n", err)
				fmt.Println("You can create it manually with: gh pr create")
			}
		}
	}

	fmt.Println("Done!")
	return nil
}

func branchNameToCommitMessage(branch string) string {
	// Remove common prefixes
	prefixes := []string{"feature/", "feat/", "bugfix/", "fix/", "hotfix/", "release/", "chore/"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(branch, prefix) {
			branch = strings.TrimPrefix(branch, prefix)
			break
		}
	}

	// Replace separators with spaces
	replacer := strings.NewReplacer(
		"-", " ",
		"_", " ",
	)
	return replacer.Replace(branch)
}
