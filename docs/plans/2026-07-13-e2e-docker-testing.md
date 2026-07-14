# E2E Testing with Docker Compose Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Switch Playwright E2E tests from manual Go server to Docker Compose stack for production-realistic testing

**Architecture:** Playwright webServer will use `docker compose up` instead of `go run ./cmd/server`. Tests run against the full stack (Postgres, slskd, gonic, caddy, web, worker) on port 8080.

**Tech Stack:** Playwright, Docker Compose, Go backend, PostgreSQL, HTMX frontend

---

## Task 1: Update Playwright Config for Docker

**Files:**
- Modify: `e2e/playwright.config.ts`

**Step 1: Update webServer command to use Docker**

```typescript
webServer: {
  command: 'docker compose up -d --build',
  url: 'http://localhost:8080/api/health',
  reuseExistingServer: !process.env.CI,
  timeout: 180 * 1000, // 3 min for Docker build + startup
  stdout: 'pipe',
  stderr: 'pipe',
  env: {
    // Docker compose env vars (from .env or inline)
    POSTGRES_PASSWORD: 'testpass',
    SLSKD_USERNAME: 'testuser',
    SLSKD_PASSWORD: 'testpass',
    SLSKD_API_KEY: 'test-api-key',
    JWT_SECRET: 'test-secret-key-for-e2e-testing',
    ENVIRONMENT: 'development',
  },
},
```

**Step 2: Update baseURL to port 8080**

```typescript
use: {
  baseURL: 'http://localhost:8080',
  trace: 'on-first-retry',
  screenshot: 'only-on-failure',
},
```

**Step 3: Add teardown command**

```typescript
// Add to config
export default defineConfig({
  // ... existing config
  
  // Add global teardown
  globalTeardown: './teardown.ts',
});
```

**Step 4: Create teardown.ts**

```typescript
// e2e/teardown.ts
import { execSync } from 'child_process';

export default async function globalTeardown() {
  console.log('Tearing down Docker stack...');
  execSync('docker compose down', { stdio: 'inherit' });
}
```

**Step 5: Run tests to verify Docker startup**

Run: `cd e2e && npx playwright test tests/smoke.spec.ts --reporter=list`
Expected: Tests pass against Docker stack

**Step 6: Commit**

```bash
git add e2e/playwright.config.ts e2e/teardown.ts
git commit -m "test: switch E2E tests to Docker Compose stack"
```

---

## Task 2: Update Auth Fixture for Docker Environment

**Files:**
- Modify: `e2e/fixtures/auth.fixture.ts`

**Step 1: Remove rate limit workarounds**

Docker environment has proper Postgres, so rate limiting should work correctly. Remove any test-specific rate limit env vars.

**Step 2: Ensure users are created via API**

The fixture already creates users via API. Verify this works with Postgres backend.

**Step 3: Run auth tests**

Run: `cd e2e && npx playwright test tests/auth.spec.ts --reporter=list`
Expected: All auth tests pass

**Step 4: Commit**

```bash
git add e2e/fixtures/auth.fixture.ts
git commit -m "test: update auth fixture for Docker/Postgres environment"
```

---

## Task 3: Update CI Workflow for Docker

**Files:**
- Modify: `.github/workflows/e2e.yml`

**Step 1: Update CI to use Docker**

```yaml
- name: Start Docker stack
  run: docker compose up -d --build
  
- name: Wait for health
  run: |
    for i in {1..30}; do
      if curl -f http://localhost:8080/api/health; then
        echo "Server ready"
        exit 0
      fi
      sleep 5
    done
    echo "Server failed to start"
    exit 1
```

**Step 2: Remove Go build step**

Remove the Go build step since Docker handles it.

**Step 3: Add Docker teardown**

```yaml
- name: Stop Docker stack
  if: always()
  run: docker compose down
```

**Step 4: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -m "ci: update E2E workflow for Docker Compose"
```

---

## Task 4: Create Docker-specific Test Database Setup

**Files:**
- Create: `e2e/setup-test-db.sh`

**Step 1: Create database setup script**

```bash
#!/bin/bash
# Wait for Postgres to be ready
until docker compose exec -T postgres pg_isready -U netrunner; do
  sleep 1
done

# Create test database
docker compose exec -T postgres psql -U netrunner -c "DROP DATABASE IF EXISTS netrunner_test;"
docker compose exec -T postgres psql -U netrunner -c "CREATE DATABASE netrunner_test;"

echo "Test database ready"
```

**Step 2: Make executable**

```bash
chmod +x e2e/setup-test-db.sh
```

**Step 3: Update playwright.config.ts to run setup**

```typescript
webServer: {
  command: 'docker compose up -d --build && ./setup-test-db.sh',
  // ... rest of config
},
```

**Step 4: Commit**

```bash
git add e2e/setup-test-db.sh e2e/playwright.config.ts
git commit -m "test: add Docker test database setup script"
```

---

## Task 5: Verify Full E2E Suite with Docker

**Files:**
- No file changes

**Step 1: Run all E2E tests**

Run: `cd e2e && npx playwright test --reporter=list`
Expected: All tests pass against Docker stack

**Step 2: Check test coverage**

Verify all 12 phases (DJI-423 through DJI-434) are covered.

**Step 3: Document any Docker-specific issues**

Note any differences between SQLite and Postgres behavior.

**Step 4: Update Linear issues**

Mark DJI-435 as complete, update other phase issues with Docker testing status.

---

## Summary

This plan switches E2E testing from a manual Go server to Docker Compose, providing:
- Production-realistic environment (Postgres, all services)
- Consistent test environment across local and CI
- Proper service integration testing
- Better alignment with how users actually run NetRunner

Estimated time: 30-45 minutes
