---
name: testing-watchlist-ui
description: "Guidance for testing the watchlist UI - covers test patterns, setup, and common issues."
---

# Testing Watchlist UI

## Overview

This skill provides guidance for testing the Netrunner watchlist user interface.

## Test Patterns

- Use Playwright for end-to-end browser tests
- Test watchlist creation, editing, and deletion flows
- Verify artist monitoring status indicators
- Test pagination and filtering behavior

## Common Issues

- Ensure test data is seeded before running
- Watch for timing issues with async artist lookups
- Verify accessibility attributes on interactive elements
