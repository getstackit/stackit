---
icon: material/source-branch
title: Create Your First Stack
description: Step-by-step guide to creating a stack of Git branches with stackit. Learn to visualize and navigate your stack structure.
---

# Create your first stack

This guide walks you through creating a stack of branches.

## Initialize stackit

In your repository, run:

```bash
stackit init
```

This detects your trunk branch (usually `main`) and prepares the repo for stacking.

## Create your first branch

Stage some changes, then create a branch:

```bash
git add internal/api.go
stackit create add-api -m "feat: add base api"
```

!!! note
    $$stackit create$$ commits your staged changes and creates a new branch in one step.

## Stack another branch on top

Make more changes and create another branch:

```bash
git add internal/logic.go
stackit create add-logic -m "feat: implement logic"
```

## Visualize the stack

See your current position in the stack:

```bash
stackit tree
```

```
● add-logic ← you are here
│
◯ add-api
│
main
```

### Tree structures

Stacks can also branch when you have parallel work:

```
◯ add-tests
│
│ ● add-ui ← you are here
├─┘
◯ add-logic
│
◯ add-api
│
main
```

To create a parallel branch, navigate to the parent branch and create from there:

```bash
stackit checkout add-logic
git add internal/ui.go
stackit create add-ui -m "feat: add ui components"
```

## Navigate your stack

Use navigation commands to move around:

- $$stackit up$$ - Move to the child branch
- $$stackit down$$ - Move to the parent branch
- $$stackit top$$ - Jump to the top of the stack
- $$stackit bottom$$ - Jump to the bottom (trunk)
- $$stackit checkout$$ - Interactive branch switcher

## Next steps

Now that you have a stack, learn how to [submit PRs →](submit.md)
