"""
Generate CLI reference documentation from stackit --help output.

This hook runs during the build process and generates the cli-reference.md
file in the includes directory based on stackit command help text.
"""

import subprocess
import os
from pathlib import Path
from mkdocs.config.defaults import MkDocsConfig


def on_pre_build(config: MkDocsConfig):
    """Generate CLI reference documentation before building the site."""
    # Get the repository root (2 levels up from doc/)
    doc_dir = Path(config.docs_dir).parent
    repo_root = doc_dir.parent

    # Check if this is a git worktree and find the actual repo root
    git_file = repo_root / ".git"
    if git_file.is_file():
        # This is a worktree, read the gitdir to find main repo
        git_content = git_file.read_text().strip()
        if git_content.startswith("gitdir:"):
            # Extract path like: gitdir: /path/to/repo/.git/worktrees/name
            worktree_git = Path(git_content.split(":", 1)[1].strip())
            # The common-dir file in the worktree git dir points to the main .git
            common_dir_file = worktree_git / "commondir"
            if common_dir_file.exists():
                # Read commondir (usually contains "../..")
                common_rel = common_dir_file.read_text().strip()
                # Resolve relative to worktree git dir to get main .git
                main_git_dir = (worktree_git / common_rel).resolve()
                # Parent of .git is the repo root
                actual_repo_root = main_git_dir.parent
                print(f"Detected worktree, using main repo at: {actual_repo_root}")
                repo_root = actual_repo_root

    # Path to the stackit binary
    stackit_bin = repo_root / "stackit"

    # Check if stackit binary exists, if not try to build it
    if not stackit_bin.exists():
        print(f"stackit binary not found at {stackit_bin}, attempting to build...")
        try:
            subprocess.run(
                ["just", "build"],
                cwd=repo_root,
                check=True,
                capture_output=True
            )
        except subprocess.CalledProcessError as e:
            print(f"Failed to build stackit: {e}")
            # Create a placeholder file
            _create_placeholder(doc_dir)
            return
        except FileNotFoundError:
            print("'just' command not found, trying 'go build'...")
            try:
                subprocess.run(
                    ["go", "build", "-o", "stackit", "./apps/cli"],
                    cwd=repo_root,
                    check=True,
                    capture_output=True
                )
            except (subprocess.CalledProcessError, FileNotFoundError) as e:
                print(f"Failed to build stackit with go: {e}")
                _create_placeholder(doc_dir)
                return

    if not stackit_bin.exists():
        print(f"stackit binary still not found at {stackit_bin}")
        _create_placeholder(doc_dir)
        return

    # Generate the CLI reference
    try:
        result = subprocess.run(
            [str(stackit_bin), "--help"],
            capture_output=True,
            text=True,
            check=True
        )

        help_output = result.stdout

        # Get help for all subcommands
        cli_ref = _generate_cli_reference(stackit_bin, help_output)

        # Write to includes directory
        includes_dir = doc_dir / "includes"
        includes_dir.mkdir(exist_ok=True)

        output_file = includes_dir / "cli-reference.md"
        output_file.write_text(cli_ref)

        print(f"Generated CLI reference at {output_file}")

    except subprocess.CalledProcessError as e:
        print(f"Failed to generate CLI reference: {e}")
        _create_placeholder(doc_dir)
    except Exception as e:
        print(f"Unexpected error generating CLI reference: {e}")
        _create_placeholder(doc_dir)


def _generate_cli_reference(stackit_bin: Path, main_help: str) -> str:
    """Generate formatted CLI reference from help output."""
    output = []

    output.append("```")
    output.append("stackit <command> [flags]")
    output.append("```")
    output.append("")
    output.append(f"stackit is a command-line tool that makes working with stacked changes fast and intuitive.")
    output.append("")

    # Parse main help output for commands
    lines = main_help.split('\n')
    in_commands = False
    commands = []

    for line in lines:
        if 'Available Commands:' in line or 'Commands:' in line:
            in_commands = True
            continue
        if in_commands:
            if line.strip() == '' or line.startswith('Flags:') or line.startswith('Global Flags:'):
                break
            # Parse command line (format: "  command     description")
            parts = line.strip().split(None, 1)
            if len(parts) >= 1 and not parts[0].startswith('-'):
                # Skip git passthrough commands
                description = parts[1] if len(parts) > 1 else ""
                if "passthrough" in description.lower():
                    continue
                commands.append(parts[0])

    # Generate documentation for each command
    for cmd in commands:
        try:
            result = subprocess.run(
                [str(stackit_bin), cmd, "--help"],
                capture_output=True,
                text=True,
                check=True,
                timeout=5
            )

            cmd_help = result.stdout

            # Skip commands that output git man pages
            if cmd_help.strip().startswith("GIT-") or "Git Manual" in cmd_help:
                continue

            output.append(f"## {cmd} {{#stackit-{cmd}}}")
            output.append("")
            output.append(_format_help_output(cmd_help, cmd))
            output.append("")

        except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
            # Skip commands that don't have help or timeout
            pass

    # Add global flags section
    output.append("## Global Flags")
    output.append("")
    output.append("These flags are available on all stackit commands:")
    output.append("")
    output.append("| Flag | Description |")
    output.append("|:---|:---|")
    output.append("| `--cwd <path>` | Working directory in which to perform operations |")
    output.append("| `--debug` | Write debug output to the terminal |")
    output.append("| `--interactive` | Enable interactive features like prompts, pagers, and editors (default: true) |")
    output.append("| `--no-interactive` | Disable all interactive features |")
    output.append("| `--verify` | Enable git hooks (pre-commit, etc.) (default: true) |")
    output.append("| `--no-verify` | Disable git hooks |")
    output.append("| `--quiet`, `-q` | Minimize output to the terminal (implies `--no-interactive`) |")
    output.append("")

    return '\n'.join(output)


def _format_help_output(help_text: str, cmd: str) -> str:
    """Format help output into markdown.

    Parses the Cobra help format:
    - Description (before Usage:)
    - Usage line(s)
    - Flags section
    - Global Flags section (skipped - shown once at end)
    """
    lines = help_text.split('\n')
    output = []

    # State machine for parsing
    section = "description"  # description, usage, flags, global_flags, done
    description_lines = []
    usage_lines = []
    flags = []

    for line in lines:
        # Detect section transitions
        if line.startswith('Usage:'):
            section = "usage"
            continue
        if line.startswith('Flags:'):
            section = "flags"
            continue
        if line.startswith('Global Flags:'):
            section = "global_flags"
            continue
        if line.startswith('Available Commands:') or line.startswith('Commands:'):
            section = "done"
            continue

        # Process based on current section
        if section == "description":
            if line.strip():
                description_lines.append(line.strip())
        elif section == "usage":
            if line.strip():
                usage_lines.append(line.strip())
        elif section == "flags":
            if line.strip():
                flags.append(line)
        elif section == "global_flags":
            # Skip global flags - they're documented once at the end
            pass

    # Build output

    # Usage block
    if usage_lines:
        output.append("```")
        for usage in usage_lines:
            output.append(usage)
        output.append("```")
        output.append("")

    # Description
    if description_lines:
        for desc in description_lines:
            output.append(desc)
        output.append("")

    # Flags table
    if flags:
        output.append("**Flags:**")
        output.append("")
        output.append("| Flag | Description |")
        output.append("|:-----|:------------|")

        for flag_line in flags:
            parsed = _parse_flag_line(flag_line)
            if parsed:
                flag_str, desc = parsed
                # Escape pipe characters in description
                desc = desc.replace("|", "\\|")
                output.append(f"| `{flag_str}` | {desc} |")

        output.append("")

    return '\n'.join(output)


def _parse_flag_line(line: str) -> tuple[str, str] | None:
    """Parse a flag line into (flag_string, description).

    Handles formats like:
      -a, --all              Description here
      --flag                 Description here
      -f, --flag string      Description with type
    """
    line = line.strip()
    if not line or not line.startswith('-'):
        return None

    # Find where the description starts
    # Flags are indented with multiple spaces before description
    parts = line.split('  ')
    if len(parts) < 2:
        # Single part - might be wrapped or malformed
        return None

    # First part is the flag(s), rest is description
    flag_part = parts[0].strip()

    # Find the description (skip empty parts from multiple spaces)
    desc_parts = [p.strip() for p in parts[1:] if p.strip()]
    description = ' '.join(desc_parts) if desc_parts else ""

    return (flag_part, description)


def _create_placeholder(doc_dir: Path):
    """Create a placeholder CLI reference file."""
    includes_dir = doc_dir / "includes"
    includes_dir.mkdir(exist_ok=True)

    output_file = includes_dir / "cli-reference.md"
    placeholder = """!!! warning "CLI reference not available"

    The CLI reference could not be generated automatically.
    Please build the stackit binary and rebuild the documentation.

    ```bash
    just build
    cd doc && uv run mkdocs build
    ```

For a complete list of commands, run:

```bash
stackit --help
```
"""
    output_file.write_text(placeholder)
    print(f"Created placeholder CLI reference at {output_file}")
