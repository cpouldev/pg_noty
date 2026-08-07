# Repository Guidelines

## General guidelines

1. Ask, don't assume. If something is unclear, ask before writing a single line. Never make silent assumptions about intent, architecture, or requirements. When running unattended, pick the most reasonable interpretation, proceed, and record the assumption rather than blocking.

2. Implement the simplest solution for simple problems, better solutions for harder problems. Do not over-engineer or add flexibility that isn't needed yet.

3. Don't touch unrelated code but please do surface bad code or design smells you discover with me so we can address them as a separate issue.

4. Flag uncertainty explicitly. If you're unsure about something, see point 1 above. If it makes sense to do so, conduct a small, localised and low-risk experiment and bring the hypothesis and results to me to discuss. Confidence without certainty causes more damage than admitting a gap.

5. I'm always open to ideas on better ways to do things. Please don't hesitate to suggest a better way, or one that has long lasting impact over a tactical change. (as a few examples)


## Project Structure & Module Organization

`cmd/pg_noty/` builds the CLI. Under `internal/`, `cli` composes commands, `config` validates YAML, `schema` owns storage, `source` manages triggers and queues, `reconcile` plans database changes, `delivery` sends webhooks, and `goartifact` supports source checks. Tests sit beside their packages; fixtures and golden files belong in each package's `testdata/`. Use `demo/` and `docker-compose.yml` for the runnable example. Design records live in `.specs/`.

## Build, Test, and Development Commands

- `go build ./...` compiles the repository.
- `go run ./cmd/pg_noty --help` starts CLI help locally.
- `go test -count=1 ./...` runs the container-free suite without cached results.
- `go test -p=1 -tags=integration -count=1 ./...` runs PostgreSQL integration tests through Testcontainers; package serialization prevents competing Compose stacks, and a working Docker daemon is required.
- `go vet ./...` and `go vet -tags=integration ./...` check both build tiers.
- `go mod tidy -diff` verifies that module files are settled.
- Set `GO_VERSION` from `go.mod`, then run `docker compose up --build` to launch the demo.

## Coding Style & Naming Conventions

Use Go 1.25 and format changed files with `gofmt -w <files>`; `gofmt` handles tabs and imports. Name exported identifiers in PascalCase, unexported identifiers in camelCase, and files with concise lowercase names. Keep package dependencies pointed inward and preserve the boundaries in each `doc.go`. For configuration work, keep schema keys and sensitivity metadata in `internal/config/schema.go`.

## Testing Guidelines

Use the standard `testing` package. Name tests `TestBehavior`, fuzz targets `FuzzInvariant`, and tagged files `*_integration_test.go` with `//go:build integration`. Add fixtures under `testdata/`; update golden data only through the test-provided update command. No percentage threshold is declared, but the full plain and tagged suites are release gates. Before adding tests, read the applicable defect-derived rules in `.claude/rules/`.

CI builds the multi-platform image only for ordinary pull requests and manual runs. Release Please PRs and `main` pushes run the Go gates without rebuilding it; `.github/workflows/publish.yml` performs the single release build from the resulting tag.

## Commit & Pull Request Guidelines

Release Please derives versions from commits on `main`. Use Conventional Commits for every commit and for PR titles that may become squash commits: `fix(config): reject an invalid listener` releases a patch, `feat(delivery): add retry controls` a minor, and `feat(cli)!: change command output` a major. A `BREAKING CHANGE: <description>` footer also marks a major release. Prefer squash merges, keep descriptions imperative and lowercase, and do not create release tags or edit versions manually.

Pull requests should explain the behavior and risk, link the relevant issue or `.specs` task, and list each verification command run. Include CLI output or screenshots when user-visible behavior changes, and call out skipped integration tests explicitly. Merge the generated Release Please PR when the accumulated changes are ready to publish.

## Security & Configuration

Keep credentials out of tracked YAML. Follow `demo/listeners.yaml` by referencing environment variables such as `${DATABASE_URL}` and `${WEBHOOK_SECRET}`, and preserve redaction boundaries in errors and logs.

## Agent-Specific Instructions

### Use Serena MCP for Semantic Code Analysis instead of regular code search and editing

Serena MCP is available for advanced code retrieval and editing capabilities.

**When to use Serena:**
- Symbol-based code navigation (find definitions, references, implementations)
- Precise code manipulation in structured codebases
- Prefer symbol-based operations over file-based grep/sed when available

**Key tools:**
- `find_symbol` - Find symbol by name across the codebase
- `find_referencing_symbols` - Find all symbols that reference a given symbol
- `get_symbols_overview` - Get overview of top-level symbols in a file
- `read_file` - Read file content within the project directory

**Usage notes:**
- Memory files can be manually reviewed/edited in `.serena/memories/`
