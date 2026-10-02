# github-sync

A CLI tool to sync all repositories from GitHub instances into the local filesystem using git worktrees.

## Features

- **Multi-instance support**: Sync from multiple GitHub instances (GitHub.com and GitHub Enterprise)
- **Parallel syncing**: Configurable worker pool for fast parallel repository syncing
- **Git worktrees**: Branches are managed as worktrees, sharing the git object store
- **Safe updates**: Skips repos with uncommitted changes, uses fast-forward only pulls
- **Automatic organization**: Repos organized by instance and branch

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

Edit the file. Prefer `token_cli` or `token_env` so the token stays out of the config file:

```yaml
# Root directory for all synced repositories
root_dir: ~/github-repos

# Number of parallel sync workers (default: 10)
workers: 20

instances:
  # GitHub.com
  - alias: github
    base_url: https://api.github.com
    token_cli: gh auth token
    org: myorg

  # Same token, another org: add a second instance with its own alias
  - alias: myorg-oss
    base_url: https://api.github.com
    token: "gh_some_token"
    org: myorg-oss

  # GitHub Enterprise
  - alias: work
    base_url: https://github.mycompany.com/api/v3
    token_env: GITHUB_TOKEN
    org: internal
```

`token_cli` runs without a shell, so pipes, quotes and `$VAR` expansion are not supported. It must print only the token to stdout and finish within 30 seconds. The same command is run only once, even if several instances use it. Examples:

token_cli examples:

```yaml
token_cli: gh auth token
token_cli: op read op://Private/GitHub/token
token_cli: pass github/token
```

### Configuration Options

| Field                   | Description                                                                                                                                                        |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `root_dir`              | Base directory for all synced repositories (supports `~`)                                                                                                          |
| `workers`               | Number of parallel sync workers (default: 10)                                                                                                                      |
| `instances[].alias`     | Unique name for the GitHub instance                                                                                                                                |
| `instances[].base_url`  | API base URL (use `https://api.github.com` for GitHub.com)                                                                                                         |
| `instances[].token`     | Personal access token with `repo` scope. Use `token_env` instead when possible.                                                                                    |
| `instances[].token_env` | Name of an environment variable that holds the token.                                                                                                              |
| `instances[].token_cli` | Command that prints the token to stdout, e.g. `gh auth token`.                                                                                                     |
| `instances[].org`       | Organization to sync (required). Its repos are checked out directly into `<root_dir>/<alias>/`. Use `github-sync list orgs` to see the orgs your token can access. |

## Directory Structure

Repositories are organized as:

```
<root_dir>/
└── <instance-alias>/
    ├── <repo>/                  # main/master branch
    ├── <repo>-<branch>/         # other branches (worktrees)
    └── ...
```

Example:

```
~/github-repos/
├── github/                  # org: myorg
│   ├── api-service/
│   ├── api-service-feature-auth/
│   └── web-app/
└── work/                    # org: internal
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

# Combine flags
github-sync sync -i work -b -w 15

# Verbose output
github-sync sync -v
```

### Create Branch Worktree

Create a worktree for a specific branch while inside a repository:

```bash
cd ~/github-repos/github/myrepo

# Create worktree for an existing branch
github-sync branch feature-auth
# Creates: ~/github-repos/github/myrepo-feature-auth

# Create worktree for a new branch (auto-creates if it doesn't exist)
github-sync branch my-new-feature
# Creates: ~/github-repos/github/myrepo-my-new-feature

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

# List repos for a specific instance
github-sync list repos -i work
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

## IDE Setup

The tool automatically symlinks `.idea/`, `.vscode/`, `.zed/`, `.venv/`, and `.env` from the main repo to worktrees, so most settings are shared. Below are IDE-specific tips for working with worktrees.

### IntelliJ IDEA / JetBrains IDEs

**Option 1: Open worktrees as separate projects**

- Open the worktree directory directly (`File > Open`)
- IntelliJ detects it as a git repo and uses the symlinked settings
- Each worktree becomes a separate project window

**Option 2: Use built-in worktree support (2023.1+)**

- Open your main repo
- Use `Git > Manage Worktrees` to view and switch between worktrees

**Recommended `.gitignore` additions** (to keep per-worktree state separate):

```
.idea/workspace.xml
.idea/tasks.xml
.idea/usage.statistics.xml
.idea/shelf/
```

### VS Code

**Option 1: Open worktrees as separate windows**

- Open the worktree folder directly (`File > Open Folder`)
- The symlinked `.vscode/` provides shared settings and extensions config

**Option 2: Multi-root workspace**

- `File > Add Folder to Workspace` to add multiple worktrees
- Save as a `.code-workspace` file for easy reopening

**Recommended `.gitignore` additions**:

```
.vscode/.history/
.vscode/*.log
```

**Tip**: The GitLens extension has excellent worktree support via `GitLens: Git Worktrees` view.

### Zed

Zed works well with worktrees out of the box:

- Open worktree directories directly (`file > open`)
- Each worktree opens as an independent project
- Zed auto-detects the git context from the worktree

**Multi-branch workflow**:

- Use `cmd+shift+o` (Open Recent) to quickly switch between worktree directories
- Pin frequently-used worktrees for fast access

**Shared settings**: Zed uses `~/.config/zed/settings.json` globally, so no per-project symlinking is needed for editor settings. Project-specific settings in `.zed/` will be symlinked if present.

## Token Permissions

Your GitHub personal access token needs the following scopes:

- `repo` - Full control of private repositories
- `read:org` - Read organization membership (for listing orgs)

For GitHub Enterprise, ensure your token has equivalent permissions.

## License

MIT
