package sync

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marco-hoyer/github-sync/github"
)

type Syncer struct {
	rootDir       string
	instanceAlias string
	token         string
	verbose       bool
}

func NewSyncer(rootDir, instanceAlias, token string, verbose bool) *Syncer {
	return &Syncer{
		rootDir:       rootDir,
		instanceAlias: instanceAlias,
		token:         token,
		verbose:       verbose,
	}
}

func (s *Syncer) runGit(args ...string) error {
	cmd := exec.Command("git", args...)

	var stdout, stderr bytes.Buffer
	if s.verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		if !s.verbose {
			errOutput := strings.TrimSpace(stderr.String())
			if errOutput == "" {
				errOutput = strings.TrimSpace(stdout.String())
			}
			if errOutput != "" {
				return fmt.Errorf("%w: %s", err, errOutput)
			}
		}
		return err
	}
	return nil
}

func (s *Syncer) SyncRepository(repo github.Repository) error {
	repoPath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, repo.Name)
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		if err := s.cloneRepo(repo, repoPath); err != nil {
			return fmt.Errorf("failed to clone: %w", err)
		}
	} else {
		if err := s.updateRepo(repoPath, defaultBranch); err != nil {
			return fmt.Errorf("failed to update: %w", err)
		}
	}

	return nil
}

func (s *Syncer) cloneRepo(repo github.Repository, repoPath string) error {
	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return err
	}

	cloneURL := github.InsertTokenInURL(repo.CloneURL, s.token)

	s.log("Cloning %s/%s...", repo.Owner, repo.Name)
	return s.runGit("clone", cloneURL, repoPath)
}

func (s *Syncer) updateRepo(repoPath, defaultBranch string) error {
	s.log("Updating %s...", repoPath)

	// Fetch all updates
	if err := s.runGit("-C", repoPath, "fetch", "--all", "--prune"); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Check if we're on the default branch and it's clean
	currentBranch, err := s.getCurrentBranch(repoPath)
	if err != nil {
		return err
	}

	if currentBranch == defaultBranch {
		// Check for uncommitted changes
		cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain")
		output, err := cmd.Output()
		if err != nil {
			return err
		}

		if len(output) == 0 {
			// Clean working directory, safe to pull
			if err := s.runGit("-C", repoPath, "pull", "--ff-only"); err != nil {
				s.log("Warning: pull failed for %s: %v", repoPath, err)
			}
		} else {
			s.log("Warning: %s has uncommitted changes, skipping pull", repoPath)
		}
	}

	return nil
}

func (s *Syncer) SyncWorktree(repo github.Repository, branch string) error {
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Skip if this is the default branch (already handled by main repo)
	if branch == defaultBranch {
		return nil
	}

	mainRepoPath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, repo.Name)
	worktreePath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, fmt.Sprintf("%s-%s", repo.Name, sanitizeBranchName(branch)))

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return s.updateWorktree(worktreePath, branch)
	}

	// Check if main repo exists
	if _, err := os.Stat(mainRepoPath); os.IsNotExist(err) {
		return fmt.Errorf("main repo not cloned yet: %s", mainRepoPath)
	}

	s.log("Creating worktree for %s/%s branch %s...", repo.Owner, repo.Name, branch)

	// Create worktree
	if err := s.runGit("-C", mainRepoPath, "worktree", "add", worktreePath, branch); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}

func (s *Syncer) updateWorktree(worktreePath, branch string) error {
	s.log("Updating worktree %s...", worktreePath)

	// Check for uncommitted changes
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if len(output) > 0 {
		s.log("Warning: %s has uncommitted changes, skipping update", worktreePath)
		return nil
	}

	// Pull changes
	if err := s.runGit("-C", worktreePath, "pull", "--ff-only"); err != nil {
		s.log("Warning: pull failed for worktree %s: %v", worktreePath, err)
	}

	return nil
}

func (s *Syncer) ListWorktrees(repo github.Repository) ([]string, error) {
	mainRepoPath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, repo.Name)

	cmd := exec.Command("git", "-C", mainRepoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			worktrees = append(worktrees, strings.TrimPrefix(line, "worktree "))
		}
	}

	return worktrees, nil
}

func (s *Syncer) RemoveWorktree(repo github.Repository, branch string) error {
	mainRepoPath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, repo.Name)
	worktreePath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, fmt.Sprintf("%s-%s", repo.Name, sanitizeBranchName(branch)))

	s.log("Removing worktree %s...", worktreePath)

	return s.runGit("-C", mainRepoPath, "worktree", "remove", worktreePath)
}

func (s *Syncer) CleanupStaleWorktrees(repo github.Repository, remoteBranches []string) error {
	mainRepoPath := filepath.Join(s.rootDir, s.instanceAlias, repo.Owner, repo.Name)

	// Check if main repo exists
	if _, err := os.Stat(mainRepoPath); os.IsNotExist(err) {
		return nil
	}

	// Build a set of valid branch names (sanitized)
	validBranches := make(map[string]bool)
	for _, branch := range remoteBranches {
		validBranches[sanitizeBranchName(branch)] = true
	}

	// Also add the default branch
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	validBranches[sanitizeBranchName(defaultBranch)] = true

	// List all worktrees
	worktrees, err := s.ListWorktrees(repo)
	if err != nil {
		return err
	}

	// Check each worktree
	repoPrefix := repo.Name + "-"
	for _, wtPath := range worktrees {
		// Skip the main repo itself
		if wtPath == mainRepoPath {
			continue
		}

		wtName := filepath.Base(wtPath)

		// Check if this is a worktree for this repo
		if !strings.HasPrefix(wtName, repoPrefix) {
			continue
		}

		// Extract the branch name part
		branchPart := strings.TrimPrefix(wtName, repoPrefix)

		// Check if this branch still exists
		if !validBranches[branchPart] {
			s.log("Removing stale worktree for deleted branch: %s", wtPath)

			// Check for uncommitted changes before removing
			cmd := exec.Command("git", "-C", wtPath, "status", "--porcelain")
			output, err := cmd.Output()
			if err == nil && len(output) > 0 {
				s.log("Warning: %s has uncommitted changes, skipping removal", wtPath)
				continue
			}

			// Remove the worktree
			if err := s.runGit("-C", mainRepoPath, "worktree", "remove", wtPath); err != nil {
				s.log("Warning: failed to remove stale worktree %s: %v", wtPath, err)
			}
		}
	}

	return nil
}

func (s *Syncer) getCurrentBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *Syncer) log(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func sanitizeBranchName(branch string) string {
	// Replace characters that are problematic in directory names
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
