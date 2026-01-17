# Contributing to KubeOpenCode

We welcome contributions! This document provides guidelines for contributing to KubeOpenCode.

## Getting Started

Before contributing, please:

1. Review the [Architecture Documentation](docs/architecture.md)
2. Set up your [Local Development Environment](docs/local-development.md)
3. Read through [CLAUDE.md](CLAUDE.md) for detailed development guidelines

## Commit Standards

Always use signed commits with the `-s` flag (Developer Certificate of Origin):

```bash
git commit -s -m "feat: add new feature"
```

### Commit Message Format

Follow the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>: <description>

[optional body]

Signed-off-by: Your Name <your.email@example.com>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

## Pull Requests

### Before Submitting

1. Check for upstream repositories first
2. Create PRs against upstream, not forks
3. Ensure your branch is up to date with main
4. Run all checks locally:

```bash
make lint    # Run linter
make test    # Run unit tests
make verify  # Verify generated code is up to date
```

### PR Guidelines

- Use descriptive titles and comprehensive descriptions
- Reference related issues (e.g., "Fixes #123")
- Keep PRs focused - one feature or fix per PR
- Update documentation if your changes affect user-facing behavior
- Add tests for new functionality

### PR Description Template

```markdown
## Summary
Brief description of the changes

## Related Issues
Fixes #<issue-number>

## Test Plan
- [ ] Unit tests pass
- [ ] Integration tests pass (if applicable)
- [ ] E2E tests pass (if applicable)
- [ ] Manual testing performed
```

## Code Standards

### Go Code

- Follow standard Go conventions
- Use `gofmt` and `golint`
- Write comments in English
- Document exported types and functions
- Use meaningful variable and function names

### Testing

- Write tests for new features
- Maintain test coverage
- Use table-driven tests where appropriate

```bash
# Run unit tests
make test

# Run integration tests (uses envtest)
make integration-test

# Run E2E tests (uses Kind cluster)
make e2e-teardown && make e2e-setup && make e2e-test
```

### API Changes

When modifying CRD definitions:

1. Update `api/v1alpha1/types.go`
2. Run `make update` to regenerate CRDs and deepcopy
3. Run `make verify` to ensure everything is correct
4. Update documentation in `docs/architecture.md`
5. Update integration tests in `internal/controller/*_test.go`
6. Update E2E tests in `e2e/`

## Development Workflow

### Building

```bash
make build        # Build the controller
make docker-build # Build Docker image
```

### Running Locally

```bash
make run  # Run controller locally (requires kubeconfig)
```

### Testing Changes

See [Local Development Guide](docs/local-development.md) for detailed instructions on:
- Setting up a Kind cluster
- Building and loading images
- Running E2E tests

## Reporting Issues

When reporting issues:

1. Search existing issues to avoid duplicates
2. Use a clear, descriptive title
3. Include:
   - Steps to reproduce
   - Expected behavior
   - Actual behavior
   - Environment details (Kubernetes version, OS, etc.)
   - Relevant logs or error messages

## Getting Help

- Review existing documentation in `docs/`
- Check [Troubleshooting Guide](docs/troubleshooting.md)
- Open a [GitHub Discussion](https://github.com/kubeopencode/kubeopencode/discussions)
- Review existing issues and PRs

## License

By contributing to KubeOpenCode, you agree that your contributions will be licensed under the Apache License 2.0.
