# TUI Development Guide

## Quick Reference

**Creating a new TUI:**
1. Embed `core.BaseModel` in your model
2. Call `m.SignalReady()` in `Init()`
3. Call `m.HandleCommonMsg(msg)` in `Update()`
4. Use `style.Default*Styles()` for styling
5. Use `tree.NewRenderer()` for branch trees

**Handler pattern:**
- Interface in `internal/actions/<action>/handler.go`
- Implementation in `internal/cli/stack/<action>_handlers.go`
- Two implementations: `SimpleHandler` (text) + `InteractiveHandler` (TUI)

**Key constants:** `core.KeyCtrlC`, `core.KeyEnter`, `core.KeyEsc`, `core.KeyQuit`

---

## Key Files

- `internal/tui/core/base_model.go` - BaseModel, ReadySignaler, key constants
- `internal/tui/style/colors.go` - Color constants, theme detection
- `internal/tui/style/theme.go` - Style structures (HeaderStyles, LayoutStyles, etc.)
- `internal/tui/components/tree/tree.go` - Stack tree renderer
- `internal/tui/runner.go` - Runner, SafeCmd, Send utilities

## Elm Architecture Foundation

Bubble Tea implements The Elm Architecture (TEA), a functional pattern with three core components:

- **Model**: Single source of truth for all application state
- **Update**: Processes messages, returns `(Model, Cmd)` - state changes happen only here
- **View**: Pure render function returning a string - no side effects

This creates predictable, testable state management. All user interactions flow through typed messages, and the view is always a function of the current model state.

## Performance Rules

**Critical**: Bubble Tea can only process messages as fast as your `Update()` and `View()` methods execute. Violating these rules causes UI lag and message queue buildup.

### Never Do Expensive Work in Update/View

```go
// BAD - blocks the event loop
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    result := fetchFromAPI()  // Blocks UI!
    m.data = result
    return m, nil
}

// GOOD - offload to tea.Cmd
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    return m, m.fetchData()
}

func (m Model) fetchData() tea.Cmd {
    return func() tea.Msg {
        result := fetchFromAPI()  // Runs in goroutine
        return dataFetchedMsg{result}
    }
}
```

### Never Use Goroutines Directly

Always wrap async operations in `tea.Cmd`. Direct goroutines bypass the event loop and cause race conditions.

```go
// BAD - race condition
go func() {
    m.data = fetchData()  // Modifies state outside event loop!
}()

// GOOD - use tea.Cmd
return m, func() tea.Msg {
    return dataMsg{fetchData()}
}
```

## Message Ordering

**Gotcha**: Commands execute concurrently, so their resulting messages arrive in unpredictable order. User input remains ordered (single routine).

```go
// These may complete in any order
return m, tea.Batch(
    fetchUserCmd,
    fetchSettingsCmd,
)

// Use tea.Sequence when order matters
return m, tea.Sequence(
    validateCmd,
    submitCmd,  // Only runs after validate completes
)
```

## Architecture

### Handler Pattern

Actions use a Handler interface to separate business logic from UI:

```
internal/actions/<action>/handler.go    - Defines Handler interface
internal/cli/stack/<action>_handlers.go - Implements handlers
```

Handler interfaces define:
- Callbacks for UI events (Start, OnStep, Complete, Cleanup)
- Prompts (PromptConfirm, PromptRename, etc.)
- `IsInteractive() bool` to check mode
- `NullHandler` struct for non-interactive/testing use

Handler interfaces are adapter boundaries. The action or use case may define the interface, but the implementation belongs in the CLI/TUI layer.

### Boundary Rules

The handler pattern only works if the business logic stays on its side of the boundary:

- Business logic may call handler methods for prompts, progress, and completion events.
- Business logic should return structured results and errors.
- Business logic should not render terminal output directly with `output`, `style`, or Bubble Tea components.
- Business logic should not load config or construct concrete integration clients.
- CLI and TUI code should resolve config, construct dependencies, and decide how to present events/results.

If you are adding a new operation, prefer putting the orchestration in `internal/usecase/<name>/`. Existing `internal/actions/<name>/` packages should be treated as transitional use case packages and follow the same constraints for new code.

### Handler Implementations

Provide two implementations:
- `SimpleHandler` - text output for non-TTY (embed `cli/common.BaseHandler`)
- `InteractiveHandler` - TUI for TTY

```go
func NewFooUI(out output.Output, logger output.Logger) (*tui.Runner, Handler) {
    if tui.IsTTY() {
        model := NewModel()
        runner := tui.NewRunner(model, out, logger)
        runner.Start()
        return runner, NewInteractiveHandler(out, runner, model)
    }
    return nil, NewSimpleHandler(out)
}

// Caller must cleanup
runner, handler := NewFooUI(ctx.Output, ctx.Logger)
if runner != nil {
    defer runner.Cleanup()
}
```

## Models

### BaseModel

Embed `core.BaseModel` for standard lifecycle handling. Import from `tui/core` to avoid import cycles.

```go
type Model struct {
    core.BaseModel  // Spinner, Done, Width, Height
    // ... fields
}

func (m *Model) Init() tea.Cmd {
    m.SignalReady()  // MUST call to prevent Send() race conditions
    return m.InitSpinner()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if handled, cmd := m.HandleCommonMsg(msg); handled {
        return m, cmd
    }
    // Handle component-specific messages...
}
```

### Key Constants

Use constants from `tui/core` instead of string literals:

```go
core.KeyCtrlC  // "ctrl+c"
core.KeyEnter  // "enter"
core.KeyEsc    // "esc"
core.KeyQuit   // "q"
core.KeyUp     // "up"
core.KeyDown   // "down"
```

### Status Types

Use `core.Status` for state tracking:

```go
core.StatusPending  // not started
core.StatusActive   // in progress
core.StatusDone     // completed successfully
core.StatusError    // failed
core.StatusSkipped  // bypassed
core.StatusWaiting  // waiting on dependency
```

### Message Types

Define typed messages for communication. Never use raw strings.

```go
type PhaseStartMsg struct{ Phase Phase }
type CompleteMsg struct{ Summary string }
```

## Styling

Import from `tui/style`. Never use inline `lipgloss.NewStyle()` with magic color numbers.

```go
headerStyles := style.DefaultHeaderStyles()
headerStyles.Title.Render("Select Branch")

layoutStyles := style.DefaultLayoutStyles()
layoutStyles.Container.Render(content)  // standard margin (1, 2)

selectionStyles := style.DefaultSelectionStyles()
selectionStyles.Highlighted.Render("> " + item)

statusStyles := style.DefaultStatusStyles()
statusStyles.Done.Render("Complete")
```

### Color Constants

```go
style.ColorPrimary   // 205 - magenta, titles/emphasis
style.ColorSuccess   // 42  - green, done states
style.ColorError     // 196 - red, error states
style.ColorWarning   // 214 - orange, warnings
style.ColorInsert    // 10  - bright green, new items
style.ColorPending   // 240 - dark gray, pending
```

## Tree Rendering

Always use `tree.StackTreeRenderer` for branch visualizations. Never implement custom tree rendering.

```go
renderer := tree.NewRenderer(data)
renderer.SetAnnotation(branchName, tree.BranchAnnotation{
    CustomLabel: "(marker)",
})
lines := renderer.RenderStack(trunk, tree.RenderOptions{
    HideSummary: true,           // hide PR info for previews
    SkipSelectionPrefix: true,   // omit cursor padding
})
```

For virtual trees (e.g., showing where a branch will be inserted), implement `tree.Data` interface. See `virtualDirectionTree` in `direction_select.go`.

## Terminal Management

### Pause/Resume for Prompts

```go
func (h *InteractiveHandler) PromptConfirm(preview Preview) (bool, error) {
    if h.runner != nil {
        h.runner.Pause()
        defer h.runner.Resume()
    }
    return tui.PromptConfirm("Proceed?", true)
}
```

### Error Handling

```go
// Panic recovery for IO operations
cmd := tui.SafeCmd("fetch-data", logger, func() tea.Msg {
    return doFetch()
})

// Timeout protection for critical messages
runner.MustSend(msg)  // 5s timeout, logs on failure
```

## Patterns

### Multi-Step Flows

Use a single model with explicit state machine, not multiple bubbletea programs:

```go
type flowState int
const (
    stateSelecting flowState = iota
    stateConfirming
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch m.state {
    case stateSelecting:
        return m.updateSelecting(msg)
    case stateConfirming:
        return m.updateConfirming(msg)
    }
    return m, nil
}
```

### Async Operations

Use `tea.Cmd` for async work, return results as typed messages:

```go
func (m Model) validate() tea.Cmd {
    return func() tea.Msg {
        return validationResultMsg{doValidation()}
    }
}
```

### Preview -> Confirm -> Execute

1. Build preview data
2. Call `handler.PromptConfirm*(preview)`
3. If confirmed, execute
4. Call `handler.Complete(result)`

For operations that might fail, run validation early and include results in the preview.

### Live Validation

Pass `ValidateSelection` callback to `LogOptions`. The LogModel handles debouncing and displays results in the footer.

### Hierarchical Model Design

For complex applications, organize models as a tree where the root model routes messages to children:

```go
type RootModel struct {
    core.BaseModel
    activeView  viewType
    listModel   *ListModel
    detailModel *DetailModel
}

func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // 1. Handle global keys directly
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case core.KeyCtrlC:
            return m, tea.Quit
        }
    }

    // 2. Broadcast resize to all children
    if _, ok := msg.(tea.WindowSizeMsg); ok {
        m.listModel.Update(msg)
        m.detailModel.Update(msg)
    }

    // 3. Route other messages to active child
    switch m.activeView {
    case viewList:
        return m.updateList(msg)
    case viewDetail:
        return m.updateDetail(msg)
    }
    return m, nil
}
```

### Receiver Type Considerations

While Bubble Tea examples use value receivers (functional style), pointer receivers enable:
- Persistent state changes in `Init()` and helper methods
- Shared state between model and handler

**Warning**: Avoid modifying model state outside the Update function - this creates race conditions.

## Layout Best Practices

### Dynamic Dimensions

Use lipgloss's `Height()` and `Width()` methods instead of hardcoded offsets:

```go
// BAD - breaks when adding borders/padding
contentHeight := m.Height - 5

// GOOD - calculate from rendered components
headerHeight := lipgloss.Height(headerView)
footerHeight := lipgloss.Height(footerView)
contentHeight := m.Height - headerHeight - footerHeight
```

### Layer-Based Rendering

Render components in consistent layers:

```go
func (m Model) View() string {
    header := m.renderHeader()
    content := m.renderContent()
    footer := m.renderFooter()

    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        content,
        footer,
    )
}
```

## Debugging

### Message Inspection

Dump messages to a file during development for visibility into message flow:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if os.Getenv("DEBUG_TUI") != "" {
        f, _ := os.OpenFile("/tmp/tui-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        fmt.Fprintf(f, "%T: %+v\n", msg, msg)
        f.Close()
    }
    // ... rest of update
}
```

Then tail in another terminal: `tail -f /tmp/tui-debug.log`

### Testing with teatest

Use Charm's teatest for end-to-end testing:

```go
func TestModel(t *testing.T) {
    m := NewModel()
    tm := teatest.NewTestModel(t, m)

    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("test")})

    out := tm.FinalModel(t).(Model)
    assert.Equal(t, "expected", out.value)
}
```

## Terminal Recovery

Panics in commands don't properly reset terminal state, leaving the terminal in raw mode. If this happens, run `reset` in the terminal to restore normal operation.

Use `tui.SafeCmd` for panic recovery in commands:

```go
cmd := tui.SafeCmd("operation-name", logger, func() tea.Msg {
    return riskyOperation()  // Panic is caught and logged
})
```
