# TASK: Ingest Repository, Update agents.md, and Generate New Skills

You are a senior AI agent engineer. Your goal is to deeply analyze the provided
repository, then perform three concrete deliverables:
  1. Ingest and map the repo's full architecture
  2. Update `agents.md` with accurate, current documentation
  3. Create new skill files that enable agents to work effectively with this repo

Work methodically through each phase below. Do not skip steps.

---

## PHASE 1 — REPOSITORY INGESTION & ANALYSIS

### 1.1 — Structural Discovery
Run the following analysis on the repo root:

- List all top-level directories and their purposes
- Identify the primary language(s) and frameworks in use
- Locate and read: `README.md`, `package.json` / `pyproject.toml` / `go.mod`
  (or equivalent), `Makefile` / `justfile`, `docker-compose.yml`, `.env.example`,
  any CI/CD configs (`.github/workflows/`, `.gitlab-ci.yml`, etc.)
- Map the entry points: main executables, CLI commands, server start commands,
  exported library functions
- Identify all external integrations: APIs, databases, message queues, auth
  providers, cloud services

### 1.2 — Dependency & Interface Map
- Extract all direct dependencies and group them by category:
  (core runtime / dev tooling / test / optional)
- List all environment variables the repo reads, with their purpose and whether
  they are required or optional
- Identify all public API surfaces: REST endpoints, gRPC services, exported
  modules, CLI flags, config schema keys
- Note any internal abstractions: interfaces/traits/protocols that agents might
  need to implement or call

### 1.3 — Testing & Linting Infrastructure
- Identify the test runner and test file conventions (location, naming pattern)
- List available test commands and what they cover (unit / integration / e2e)
- Identify the linter, formatter, and type-checker (if any) and their config files
- Note pre-commit hooks or CI quality gates

### 1.4 — Data & Schema Layer
- Locate schema definitions: SQL migrations, Prisma schema, protobuf/OpenAPI
  specs, JSON Schema, Zod/Pydantic models, TypeScript types
- Identify the primary data store(s) and how the repo interacts with them
- Note any seed data scripts or fixtures

### 1.5 — Security & Secrets Posture
- Scan for `.env` files, hardcoded credentials, or secret management patterns
- Identify authentication/authorization mechanisms in the codebase
- Note any rate limiting, input validation, or sanitization patterns

### 1.6 — Synthesis — Write a Repo Summary Block
At the end of Phase 1, produce a structured YAML block:

```yaml
repo_summary:
  name: <repo name>
  primary_language: <language>
  framework: <framework(s)>
  purpose: <1–2 sentence description>
  entry_points:
    - <command or file>
  key_directories:
    - path: <dir>
      purpose: <description>
  external_dependencies:
    - name: <dep>
      category: <API | DB | auth | queue | storage | other>
  environment_variables:
    - name: <VAR>
      required: <true|false>
      purpose: <description>
  test_command: <command>
  lint_command: <command>
  build_command: <command>
  api_surface: <REST | gRPC | library | CLI | mixed>
```

---

## PHASE 2 — UPDATE agents.md

Locate `agents.md` at the repo root (create it if missing). Rewrite or update it
using the structure below. Preserve any existing human-written sections that are
still accurate; annotate outdated sections with `<!-- OUTDATED: <reason> -->`.

### agents.md Target Structure

```markdown
# Agents Guide — <Repo Name>

> Last updated by Codex ingestion: <ISO date>

## Overview
<!-- 2–3 paragraphs: what this repo does, who the agents working with it are,
     and what level of autonomy is appropriate (read-only, read-write, deploy) -->

## Repository Map
<!-- Table: directory | type | purpose | agent-relevant? -->

## Quickstart for Agents
<!-- Ordered steps to clone, configure env, install deps, run tests, start server -->
1. Clone and enter repo
2. Copy `.env.example` → `.env` and populate required vars
3. Install dependencies: `<command>`
4. Run database migrations (if any): `<command>`
5. Run tests to confirm baseline: `<command>`
6. Start dev server: `<command>`

## Environment Variables
<!-- Table: VAR_NAME | required | default | purpose -->

## Available Commands
<!-- Table: command | purpose | when to use -->

## API / Interface Reference
<!-- For each major endpoint or exported function:
     - Name / route
     - Method / signature
     - Parameters
     - Returns
     - Auth required? -->

## Data Models
<!-- Key entities, their fields, and relationships — sourced from schema files -->

## Testing Guide
<!-- How to run unit, integration, and e2e tests; how to write new tests;
     where fixtures live; how to use mocks -->

## Common Agent Tasks
<!-- Concrete, copy-pasteable examples for the most frequent agent workflows:
     - Adding a new feature
     - Fixing a bug
     - Running a migration
     - Updating a dependency
     - Deploying (if applicable) -->

## Pitfalls & Gotchas
<!-- Known footguns, non-obvious constraints, flaky tests, env quirks -->

## Skill Index
<!-- Links to each skill file created in Phase 3 -->
```

---

## PHASE 3 — CREATE NEW SKILL FILES

Create a `skills/` directory at the repo root (if it doesn't exist). Generate the
following skill files. Each skill file must be self-contained: an agent with zero
prior context should be able to read the skill and immediately act correctly.

### Skill File Format

Every skill file must follow this template:

```markdown
***
skill: <skill-name>
version: 1
repo: <repo name>
language: <primary language>
tags: [<relevant tags>]
***

# Skill: <Human-Readable Title>

## Purpose
<!-- One sentence: what this skill enables an agent to do -->

## Prerequisites
<!-- What must be true before using this skill:
     env vars set, deps installed, service running, etc. -->

## Core Concepts
<!-- Key abstractions, patterns, or domain knowledge needed -->

## Step-by-Step Procedures
<!-- Numbered, copy-pasteable steps for the primary workflow -->

## Code Patterns
<!-- Canonical code snippets showing the correct way to do common things -->

## Validation
<!-- How to verify the skill was applied correctly: tests to run, outputs to check -->

## Edge Cases & Error Handling
<!-- Known failure modes and how to handle them -->

## References
<!-- Links to relevant files in the repo: schema, config, test examples -->
```

### Required Skills to Generate

Generate ALL of the following that are applicable to this repo:

#### 3.1 — `skills/repo-setup.md`
Complete environment setup from scratch: cloning, env config, dependency
installation, database setup, seed data, and first successful test run.

#### 3.2 — `skills/run-tests.md`
How to run the full test suite, individual test files, specific test cases,
and tests by tag or pattern. Includes how to interpret output and debug failures.

#### 3.3 — `skills/add-feature.md`
The canonical workflow for adding a new feature: where to add code, how to
follow existing patterns, required tests, linting steps, and PR checklist.

#### 3.4 — `skills/fix-bug.md`
Systematic approach to reproducing, isolating, fixing, and verifying a bug
in this specific codebase. Includes how to search for related code and tests.

#### 3.5 — `skills/data-model.md`
How to work with the data layer: reading/writing to the database, adding
new fields or tables, running migrations, and handling schema evolution.
*(Skip if repo has no persistent data layer.)*

#### 3.6 — `skills/api-usage.md`
How to call this repo's API (or use its exported library interface): auth,
request construction, response handling, pagination, error codes.
*(Skip if repo has no external-facing API or library surface.)*

#### 3.7 — `skills/deploy.md`
How to build, package, and deploy the application: environment promotion,
secrets management, rollback procedures, health checks.
*(Skip if repo has no deployment configuration.)*

#### 3.8 — `skills/debugging.md`
How to enable verbose logging, attach a debugger, read stack traces in this
repo's style, and use any repo-specific debug tooling or flags.

#### 3.9 — `skills/dependency-management.md`
How to add, update, and audit dependencies; how to resolve lockfile conflicts;
security vulnerability scanning process for this repo.

#### 3.10 — `skills/<domain-specific>.md`
Identify 1–3 domain-specific workflows that are unique to this repo (e.g.,
"processing a webhook", "running a batch job", "training a model", "generating
a report"). Create one skill file per workflow using the standard template.
Name each file descriptively: `skills/<workflow-name>.md`.

---

## PHASE 4 — FINAL VALIDATION

Before completing, perform these checks:

1. **Completeness check** — Every section of `agents.md` is populated (no
   `<!-- TODO -->` placeholders left).
2. **Accuracy check** — All commands in `agents.md` and skill files were
   verified against actual files in the repo (no guessed commands).
3. **Skill index check** — The `## Skill Index` section in `agents.md` lists
   every skill file created with a relative path and one-line description.
4. **Orphan check** — Every generated skill file is referenced in `agents.md`.
5. **Secret scan** — No environment variable values, API keys, passwords, or
   tokens were written into any generated file. Only variable *names* and
   *purposes* are documented.
6. **Self-test** — Using only the generated `agents.md` and skill files,
   confirm an agent could reproduce: (a) a clean dev setup, (b) a passing
   test run, and (c) one end-to-end task from `## Common Agent Tasks`.

Report the results of each check as a checklist at the end of your output:
- [ ] Completeness
- [ ] Accuracy
- [ ] Skill index
- [ ] Orphan check
- [ ] Secret scan
- [ ] Self-test

---

## OUTPUT SUMMARY

When all phases are complete, output:
✅ Ingestion complete
📁 Files created/modified:
- agents.md (updated)
- skills/repo-setup.md
- skills/run-tests.md
- ... (list all)

📊 Repo summary:
<paste yaml block from Phase 1.6>

⚠️ Items requiring human review:
- <any ambiguous sections, missing env vars, or gaps found>

