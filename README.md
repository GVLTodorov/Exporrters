# Exporrters

Prometheus exporters, one per folder. Each exporter is built, versioned and
published independently.

| Folder | Language | Image | Local port |
|---|---|---|---|
| [mikrotik](mikrotik/) | Go | `ghcr.io/gvltodorov/mikrotikexporrter` | 9436 |
| [ping](ping/) | Go | `ghcr.io/gvltodorov/pingexporrter` | 9427 |
| [stock](stock/) | Go | `ghcr.io/gvltodorov/stockexporrter` | 8087 |
| [weather](weather/) | Go | `ghcr.io/gvltodorov/weatherexporrter` | 9080 |
| [xiaomi](xiaomi/) | Python | `ghcr.io/gvltodorov/xiaomiexporrter` | 8086 |

Image names are the same as when each exporter lived in its own repo, so
existing `docker-compose` files keep working.

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
   - **Run and Debug (F5):** pick `mikrotik`, `ping`, `stock`, `weather` or
     `xiaomi`.
     - ping needs raw ICMP sockets, so on Windows start VS Code as
       Administrator.
     - xiaomi needs the **xiaomi: create venv** task run once first, with
       Python 3.11. That matches the Docker image; python-miio 0.4.7 is old.
     - stock and weather both listen on `:8080`, so run only one of them
       natively at a time.
   - **Docker:** use the task **docker: up (pick exporter)** or
     **docker: up all**. This builds from source with
     [compose.yaml](compose.yaml), and each exporter gets its own port (see
     the table above).

Test tasks: **test: <exporter>** and **test: all Go exporters**.

## Adding a new exporter

1. Create `<name>/` with a `Dockerfile`. CI detects it automatically.
2. Add `<name>` to the `workflow_dispatch` options in `ci.yml`, to
   `.github/dependabot.yml`, to `compose.yaml` and to `.vscode/`.
3. Images publish as `ghcr.io/gvltodorov/<name>exporrter`.

## History

Each folder was imported with its full git history from its original repo,
so `git log -- mikrotik/` and blame work as before.
