# Tests

This directory contains various test suites for the Solitudes project.

## Test Structure

- `unit/` - Unit tests for individual components
  - `model/` - Tests for model layer (Article, Comment, User, etc.)
  - `router/` - Tests for HTTP route handlers
  - `pkg/` - Tests for utility packages

- `integration/` - Integration tests
  - Tests that verify multiple components working together

- `concurrency/` - Concurrency and performance tests
  - Tests for concurrent operations
  - Thread-safety tests

## Running Tests

```bash
# Run all tests
go test ./tests/...

# Run specific test suite
go test ./tests/unit/...
go test ./tests/integration/...
go test ./tests/concurrency/...

# Run with verbose output
go test -v ./tests/...

# Run with race detector
go test -race ./tests/...
```

## Writing Tests

- Follow Go testing conventions
- Use descriptive test names
- Include table-driven tests where appropriate
- Mock external dependencies
- Keep tests focused and independent
