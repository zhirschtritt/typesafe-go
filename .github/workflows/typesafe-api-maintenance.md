---
description: Keep the Go SDK aligned with TypeSafe's public API and official SDKs.
intent: Detect concrete TypeSafe API additions or compatibility gaps and propose the smallest verified SDK update for maintainer review.
engine:
  id: copilot
  model: claude-sonnet-5
max-ai-credits: 100
max-turns: 60
on:
  schedule: weekly on monday
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[typesafe-api]"'

permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read

tools:
  github:
    mode: gh-proxy
    toolsets: [default]

network:
  allowed:
    - defaults
    - github
    - go
    - api.typesafe.ai
    - docs.typesafe.ai
jobs:
  detection:
    if: needs.agent.result == 'success'
  safe_outputs:
    if: needs.agent.result == 'success'

safe-outputs:
  create-pull-request:
    title-prefix: "[typesafe-api] "
    branch-prefix: "copilot/typesafe-api/"
    draft: true
    max: 1
    allowed-files:
      - "**/*.go"
      - "README.md"
      - "CONTRIBUTING.md"
      - "go.mod"
      - "go.sum"
---

Maintain this unofficial Go SDK against current TypeSafe behavior.

1. Read `README.md` and `CONTRIBUTING.md`, then inspect the implementation and tests.
2. Fetch `https://api.typesafe.ai/openapi.json` as the canonical API contract.
3. Inspect recent relevant changes in the official repositories with read-only `gh` commands:
   - `typesafe-ai/typesafe-sdk-js`
   - `typesafe-ai/typesafe-sdk-python`
4. Compare their public behavior with this SDK. Look only for concrete, externally evidenced gaps: new endpoints, request or response fields, question or answer variants, model metadata, validation rules, error semantics, or documented capabilities.
5. Ignore style differences, internal refactors, generated-code churn, and features not supported by the OpenAPI contract or both official documentation and an official SDK.
6. If this SDK is already compatible, or evidence is ambiguous, call `noop` with a short reason. Do not create speculative work.
7. Otherwise implement one focused compatibility update. Preserve additive JSON forward compatibility and existing public APIs unless the upstream contract requires a breaking change.
8. Add behavior-focused tests for the changed contract. Update `README.md` only when user-facing behavior changes.
9. Run `gofmt` on changed Go files, `go test ./...`, `go vet ./...`, and `go test -race ./...`. Review the diff for unrelated edits and secrets.
10. Only if every changed-path check passes, create one draft pull request through the `create-pull-request` safe output. Explain the upstream evidence, SDK behavior added or corrected, compatibility impact, and exact commands run. Never merge it.

Keep the change small. One upstream concern per pull request.
