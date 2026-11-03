# Exporrters

Prometheus exporters, one per folder. Each exporter is built, versioned and
published independently.

| Folder | Language | Image | Local port |
|---|---|---|---|
| [binance](binance/) | Go | `ghcr.io/gvltodorov/exporrters/binance` | 8088 |
| [mikrotik](mikrotik/) | Go | `ghcr.io/gvltodorov/exporrters/mikrotik` | 9436 |
| [ping](ping/) | Go | `ghcr.io/gvltodorov/exporrters/ping` | 9427 |
| [stock](stock/) | Go | `ghcr.io/gvltodorov/exporrters/stock` | 8087 |
| [weather](weather/) | Go | `ghcr.io/gvltodorov/exporrters/weather` | 9080 |
| [xiaomi](xiaomi/) | Python | `ghcr.io/gvltodorov/exporrters/xiaomi` | 8086 |

Opening an exporter's root URL (for example `http://localhost:9436/`)
redirects to `/metrics`.

Images are published under this repo's namespace. The old per-repo images
(`ghcr.io/gvltodorov/<name>exporrter`) are no longer updated.

## How releases work

Every push to `main` runs [`.github/workflows/ci.yml`](.github/workflows/ci.yml):

1. **Plan:** for each folder with a `Dockerfile`, look for commits that
   touched that folder since its last `<exporter>/vX.Y.Z` tag.
2. **Test:** only the exporters that changed (`go vet` + `go test`, or the
   Python smoke test).
3. **Release:** for each changed exporter, build and push the image tagged
   `X.Y.Z`, `X.Y` and `latest`. Then push the git tag
   `<exporter>/vX.Y.Z` and create a GitHub Release listing that folder's
   commits.

Pull requests run only the plan and test steps, for the folders they touch.

### Version bumps

The bump is worked out from [Conventional Commit](https://www.conventionalcommits.org)
messages. Only commits that touched the exporter's folder count:

| Commit message | Bump |
|---|---|
| `fix(ping): handle DNS timeout` | patch `0.1.1 → 0.1.2` |
| `feat(mikrotik): export PoE metrics` | minor `0.1.0 → 0.2.0` |
| `feat(stock)!: rename metrics` / `BREAKING CHANGE:` | major (minor while on `0.x`) |
| anything else | patch |

A commit that touches two folders releases both exporters. A commit that
touches only docs, `.vscode/` or `compose.yaml` releases nothing.

To see what would be released before you push, run the VS Code task
**release: preview next versions**, or:

```bash
bash .github/scripts/next-version.sh mikrotik
```

To force a release (for example to rebuild on a new base image), go to
**Actions → CI → Run workflow**, then pick the exporter and the bump.

## Local development (VS Code)

1. Open this folder in VS Code and install the recommended extensions.
2. Copy `<exporter>/.env.example` to `<exporter>/.env` and fill in the
   secrets. For ping, also copy `ping/config.example.yml` to
   `ping/config.yml`. All of these are git-ignored.
3. Then use either:
   - **Run and Debug (F5):** pick `binance`, `mikrotik`, `ping`, `stock`,
     `weather` or `xiaomi`.
     - ping needs raw ICMP sockets, so on Windows start VS Code as
       Administrator.
     - xiaomi needs the **xiaomi: create venv** task run once first, with
       Python 3.11. That matches the Docker image; python-miio 0.4.7 is old.
     - binance, stock and weather all listen on `:8080`, so run only one of
       them natively at a time.
   - **Docker:** use the task **docker: up (pick exporter)** or
     **docker: up all**. This builds from source with
     [compose.yaml](compose.yaml), and each exporter gets its own port (see
     the table above).

Test tasks: **test: <exporter>**, **test: all Go exporters**,
**test: leak tests (pick exporter)** and **test: xiaomi**.

## Tests

Every exporter has two kinds of tests, and CI runs them as separate steps:

| | Go (`*_test.go`) | Python (`xiaomi/tests`) |
|---|---|---|
| Unit tests | `go test ./... -race -skip Leak` | `pytest -m "not leak"` |
| Leak tests | `go test ./... -run Leak -count=3` | `pytest -m leak` |

The leak tests check that a long-running exporter doesn't slowly use up
resources:

- **Goroutines:** `goleak` runs after every test in the package and fails if
  anything is left running. It catches refresh loops, prober goroutines and
  HTTP keep-alive readers.
- **Connections:** a counting test server checks that response bodies are
  closed and drained, so connections are reused rather than piling up.
  Hung upstreams must time out.
- **Heap:** each test runs thousands of scrape or refresh cycles and checks
  that the live heap after GC stays flat (`assertNoHeapGrowth` in
  `leakutil_test.go`; `tracemalloc` for Python). Oversized upstream
  responses must be capped.
- **Series count:** interface names, DNS answers, forecast lengths and
  sensors that change must not leave stale metric series behind.

Leak tests have `Leak` in their name (Go) or the `leak` marker (Python),
and run without `-race`, because the race detector makes heap figures
meaningless.

A commit that only changes tests or docs (`*_test.go`, `tests/`, `*.md`)
is still tested, but it doesn't publish a new image.

## Soak test

[soak/](soak/) runs every exporter in Docker against its real upstream for
30 minutes, scrapes them like Prometheus, and checks memory, goroutines,
threads, file descriptors and series counts for steady growth. Run
`python soak/soak.py`, or the VS Code task **soak: run 30 min**. See
[soak/README.md](soak/README.md).

## Adding a new exporter

1. Create `<name>/` with a `Dockerfile`. CI detects it automatically.
2. Add `<name>` to the `workflow_dispatch` options in `ci.yml`, to
   `.github/dependabot.yml`, to `compose.yaml` and to `.vscode/`.
3. Images publish as `ghcr.io/gvltodorov/exporrters/<name>`.

## History

Each folder was imported with its full git history from its original repo,
so `git log -- mikrotik/` and blame work as before.
