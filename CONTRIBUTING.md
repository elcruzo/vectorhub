# Contributing to VectorHub

We love your input! We want to make contributing to VectorHub as easy and transparent as possible, whether it's:

- Reporting a bug
- Discussing the current state of the code
- Submitting a fix
- Proposing new features
- Becoming a maintainer

## Development Process

We use GitHub to host code, to track issues and feature requests, as well as accept pull requests.

## Pull Requests

Pull requests are the best way to propose changes to the codebase. We actively welcome your pull requests:

1. Fork the repo and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes.
5. Make sure your code lints.
6. Issue that pull request!

## Development Setup

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- Protocol Buffers compiler (`protoc`)
- Redis (for local development)

### Quick Start

```bash
# Clone the repository
git clone https://github.com/elcruzo/vectorhub.git
cd vectorhub

# Install development tools
make install-tools

# Generate protobuf code
make proto

# Run tests
make test

# Build the project
make build

# Start development environment
docker-compose up -d
```

### Code Style

We use standard Go conventions:

- Run `gofmt` to format your code
- Run `golangci-lint run` to check for issues
- Follow effective Go guidelines
- Write tests for new functionality
- Document exported functions and types

### Testing

- Write unit tests for all new functionality
- Integration tests should be in `test/integration/`
- Benchmark tests should be in `test/benchmark/`
- Ensure all tests pass: `make test`

### Project Structure

```
VectorHub/
├── cmd/vectorhub/          # Main server application
├── internal/               # Private application code
│   ├── server/             # gRPC service implementation
│   ├── storage/            # Storage layer (Redis adapter)
│   ├── shard/              # Sharding and consistent hashing
│   ├── replication/        # Replication management
│   ├── config/             # Configuration management
│   └── metrics/            # Metrics and monitoring
├── api/proto/              # Protocol buffer definitions
├── pkg/client/             # Public client SDK
├── test/                   # Test files
├── configs/                # Configuration files
├── deployments/            # Deployment manifests
└── examples/               # Usage examples
```

## Any Contributions You Make Will Be Under the MIT Software License

In short, when you submit code changes, your submissions are understood to be under the same [MIT License](http://choosealicense.com/licenses/mit/) that covers the project. Feel free to contact the maintainers if that's a concern.

## Report Bugs Using GitHub Issues

We use GitHub issues to track public bugs. Report a bug by [opening a new issue](https://github.com/elcruzo/vectorhub/issues).

**Great Bug Reports** tend to have:

- A quick summary and/or background
- Steps to reproduce
  - Be specific!
  - Give sample code if you can
- What you expected would happen
- What actually happens
- Notes (possibly including why you think this might be happening, or stuff you tried that didn't work)

## Feature Requests

We welcome feature requests! Please open an issue with:

- Clear description of the feature
- Use case and motivation
- Possible implementation approach
- Any breaking changes

## Performance Considerations

VectorHub is designed for high performance. When contributing:

- Profile performance-critical code
- Include benchmark tests for performance improvements
- Document performance characteristics
- Consider memory allocation patterns
- Optimize for concurrent access

## Documentation

- Update README.md if you change functionality
- Add docstrings to exported functions
- Include examples for new APIs
- Update configuration documentation

## Code Review Process

The core team looks at Pull Requests on a regular basis. After feedback has been given we expect responses within two weeks. After two weeks we may close the pull request if it isn't showing any activity.

## Community

- Be respectful and inclusive
- Help newcomers get started
- Share knowledge and best practices
- Follow the [Go Code of Conduct](https://golang.org/conduct)

## License

By contributing, you agree that your contributions will be licensed under its MIT License.

## Getting Help

- Open an issue for bugs and feature requests
- Check existing issues before opening new ones
- Join discussions in issue comments
- Ask questions in the repository discussions

Thank you for contributing to VectorHub! 🚀