# Integration Testing for NetRunner

This directory contains end-to-end integration tests for NetRunner that validate real-world acquisition flows using dockerized slskd and test Soulseek accounts.

## Overview

The integration test harness provides:

- **Dockerized slskd**: A containerized Soulseek daemon for testing
- **Test database**: Isolated PostgreSQL instance for integration tests
- **Real Soulseek protocol testing**: Validates actual search and download flows
- **End-to-end acquisition validation**: Tests complete job processing pipelines

## Prerequisites

1. **Docker and Docker Compose** installed
2. **Go 1.25+** installed
3. **Test Soulseek account** (optional, for download tests)

## Quick Start

### 1. Start Integration Services

```bash
docker compose -f docker-compose.integration.yml up -d
```

This starts:
- `slskd-integration`: Dockerized Soulseek daemon on port 15030
- `integration-db`: PostgreSQL test database on port 15432

### 2. Run Integration Tests

```bash
# Run all integration tests
go test ./backend/internal/integration/... -v -tags=integration

# Run specific integration test
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdEndToEndSearch

# Run with real Soulseek credentials (for download tests)
INTEGRATION_SLSKD_USERNAME=your_username \
INTEGRATION_SLSKD_PASSWORD=your_password \
go test ./backend/internal/integration/... -v -tags=integration
```

### 3. Stop Integration Services

```bash
docker compose -f docker-compose.integration.yml down -v
```

## Test Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `INTEGRATION_SLSKD_URL` | `http://localhost:15030` | slskd API endpoint |
| `INTEGRATION_SLSKD_API_KEY` | `test-api-key-for-integration` | slskd API key |
| `INTEGRATION_DATABASE_URL` | `postgresql://testuser:testpass@localhost:15432/netrunner_integration?sslmode=disable` | Test database URL |
| `INTEGRATION_SLSKD_USERNAME` | (none) | Soulseek username for download tests |
| `INTEGRATION_SLSKD_PASSWORD` | (none) | Soulseek password for download tests |
| `SKIP_INTEGRATION_TESTS` | `false` | Set to `true` to skip all integration tests |

## Test Suites

### 1. End-to-End Search Tests

Validates real Soulseek search functionality:

```bash
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdEndToEndSearch
```

Tests:
- Search query processing
- Result scoring algorithm
- Quality profile filtering
- Concurrent searches

### 2. Health Check Tests

Validates slskd connectivity:

```bash
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdHealthCheck
```

### 3. Download Flow Tests (Requires Real Account)

Validates download functionality:

```bash
# Requires valid Soulseek credentials
INTEGRATION_SLSKD_USERNAME=testuser \
INTEGRATION_SLSKD_PASSWORD=testpass \
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdDownloadLifecycle
```

### 4. Error Handling Tests

Validates error scenarios:

```bash
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdErrorHandling
```

### 5. Concurrent Operations Tests

Validates thread safety:

```bash
go test ./backend/internal/integration/... -v -tags=integration -run TestSlskdConcurrentOperations
```

## Test Architecture

### Integration Harness

The `IntegrationHarness` struct provides:

- Database connection with automatic migrations
- slskd service client
- Test data cleanup
- Service health monitoring
- Test fixtures (quality profiles, jobs, job items)

### Docker Compose Configuration

The `docker-compose.integration.yml` defines:

- **slskd-integration**: Latest slskd/slskd image with configurable credentials
- **integration-db**: PostgreSQL 16 for test isolation
- **Network**: Dedicated bridge network for service communication
- **Volumes**: Persistent data for test continuity

### Test Lifecycle

```
1. Setup Phase
   - Connect to test database
   - Run migrations
   - Clean up previous test data
   - Wait for slskd to be healthy
   - Create test fixtures

2. Test Execution
   - Run test scenarios
   - Validate results
   - Log test progress

3. Teardown Phase
   - Clean up test data
   - Close database connections
   - Cleanup resources
```

## Continuous Integration

### GitHub Actions Example

```yaml
name: Integration Tests

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Start integration services
        run: docker compose -f docker-compose.integration.yml up -d
        
      - name: Wait for services
        run: |
          timeout 120 bash -c 'until curl -s http://localhost:15030/health; do sleep 2; done'
        
      - name: Run integration tests
        run: go test ./backend/internal/integration/... -v -tags=integration -timeout 10m
        
      - name: Stop services
        run: docker compose -f docker-compose.integration.yml down -v
```

## Test Data Management

### Test Quality Profiles

Integration tests create test quality profiles:

- **Integration Test Profile**: Default profile with MP3/FLAC support, 192kbps minimum
- **High Quality Profile**: FLAC preference, 320kbps minimum

### Test Jobs

Jobs are created with `created_by = 'integration_test'` for easy cleanup.

## Troubleshooting

### Services Won't Start

```bash
# Check service logs
docker compose -f docker-compose.integration.yml logs slskd-integration
docker compose -f docker-compose.integration.yml logs integration-db

# Verify ports are available
lsof -i :15030
lsof -i :15432
```

### Tests Timeout

Increase timeout for slow networks:

```bash
go test ./backend/internal/integration/... -v -tags=integration -timeout 20m
```

### Connection Refused

Ensure services are healthy:

```bash
# Check slskd health
curl http://localhost:15030/health

# Check database
pg_isready -h localhost -p 15432
```

### Permission Denied

On Linux, you may need to run Docker commands with sudo or add your user to the docker group.

## Security Notes

1. **Never commit real Soulseek credentials** to the repository
2. Use environment variables or secrets management for credentials
3. Test credentials should have limited privileges
4. Integration database is isolated from production

## Extending Integration Tests

To add new integration tests:

1. Create test file in `backend/internal/integration/`
2. Add `//go:build integration` build tag
3. Use `SetupIntegrationHarness(t)` for setup
4. Call `harness.Teardown(t)` for cleanup
5. Add test to CI pipeline

Example:

```go
//go:build integration

package integration

import "testing"

func TestMyNewIntegration(t *testing.T) {
    harness := SetupIntegrationHarness(t)
    defer harness.Teardown(t)
    
    // Your test code here
    results, err := harness.Slskd.Search("test query", 30, nil)
    if err != nil {
        t.Fatalf("Search failed: %v", err)
    }
    
    if len(results) == 0 {
        t.Error("Expected search results")
    }
}
```

## Development Tips

1. **Run unit tests first**: Unit tests are faster and catch most issues
2. **Use -run flag**: Run specific integration tests during development
3. **Check logs**: Docker logs show detailed slskd activity
4. **Parallel execution**: Integration tests can run in parallel with proper isolation
5. **Resource cleanup**: Always use `defer harness.Teardown(t)` to clean up

## Related Documentation

- [Backend README](../backend/README.md)
- [API Documentation](../docs/API.md)
- [Development Guide](../AGENTS.md)
