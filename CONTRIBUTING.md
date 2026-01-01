# Contributing to Stevedore

First off, thank you for considering contributing to Stevedore! It's people like you that make open source work.

## Code of Conduct

This project and everyone participating in it is governed by our commitment to providing a welcoming and inclusive environment. By participating, you are expected to:

- Use welcoming and inclusive language
- Be respectful of differing viewpoints and experiences
- Gracefully accept constructive criticism
- Focus on what is best for the community
- Show empathy towards other community members

Unacceptable behavior may be reported to the project maintainers.

## Getting Started

Stevedore is a lightweight container orchestration system written in Go. Before diving in, familiarize yourself with:

- [Go](https://go.dev/doc/) (1.25+)
- [Docker](https://docs.docker.com/) and [Docker Compose](https://docs.docker.com/compose/)
- The project [README](README.md) for architecture overview

### Good First Issues

Looking for a place to start? Check out issues labeled [`good first issue`](https://github.com/jonnyzzz/stevedore/labels/good%20first%20issue) — these are specifically curated for new contributors.

## Development Setup

### Prerequisites

- Go 1.25 or later
- Docker 24+ with Compose plugin
- Docker Buildx (optional; BuildKit is used when available)
- Git
- A C toolchain (CGO) for `go-sqlcipher` (e.g. `gcc`/`clang` + libc headers)
- Make (optional, but recommended)

## How to Contribute

### Reporting Bugs

Before creating a bug report, please check existing issues to avoid duplicates. When filing a bug report, include:

- **Clear title** describing the issue
- **Steps to reproduce** the behavior
- **Expected behavior** vs what actually happened
- **Environment details**: OS, Docker version, Go version, Stevedore version, hardware info
- **Logs** if applicable (sanitize any secrets!)
- **Configuration** (sanitized) if relevant

### Suggesting Features

Feature requests are welcome! Please:

- Check existing issues and discussions first
- Describe the problem your feature would solve
- Explain your proposed solution
- Consider alternatives you've thought about
- Note if you're willing to implement it yourself

### Submitting Code

1. **Fork the repository** and create your branch from `main`
2. **Make your changes** following our coding guidelines
3. **Add tests** for any new functionality
4. **Update documentation** if needed
5. **Submit a pull request**

## Pull Request Process

### Before Submitting

- [ ] Code compiles without errors (`make build`)
- [ ] All tests pass (`make test`)
- [ ] Integration tests pass (`make test-integration`) (requires Docker + outbound network)
- [ ] Code is formatted (`make fmt`)
- [ ] New code has appropriate test coverage
- [ ] Documentation is updated if needed
- [ ] Commit messages are clear and descriptive

### PR Guidelines

1. **Keep PRs focused** — one feature or fix per PR
2. **Write a clear description** explaining what and why
3. **Reference related issues** using `Fixes #123` or `Relates to #456`
4. **Be responsive** to review feedback
5. **Squash commits** if requested before merge

### Review Process

- A maintainer will review your PR, usually within a few days
- Address any requested changes
- Once approved, a maintainer will merge your PR
- Your contribution will be included in the next release

## Coding Guidelines

### Go Style

We follow standard Go conventions:

- Run `gofmt` on all code (automated via `make fmt`)
- Follow [Effective Go](https://go.dev/doc/effective_go)
- Keep changes testable (prefer small, verifiable diffs)

### Changelog

We maintain a changelog following [Keep a Changelog](https://keepachangelog.com/) format. Add entries under "Unreleased" for your changes:

```markdown
## [Unreleased]

### Added
- New feature X that does Y (#123)

### Fixed
- Bug where Z happened under certain conditions (#456)
```

## Community

### Getting Help

- **GitHub Issues** — for bugs and feature requests
- **GitHub Discussions** — for questions and general discussion

### Recognition

Contributors are recognized in:

- Git history (forever!)
- Release notes for significant contributions
- README acknowledgments for major features

---

Thank you for contributing to Stevedore! Every contribution, no matter how small, makes a difference.
