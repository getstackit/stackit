---
icon: material/book-open-variant
title: Stackit User Guide
description: Comprehensive guide to using stackit. Learn core concepts, troubleshoot common issues, and master stacked Git workflows.
---

# User Guide

This guide covers core concepts and troubleshooting for stackit. For practical usage patterns, see [Workflows](../workflows/index.md). For parallel development, see [Worktrees](../worktrees/index.md).

## Topics

<div class="grid cards" markdown>

-   :material-graph:{ .lg .middle } **Core Concepts**

    ---

    Learn about stacks, parents, children, and how stackit manages branch relationships.

    [Learn concepts →](concepts.md)

-   :material-help-circle:{ .lg .middle } **Troubleshooting**

    ---

    Solutions to common issues and error messages.

    [Get help →](troubleshooting.md)

</div>

## Quick Reference

### Essential Commands

- $$stackit tree$$ - View your stack
- $$stackit create$$ - Create a new branch
- $$stackit submit$$ - Create/update PRs
- $$stackit sync$$ - Update from trunk
- $$stackit merge$$ - Merge your stack

### Need Help Fast?

Run the doctor command to diagnose common issues:

```bash
stackit doctor
```

Or check the [FAQ](../community/faq.md) for frequently asked questions.

## Related Sections

- [Workflows →](../workflows/index.md) — Daily, advanced, and team collaboration patterns
- [Worktrees →](../worktrees/index.md) — Work on multiple stacks in parallel
- [Integrations →](../integrations/index.md) — AI agents, GitHub, git hooks, and shell
