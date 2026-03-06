# Contributing

Thank you for your interest in contributing! We welcome bug reports, feature requests, documentation improvements, and code contributions.

## Getting Started

1. Fork the repository
2. Clone your fork
3. Create a new branch for your change
4. Commit your changes
5. Open a Pull Request

```bash
git clone https://github.com/<your-username>/<project>.git
cd <project>
git checkout -b feature/my-feature
```

## Development Setup

### Prerequisites

* Go 1.21 or newer
* Git

### Build

```bash
go build ./...
```

### Run Tests

```bash
go test ./...
```

### Formatting

Please ensure code is formatted before committing:

```bash
go fmt ./...
```

## Branch Naming

Use descriptive branch names:

* `feature/<name>`
* `fix/<issue>`
* `docs/<topic>`
* `refactor/<component>`

Examples:

```
feature/kafka-load-generator
fix/progress-bar-rendering
docs/update-installation
```

## Commit Messages

Use clear and concise commit messages.

Examples:

```
feat: add kafka producer load test
fix: prevent ticker memory leak
docs: update README installation steps
```

Try to keep commits small and focused.

## Pull Requests

When opening a Pull Request:

* Provide a clear description of the change
* Link the related issue (if applicable)
* Ensure tests pass
* Update documentation if needed

Checklist:

* [ ] Code builds successfully
* [ ] Tests pass
* [ ] Code is formatted
* [ ] Documentation updated (if required)

## Reporting Bugs

If you find a bug, please open an issue and include:

* Description of the problem
* Steps to reproduce
* Expected behavior
* Actual behavior
* Environment (OS, Go version, project version)

## Feature Requests

Before implementing large changes, please open an issue to discuss the proposal.

Include:

* Problem description
* Proposed solution
* Possible alternatives

## Code of Conduct

Please be respectful and constructive in discussions and reviews.

## License

By contributing to this project, you agree that your contributions will be licensed under the project's license.
