# Code Style Rules

## General

- Clarity and simplicity over cleverness
- No backwards compatibility concerns unless specified
- Do not leave code unimplemented, it is better to leave a TODO

## Go Code Style

- Prefer early returns over deep nesting
- Use meaningful variable names; single-letter names only for loop indices

## Error Handling

- Always handle errors explicitly; never ignore them with `_`
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Return errors to callers rather than logging and continuing

## Comments

- Write comments for exported functions and types
- Explain "why" not "what"
- Only add comments where code is not self-documenting

## Testing

- Use table-driven tests for multiple cases
- Keep test setup minimal and focused
