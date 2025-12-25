# Tests

This directory contains integration and concurrency test suites for the Solitudes project.

**Note:** Most unit tests are located alongside their respective source files following Go conventions (e.g., `internal/model/article_test.go`).

## Test Structure

- `integration/` - Integration tests
  - End-to-end workflow tests
  - Tests that verify multiple components working together

- `concurrency/` - Concurrency and performance tests
  - Tests for concurrent operations
  - Thread-safety tests

- `unit/` - Additional unit tests (cross-component validation)
  - `model/` - Article validation and Topic logic tests
  - `router/` - Route handler tests
  - `pkg/` - Utility package tests (i18n, date formatting)

## Running Tests

```bash
# Run all tests (including tests in source directories)
go test ./...

# Run only tests in this directory
go test ./tests/...

# Run specific test suite
go test ./tests/integration/...
go test ./tests/concurrency/...
go test ./tests/unit/...

# Run tests for specific package (with unit tests alongside code)
go test ./internal/model/...

# Run with verbose output
go test -v ./...

# Run with race detector
go test -race ./...
```

## Writing Tests

- Follow Go testing conventions
- Use descriptive test names
- Include table-driven tests where appropriate
- Mock external dependencies
- Keep tests focused and independent
