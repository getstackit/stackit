# Configuration System

## Quick Reference

**Key files:**
- `internal/config/config_git.go` - High-level typed interface (`GitConfig`)
- `internal/config/keys.go` - Key constants and defaults
- `internal/config/metadata.go` - Config option metadata registry (for docs/template generation)
- `internal/config/project_config.go` - Team config (`.stackit.yaml`)
- `internal/git/config.go` - Low-level git config access (`ConfigStore`)

**Adding a new config key:**
1. Add constant to `keys.go`: `const KeyFoo = "stackit.foo"`
2. Add entry to `metadata.go`: `Options` registry (for template/docs generation)
3. Add getter/setter to `config_git.go`
4. If interface method needed, add to `interface.go`

**Priority:** Personal git config > `.stackit.yaml` > defaults

---

## Overview

Stackit uses a layered configuration system with the following priority order:

1. **Personal Git Config** (highest priority) - Stored in `.git/config`, not shared
2. **Team Project Config** - Stored in `.stackit.yaml`, committed and shared with team
3. **Defaults** (lowest priority) - Built-in sensible defaults

This allows teams to define shared settings that individual developers can override locally.

## Repository Configuration (Git Config)

Repository-level settings are stored in `.git/config` using git's native configuration system with a `stackit.` prefix.

### Storage Mechanism

All keys are namespaced under `stackit.*`:

```bash
# Reading a value
git config --local stackit.trunk

# Writing a value
git config --local stackit.trunk main

# Multi-value keys (like additional trunks)
git config --local --add stackit.trunks develop
```

### Configuration Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `stackit.trunk` | string | `main` | Primary trunk branch |
| `stackit.trunks` | string[] | `[]` | Additional trunk branches |
| `stackit.branch.pattern` | string | `{username}/{date}/{message}` | Branch naming template |
| `stackit.submit.footer` | bool | `true` | Include PR footer in descriptions |
| `stackit.submit.draft` | bool | `false` | Create PRs as drafts by default |
| `stackit.submit.web` | string | `never` | When to open PRs in browser (always/created/never) |
| `stackit.submit.labels` | string[] | `[]` | Default labels for PRs |
| `stackit.submit.reviewers` | string[] | `[]` | Default reviewers for PRs |
| `stackit.submit.assignees` | string[] | `[]` | Default assignees for PRs |
| `stackit.undo.depth` | int | `10` | Max undo snapshots to retain |
| `stackit.worktree.basePath` | string | `""` | Base directory for worktrees |
| `stackit.worktree.autoClean` | bool | `true` | Auto-remove clean, empty managed worktrees during sync |
| `stackit.merge.method` | string | `""` | Merge strategy (squash/merge/rebase) |
| `stackit.ci.command` | string | `""` | CI validation command |
| `stackit.ci.timeout` | int | `600` | CI command timeout in seconds |
| `stackit.split.hunkSelector` | string | `tui` | Hunk selector mode (tui/git) |
| `stackit.maxConcurrency` | int | `0` | Max concurrent operations (0 = auto) |
| `stackit.hooks.approvedPostWorktreeCreate` | string[] | `[]` | Approved post-worktree hooks |

### Code Location

The configuration system is implemented across several files:

```
internal/config/
├── config_git.go      # GitConfig - high-level typed interface
├── keys.go            # Configuration key constants and defaults
├── metadata.go        # Config option metadata registry
├── template.go        # Template generation for `stackit config init`
├── interface.go       # Configurer interface definition
├── branch_pattern.go  # Branch naming pattern parsing
└── migrate.go         # Legacy JSON migration

internal/git/
└── config.go          # ConfigStore - low-level git config access
```

### ConfigStore (Low-Level)

`internal/git/config.go` provides direct access to git config:

```go
type ConfigStore struct {
    repoRoot string
}

// Core operations
func (c *ConfigStore) Get(key string) (string, error)
func (c *ConfigStore) GetAll(key string) ([]string, error)  // Multi-value keys
func (c *ConfigStore) Set(key, value string) error
func (c *ConfigStore) Add(key, value string) error          // Add to multi-value
func (c *ConfigStore) Unset(key string) error               // Remove all values
func (c *ConfigStore) Exists(key string) bool               // Check if key exists

// Typed helpers
func (c *ConfigStore) GetBool(key string) (bool, error)
func (c *ConfigStore) GetBoolWithDefault(key string, def bool) bool
func (c *ConfigStore) GetInt(key string) (int, error)
func (c *ConfigStore) GetIntWithDefault(key string, def int) int
func (c *ConfigStore) SetBool(key string, value bool) error
func (c *ConfigStore) SetInt(key string, value int) error
```

### GitConfig (High-Level)

`internal/config/config_git.go` wraps ConfigStore with typed methods:

```go
type GitConfig struct {
    repoRoot string
    store    *git.ConfigStore
}

// Example methods
func (c *GitConfig) Trunk() string
func (c *GitConfig) SetTrunk(trunk string) error
func (c *GitConfig) AllTrunks() []string
func (c *GitConfig) IsTrunk(branch string) bool
func (c *GitConfig) BranchNamePattern() string
func (c *GitConfig) SubmitFooter() bool
func (c *GitConfig) UndoStackDepth() int
```

### Loading Configuration

Configuration is loaded through `internal/app/context.go`:

```go
// Standard loading (includes auto-migration)
cfg, err := config.LoadConfig(repoRoot)

// Via app context
ctx, err := app.NewContextAutoWithWriter(ctx, repoRoot, opts, writer)
// Access via ctx.Config
```

## Project Configuration (.stackit.yaml)

Team-shared configuration is stored in `.stackit.yaml` at the repository root. This file is committed to git and shared with all team members. These settings act as team defaults that can be overridden by personal git config.

### File Format

```yaml
# Team trunk branch (most commonly changed)
trunk: main

# Additional trunk branches (e.g., release branches)
trunks:
  - develop
  - staging

# Branch naming pattern for the team
branch:
  pattern: "{username}/{date}/{message}"

# PR submission settings
submit:
  footer: true  # Include stackit footer in PR descriptions
  draft: false  # Create PRs as drafts by default
  web: never    # Open PRs in browser: always, created, or never
  labels:       # Default labels for PRs
    - needs-review
  reviewers:    # Default reviewers for PRs
    - teammate1
    - teammate2
  assignees:    # Default assignees for PRs
    - octocat

# Merge method preference
merge:
  method: squash  # squash, merge, or rebase

# CI validation settings
ci:
  command: "make test"
  timeout: 600  # seconds

# Undo settings
undo:
  depth: 10  # Max snapshots to retain

# Worktree settings
worktree:
  basePath: ""  # Base directory for worktrees (empty = auto)
  autoClean: true  # Auto-remove clean, empty managed worktrees during sync

# Split settings
split:
  hunkSelector: tui  # tui or git

# Worktree hooks (require approval before execution)
hooks:
  post-worktree-create:
    - "npm install"
    - "make deps"
```

### Configuration Options

The table below shows all options available in `.stackit.yaml`. The "Team Fallback" column indicates whether the setting is read from `.stackit.yaml` when not set in personal git config.

| Option | Type | Default | Description | Team Fallback |
|--------|------|---------|-------------|---------------|
| `trunk` | string | `main` | Primary trunk branch | Yes |
| `trunks` | string[] | `[]` | Additional trunk branches (merged with git config) | Yes (additive) |
| `branch.pattern` | string | `{username}/{date}/{message}` | Branch naming template | Yes |
| `submit.footer` | bool | `true` | Include PR footer | Yes |
| `submit.draft` | bool | `false` | Create PRs as drafts by default | Yes |
| `submit.web` | string | `never` | Open PRs in browser (always/created/never) | Yes |
| `submit.labels` | string[] | `[]` | Default labels for PRs | Yes (additive) |
| `submit.reviewers` | string[] | `[]` | Default reviewers for PRs | Yes (additive) |
| `submit.assignees` | string[] | `[]` | Default assignees for PRs | Yes (additive) |
| `merge.method` | string | `""` | Merge strategy (squash/merge/rebase) | Yes |
| `ci.command` | string | `""` | CI validation command | Yes |
| `ci.timeout` | int | `600` | CI timeout in seconds | Yes |
| `undo.depth` | int | `10` | Max undo snapshots to retain | Yes |
| `worktree.basePath` | string | `""` | Base directory for worktrees | Yes |
| `worktree.autoClean` | bool | `true` | Auto-remove clean, empty managed worktrees during sync | Yes |
| `split.hunkSelector` | string | `tui` | Hunk selector mode (tui/git) | Yes |
| `maxConcurrency` | int | `0` | Max concurrent operations (0 = auto) | Yes |
| `hooks.post-worktree-create` | string[] | `[]` | Post-worktree-create commands | No (requires approval) |

### Layered Configuration Example

```yaml
# .stackit.yaml (committed, shared with team)
trunk: develop
branch:
  pattern: "feature/{message}"
merge:
  method: squash
```

```bash
# Personal override in git config
git config --local stackit.trunk main
git config --local stackit.branch.pattern "{username}/{message}"
# merge.method still uses team setting: squash
```

### Code Location

```
internal/config/
└── project_config.go  # ProjectConfig struct and loading
```

### Loading Project Config

```go
type ProjectConfig struct {
    Trunk  string       `yaml:"trunk,omitempty"`
    Trunks []string     `yaml:"trunks,omitempty"`
    Branch BranchConfig `yaml:"branch,omitempty"`
    Submit SubmitConfig `yaml:"submit,omitempty"`
    Merge  MergeConfig  `yaml:"merge,omitempty"`
    CI     CIConfig     `yaml:"ci,omitempty"`
    Hooks  HooksConfig  `yaml:"hooks,omitempty"`
}

// Load from repo root
cfg, err := config.LoadProjectConfig(repoRoot)
if cfg.HasTrunk() {
    // Use cfg.Trunk
}
```

## Hook Approval System

Post-worktree-create hooks defined in `.stackit.yaml` require user approval before execution. Approvals are stored in git config (not shared).

### Flow

1. Hook defined in `.stackit.yaml` (shared)
2. User runs a command that creates a worktree
3. Stackit prompts for approval if hook not yet approved
4. Approval saved to `stackit.hooks.approvedPostWorktreeCreate` (local)
5. Subsequent runs skip the prompt

### Implementation

See `internal/actions/worktree/hooks.go` for the hook execution logic.

## Continuation State

Interrupted operations (e.g., merge conflicts during restack) store state for resumption:

**Location:** `.git/.stackit_continue`

**Format:** JSON

```go
type ContinuationState struct {
    BranchesToRestack     []string
    BranchesToSync        []string
    CurrentBranchOverride string
    RebasedBranchBase     string
}
```

**Code:** `internal/config/continuation.go`

## Migration from Legacy JSON

Older repositories may have configuration in `.git/.stackit_config` (JSON format). Stackit automatically migrates this to git config on first access.

### Migration Process

1. Check if `.git/.stackit_config` exists and `stackit.trunk` is not set
2. Read values from JSON file
3. Write to git config with `stackit.*` keys
4. Rename JSON file to `.stackit_config.migrated`

**Code:** `internal/config/migrate.go`

## Worktree Handling

For git worktrees, configuration is stored in the main repository's `.git` directory, ensuring all worktrees share the same configuration:

```go
// internal/config/repo_config.go
func resolveGitDir(repoRoot string) string {
    // Uses: git rev-parse --git-common-dir
    // Returns shared .git directory
}
```

## CLI Commands

```bash
# Interactive configuration editor
stackit config

# List all configuration
stackit config --list

# Show all configuration with sources (personal/team/default)
stackit config show

# Get a specific value
stackit config get branch.pattern
stackit config get submit.footer

# Set a value (personal override)
stackit config set branch.pattern "{username}/{date}/{message}"
stackit config set submit.footer false

# Unset a value (revert to team/default)
stackit config unset branch.pattern
stackit config unset merge.method

# Reset all personal configuration overrides
stackit config reset
```

**Code:** `internal/cli/config.go`

## Adding New Configuration

Adding a new config key requires updates to multiple files. The pattern differs based on the value type.

### Checklist

1. **`internal/config/keys.go`**
   - Add key constant: `const KeyMyNewSetting = "stackit.my.newSetting"`
   - Add default constant: `const DefaultMyNewSetting = "value"`
   - For enum types, add validation slice: `var ValidMyNewSetting = []string{"opt1", "opt2"}`

2. **`internal/config/metadata.go`** (for template/docs generation)
   - Add entry to `Options` slice with YAMLPath, GitKey, Description, Default, Section
   - Run `go test ./internal/config -run TestGenerateConfigTemplate_Golden -update` to update template
   - Verify `TestOptionsCoversAllKeys` passes (ensures all keys are documented)

3. **`internal/config/config_git.go`**
   - Add getter method with layered fallback (personal > team > default)
   - Add setter method with validation
   - Add `Unset*` method to remove personal override
   - Add key to `ResetAllPersonal()` slice

4. **`internal/config/project_config.go`** (for team config support)
   - Add field to appropriate config struct (e.g., `SubmitConfig`)
   - Add `Has*` method to check if set
   - Add `Get*` method for pointer types (bool)
   - Add validation in `Validate()` if needed

5. **`internal/cli/config.go`** (for CLI support)
   - Add key constant (e.g., `keyMyNewSetting = "my.newSetting"`)
   - Add case in `newConfigGetCmd()` switch
   - Add case in `newConfigSetCmd()` switch
   - Add case in `newConfigUnsetCmd()` switch
   - Add entry in `showConfigWithSources()`

### Patterns by Type

**Boolean config:**
```go
// keys.go
const KeyMyBool = "stackit.my.bool"
const DefaultMyBool = false

// config_git.go
func (c *GitConfig) MyBool() bool {
    if c.store.Exists(KeyMyBool) {
        return c.store.GetBoolWithDefault(KeyMyBool, DefaultMyBool)
    }
    if c.project != nil && c.project.HasMyBool() {
        return c.project.GetMyBool()
    }
    return DefaultMyBool
}

// project_config.go - use pointer to distinguish unset from false
type MyConfig struct {
    MyBool *bool `yaml:"myBool,omitempty"`
}
func (c *ProjectConfig) HasMyBool() bool { return c.My.MyBool != nil }
func (c *ProjectConfig) GetMyBool() bool { return *c.My.MyBool }
```

**String enum config:**
```go
// keys.go
const KeyMyEnum = "stackit.my.enum"
const DefaultMyEnum = "option1"
var ValidMyEnum = []string{"option1", "option2", "option3"}

// config_git.go
func (c *GitConfig) MyEnum() string {
    val, _ := c.store.Get(KeyMyEnum)
    if val != "" {
        if !slices.Contains(ValidMyEnum, val) {
            return DefaultMyEnum
        }
        return val
    }
    if c.project != nil && c.project.HasMyEnum() {
        return c.project.My.Enum
    }
    return DefaultMyEnum
}

func (c *GitConfig) SetMyEnum(val string) error {
    if !slices.Contains(ValidMyEnum, val) {
        return fmt.Errorf("invalid value: %s (must be one of: %s)", val, strings.Join(ValidMyEnum, ", "))
    }
    return c.store.Set(KeyMyEnum, val)
}
```

**Multi-value (array) config:**
```go
// keys.go
const KeyMyList = "stackit.my.list"

// config_git.go - arrays are merged from git config and project config
func (c *GitConfig) MyList() []string {
    items := []string{}
    gitItems, _ := c.store.GetAll(KeyMyList)
    for _, item := range gitItems {
        if !slices.Contains(items, item) {
            items = append(items, item)
        }
    }
    if c.project != nil && c.project.HasMyList() {
        for _, item := range c.project.My.List {
            if !slices.Contains(items, item) {
                items = append(items, item)
            }
        }
    }
    return items
}

func (c *GitConfig) AddMyListItem(item string) error {
    return c.store.Add(KeyMyList, item)
}

// cli/config.go - use .add and .clear suffixes for multi-value keys
const (
    keyMyList      = "my.list"
    keyMyListAdd   = "my.list.add"
    keyMyListClear = "my.list.clear"
)
```

### CLI Config Pattern

For each key in `internal/cli/config.go`:

```go
// Get command
case keyMyNewSetting:
    _, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.MyNewSetting())

// Set command
case keyMyNewSetting:
    if err := cfg.SetMyNewSetting(value); err != nil {
        return fmt.Errorf("failed to set %s: %w", keyMyNewSetting, err)
    }
    splog.Info("Set %s to: %s", keyMyNewSetting, value)

// Unset command
case keyMyNewSetting:
    if err := cfg.UnsetMyNewSetting(); err != nil {
        return fmt.Errorf("failed to unset %s: %w", keyMyNewSetting, err)
    }
    splog.Info("Unset %s (now using: %s)", keyMyNewSetting, cfg.MyNewSetting())

// Show command - determine source
source := getStringSource(config.KeyMyNewSetting, projectCfg != nil && projectCfg.HasMyNewSetting())
formatLine("my.newSetting", cfg.MyNewSetting(), source)
```

## Submit Command Config Flow

When adding config options that affect PR submission, values flow through multiple layers:

```
cli/stack/submit.go          → Loads config, builds submit.Options
  ↓
actions/submit/submit.go     → submit.Options struct (add ConfigXxx fields)
  ↓
actions/submit/planning.go   → Passes to MetadataOptions
  ↓
actions/submit/submit_metadata.go → MetadataOptions → PRMetadata
  ↓
github/pr_operations.go      → CreatePROptions/UpdatePROptions → GitHub API
```

**Key files for submit config:**
- `internal/cli/stack/submit.go` - Load config, populate `submit.Options`
- `internal/actions/submit/submit.go` - `Options` struct with `ConfigXxx` fields
- `internal/actions/submit/planning.go` - Pass config to `MetadataOptions`
- `internal/actions/submit/submit_metadata.go` - `MetadataOptions`, `PRMetadata` structs
- `internal/github/pr_operations.go` - `CreatePROptions` for GitHub API

**Pattern for submit config:**
```go
// 1. submit.go Options struct
type Options struct {
    // ... flags ...
    ConfigMyOption string // Config-driven option
}

// 2. cli/stack/submit.go - load and pass config
opts := submit.Options{
    // ... flags ...
    ConfigMyOption: cfg.MyOption(),
}

// 3. submit_metadata.go MetadataOptions
type MetadataOptions struct {
    // ...
    ConfigMyOption string
}

// 4. planning.go - pass to metadata
metadataOpts := MetadataOptions{
    ConfigMyOption: opts.ConfigMyOption,
}

// 5. Use in PreparePRMetadata or submitBranch
```

## Design Principles

1. **Layered configuration** - Personal git config > team project config > defaults
2. **Git-native storage** - Uses git config for personal settings, enabling standard git tooling
3. **Team consistency** - `.stackit.yaml` provides team-wide defaults
4. **Typed access** - High-level API provides type safety over raw string keys
5. **Sensible defaults** - All settings have defaults; missing keys don't error
6. **Automatic migration** - Legacy formats upgraded transparently
7. **Personal overrides** - Developers can override team settings without affecting others
