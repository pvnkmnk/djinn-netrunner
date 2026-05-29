# backend/internal/testutil/

## Responsibility
Test helpers and mocks shared across test packages. Provides reusable test doubles and utilities that enable consistent testing practices throughout the NetRunner codebase.

## Design
The testutil package follows these patterns:
- MockProvider: Implements interfaces.WatchlistProvider as a test double that returns pre-configured tracks, snapshot ID, and error
- Compile-time interface checks ensure mock implementations satisfy required interfaces
- Simple, focused implementations that return configured values without complex logic
- Designed for dependency injection in tests to isolate units under test
- Minimal external dependencies to keep test setup straightforward

## Flow
Test usage follows this pattern:
1. Test setup: Create MockProvider instance with desired return values
   - Tracks: []map[string]string representing track metadata from watchlist sources
   - SnapID: String representing watchlist snapshot/version identifier
   - Err: Error to simulate failure conditions when needed
2. Injection: Pass MockProvider to code under test that expects interfaces.WatchlistProvider
3. Exercise: Code calls FetchTracks() or ValidateConfig() on the provider interface
4. Verification: Test asserts that code under test handled the returned values correctly
5. Teardown: No special cleanup required as mocks hold no external resources

## Integration
Connections to other parts of the system:
- Interfaces: Implements interfaces.WatchlistProvider contract from backend/internal/interfaces/
- Database: References backend/internal/database/types for Watchlist struct (used in method signature)
- Test Packages: Used by unit tests in:
  - backend/internal/services/* (particularly watchlist-related services)
  - backend/internal/api/* (handlers that depend on watchlist providers)
  - backend/internal/agent/* (agent facade implementations)
  - Any package needing to test watchlist provider interactions without external dependencies
- Build: No special build tags required; included in standard test builds
