# Phase 3 Design: Polish & Hardening

## Overview
Enhance code quality through structured logging patterns and improved test coverage, focusing on the core pipeline (artist tracking, watchlists, acquisition, UI endpoints).

## Goals
1. **Structured Logging** - Improve log messages with contextual fields for better debugging
2. **Error Handling** - Add sentinel errors and better error context
3. **Test Coverage** - Add tests for uncovered services and handlers

## Changes

### 1. Structured Logging
Replace basic `log.Printf` with structured format that includes context:
- Add relevant IDs (job_id, item_id, user_id) to log messages
- Group related fields with `|`
- Consistent prefix format per component

### 2. Error Handling
- Add sentinel errors for recoverable vs non-recoverable failures
- Wrap errors with context for debugging
- Consistent error handling patterns across services

### 3. Test Coverage
**Add tests for:**
- `acoustid_service.go` - AcoustID lookup
- `cache_service.go` - Cache operations  
- `schedules.go` - Schedule CRUD operations
- `dashboard.go` - Dashboard rendering
- `watchlists.go` - Watchlist operations

## Scope
Core pipeline: artist tracking, watchlists, acquisition, UI endpoints

## Timeline
Single PR combining all changes
