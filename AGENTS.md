<coding_guidelines>
# AGENTS.md — Mighty Workspace Guidelines for AI Agents

Welcome! You are operating within the **Mighty** monorepo workspace. This file establishes core context, build/test workflows, code architecture principles, and knowledge management standards for all AI agents working in this repository.

---

## 1. Monorepo Overview

The workspace contains the following main modules:
* **`go-mighty/`**: The Go backend game server (REST API, WebSockets, PostgreSQL 16, Redis 7, OpenTelemetry instrumentation).
* **`mighty-frontend/`**: The Vite + React web and Electron frontend application.
* **`mighty-swift/`**: Native Swift client application codebase.
* **`docs/`**: Engineering documentation, architectural mappings (`osi-safeguards-mapping.md`), and superpower plans/specs (`docs/superpowers/`).

---

## 2. Key Commands & Validation

Before finishing any change, run the narrowest relevant validation checks:

### Backend (`go-mighty`)
* **Run tests**: `cd go-mighty && go test ./...`
* **Run race-detector tests**: `cd go-mighty && go test -race ./...`
* **Validate Alloy/Telemetry config**:
  ```bash
  docker run --rm -v "$PWD/go-mighty/deploy/compose/alloy:/cfg" grafana/alloy:v1.10.0 validate /cfg/config.alloy
  ```

### Frontend (`mighty-frontend`)
* **Run build/lint**: `cd mighty-frontend && npm run build` (or check package.json scripts)

---

## 3. Engineering Standards & Cardinality Rules

1. **Observability Cardinality Rule**:
   > `game_id`, `user_id`, `conn_id`, and client IPs are **span attributes and log fields only — never metric labels, never Loki labels, never span names.**
   Violating this risks exhausting Grafana Cloud series limits (10,000 series cap).

2. **Error Handling & Sanitisation**:
   * Always clamp and sanitise inputs when creating spans or error context.
   * Avoid unhandled errors in core rules (`go-mighty/internal/game/rules.go`).

3. **Database & Migrations**:
   * Schema changes must be accompanied by migration files in `go-mighty/migrations/`.

---

## 4. Knowledge Management & Documentation Workflow

We follow a **Hybrid Documentation Model**:
1. **Code-Near Markdown (`docs/`, `AGENTS.md`, `docs/superpowers/`)**: The source of truth for architectural specs, implementation plans, runbooks, and agent instructions.
2. **Notion**: The source of truth for high-level PRDs, cross-functional roadmaps, and human-centric reviews.

### Docs-with-Code Rule
* Any PR modifying architecture, security controls, API shapes, or deployment pipelines **must** update the corresponding documentation (`docs/` or specs) in the same commit.

### Two-Way Sync Process
* Technical specs and feature designs originate in `docs/superpowers/specs/` and `plans/`.
* High-level summaries and product requirements sync to the Notion **Engineering Docs** database.
</coding_guidelines>
