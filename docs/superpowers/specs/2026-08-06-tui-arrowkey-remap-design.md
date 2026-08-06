# TUI Arrow-Key Remap

## Scope

Change keyboard behavior on form screens only. Up and Down will move the selected field, while Left and Right will adjust the selected date, time, or choice value. Workspace switching, row navigation, text editing, and inline action strips retain their current behavior.

## Implementation

Update the shared form key handler in `internal/tui/model.go` so all form screens use the approved vertical-field and horizontal-value mapping. Update the form help text to describe the new mapping. No new abstraction or dependency is needed.

## Testing

Replace the existing form navigation regression expectations in `internal/tui/model_test.go` with assertions that Up and Down change the selected field and Left and Right adjust the selected value. Preserve coverage for time rollover and ensure the focused form behavior remains consistent across the shared handler.

## Acceptance criteria

- On every form screen, Up/Down changes the selected field.
- On every form screen, Left/Right adjusts supported date, time, and choice values.
- Free-form text fields still enter editing with Enter, and Left/Right still move the text cursor while editing.
- Existing non-form navigation behavior is unchanged.
- The TUI test suite passes.
