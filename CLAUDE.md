# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains a Chaos Operator for Apache Flink, designed to introduce controlled failures and disruptions to Flink clusters for testing resilience and fault tolerance. The operator follows Kubernetes operator patterns and integrates with Flink's ecosystem.

## Development Setup

### Prerequisites
- Go 1.21 or higher
- Kubernetes cluster (minikube, kind, or access to a cloud cluster)
- kubectl installed and configured
- Docker for building images

### Building the Operator
```bash
# Build the operator binary
make build

# Build and push the Docker image
make docker-build
make docker-push

# Deploy to a Kubernetes cluster
make deploy
```

### Running Tests
```bash
# Run unit tests
make test

# Run integration tests
make integration-test

# Run all tests
make test-all
```

## Code Structure

The project follows standard Go module structure with the following key directories:
- `cmd/` - Main application entry points
- `pkg/` - Package code for the operator logic
- `config/` - Kubernetes manifests for deployment
- `manifests/` - Operator deployment configurations
- `test/` - Test files and utilities

## Key Components

### Operator Logic
The operator implements standard Kubernetes controller patterns with:
- Custom resource definition (CRD) for Chaos configurations
- Reconciliation loops for managing Flink chaos scenarios
- Event handling and status updates

### Flink Integration
The operator integrates with Flink through:
- Flink's REST API for cluster management
- Kubernetes service discovery for Flink components
- Resource monitoring and health checks

### Chaos Scenarios
The operator supports various chaos scenarios including:
- Pod termination
- Network partitioning
- Resource exhaustion
- Time skew simulation

## Development Guidelines

### Code Style
- Follow Go best practices and idioms
- Use clear, descriptive variable and function names
- Maintain consistent formatting with gofmt
- Write comprehensive unit tests for all logic

### Testing
- Unit tests should cover core logic and edge cases
- Integration tests should validate operator behavior in Kubernetes
- End-to-end tests should verify chaos scenarios work as expected

### Deployment
- Use standard Kubernetes manifests for operator deployment
- Follow security best practices for RBAC
- Ensure proper resource limits and requests