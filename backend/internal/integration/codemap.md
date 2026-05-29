# backend/internal/integration/

## Responsibility
Integration test harness for the full pipeline (dockerized slskd, Postgres). Provides utilities for running end-to-end integration tests that validate real-world acquisition flows using dockerized slskd and test Soulseek accounts.

## Design
The integration test harness follows a layered architecture:
- DockerComposeRunner manages docker compose services lifecycle (start/stop/wait for healthy)
- IntegrationTestRunner orchestrates the full test lifecycle (setup -> run -> teardown)
- IntegrationHarness provides test fixtures including database connections, slskd service client, and test data management
- Uses build tags (//go:build integration) to separate integration tests from unit tests
- Leverages environment variables for configuration (INTEGRATION_SLSKD_URL, INTEGRATION_DATABASE_URL, etc.)

## Flow
Test lifecycle follows this pattern:
1. Setup Phase: 
   - SkipIfNoDocker/Compose checks ensure prerequisites are met
   - DockerComposeRunner.Start() brings up services (slskd-integration, integration-db)
   - WaitForHealthy polls until services report healthy/running status
   - Database connections established and migrations run
   - Test data cleanup and fixture creation (quality profiles, etc.)
2. Test Execution:
   - Tests use IntegrationHarness to access services and test data
   - Real slskd API calls validate search, download, and error handling flows
   - Test results validated against expected outcomes
3. Teardown Phase:
   - CleanupTestData removes test artifacts from database
   - DockerComposeRunner.Stop() brings down services and removes volumes
   - Database connections closed

## Integration
Dependencies and connections:
- Docker: Required for containerized slskd and PostgreSQL instances
- slskd: Integration tests validate real Soulseek protocol interactions via dockerized slskd service
- PostgreSQL: Isolated test database for integration test data persistence
- Backend Services: Integrates with internal/services (SlskdService) and internal/database (GORM models)
- Test Packages: Used by integration test files (*_test.go) in this directory
- Environment Variables: Configured via INTEGRATION_* variables for service endpoints and credentials
