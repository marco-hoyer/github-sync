# github-sync

A CLI tool to sync all repositories from GitHub instances into the local filesystem using git worktrees.

## Features

- **Multi-instance support**: Sync from multiple GitHub instances (GitHub.com and GitHub Enterprise)
- **Parallel syncing**: Configurable worker pool for fast parallel repository syncing
- **Git worktrees**: Branches are managed as worktrees, sharing the git object store
- **Safe updates**: Skips repos with uncommitted changes, uses fast-forward only pulls
- **Automatic organization**: Repos organized by instance, organization, and branch

## Installation

```bash
go install github.com/marco-hoyer/github-sync@latest
```

Or build from source:

```bash
git clone https://github.com/marco-hoyer/github-sync.git
cd github-sync
go build -o github-sync .
```

## Configuration

Create a configuration file at `~/.github_sync`:

```bash
github-sync init
```

Edit the file with your GitHub tokens:

```yaml
# Root directory for all synced repositories
root_dir: ~/github-repos

# Number of parallel sync workers (default: 10)
workers: 20

instances:
  # GitHub.com
  - alias: github
    base_url: https://api.github.com
    token: ghp_your_personal_access_token

  # GitHub Enterprise
  - alias: work
    base_url: https://github.mycompany.com/api/v3
    token: ghp_your_enterprise_token
```

### Configuration Options

| Field | Description |
|-------|-------------|
| `root_dir` | Base directory for all synced repositories (supports `~`) |
| `workers` | Number of parallel sync workers (default: 10) |
| `instances[].alias` | Unique name for the GitHub instance |
| `instances[].base_url` | API base URL (use `https://api.github.com` for GitHub.com) |
| `instances[].token` | Personal access token with `repo` scope |

## Directory Structure

Repositories are organized as:

```
<root_dir>/
└── <instance-alias>/
    └── <organization>/
        ├── <repo>/                  # main/master branch
        ├── <repo>-<branch>/         # other branches (worktrees)
        └── ...
```

Example:

```
~/github-repos/
├── github/
│   └── myorg/
│       ├── api-service/
│       ├── api-service-feature-auth/
│       └── web-app/
└── work/
    └── internal/
        ├── platform/
        └── platform-develop/
```

## Usage

### Sync Repositories

```bash
# Sync all repos from all configured instances
github-sync sync

# Sync with 20 parallel workers (default: 10)
github-sync sync -w 20

# Sync including all branches as worktrees
github-sync sync --branches

# Sync a specific instance only
github-sync sync -i github

# Sync a specific organization only
github-sync sync -o myorg

# Combine filters
github-sync sync -i work -o platform-team -b -w 15

# Verbose output
github-sync sync -v
```

### Create Branch Worktree

Create a worktree for a specific branch while inside a repository:

```bash
cd ~/github-repos/github/myorg/myrepo

# Create worktree for an existing branch
github-sync branch feature-auth
# Creates: ~/github-repos/github/myorg/myrepo-feature-auth

# Create worktree for a new branch (auto-creates if it doesn't exist)
github-sync branch my-new-feature
# Creates: ~/github-repos/github/myorg/myrepo-my-new-feature

# Create and cd into the new worktree in one command
cd $(github-sync branch feature-auth)
```

### Push Changes

Commit all changes and push to remote. Uses the branch name as a commit message template and creates a PR for non-main branches:

```bash
github-sync push
# 1. Shows affected files
# 2. Prompts for commit message (branch name as default, e.g., "SRE 3674 docs")
# 3. Commits, pushes, and creates PR (if not on main/master)
```

Requires `gh` CLI for PR creation.

### List Resources

```bash
# List configured GitHub instances
github-sync list instances

# List organizations you have access to
github-sync list orgs

# List organizations for a specific instance
github-sync list orgs -i github

# List repositories
github-sync list repos

# List repos for a specific org
github-sync list repos -o myorg
```

### Command Reference

```
github-sync [command]

Commands:
  init        Create example config file
  sync        Sync repositories from GitHub
  branch      Create a worktree for a branch in the current repo
  push        Commit all changes and push to remote
  list        List instances, orgs, or repos
  completion  Generate shell autocompletion
  help        Help about any command

Global Flags:
  -c, --config string     Config file (default ~/.github_sync)
  -i, --instance string   Filter to specific GitHub instance
  -o, --org string        Filter to specific organization
  -v, --verbose           Verbose output
  -h, --help              Help for command

Sync Flags:
  -b, --branches          Sync all branches as worktrees
  -w, --workers int       Number of parallel workers (overrides config, default 10)
```

## How It Works

1. **Initial clone**: Repositories are cloned to `<root>/<instance>/<org>/<repo>`
2. **Updates**: Existing repos are fetched and fast-forward merged (if clean)
3. **Worktrees**: With `--branches`, additional branches are added as git worktrees sharing the same object store
4. **Shared settings**: `.idea/`, `.vscode/`, and `.venv/` are automatically symlinked from the main repo to worktrees
5. **Cleanup**: Worktrees for deleted remote branches are automatically removed during sync
6. **Safety**: Repos/worktrees with uncommitted changes are skipped to avoid data loss

## Token Permissions

Your GitHub personal access token needs the following scopes:

- `repo` - Full control of private repositories
- `read:org` - Read organization membership (for listing orgs)

For GitHub Enterprise, ensure your token has equivalent permissions.

## License

MIT
