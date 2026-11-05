# 🐦 Budge

An opinionated tool for helping Kiwis with money and asset management. 

Budge is powered by [Go](https://go.dev/), [Echo](https://echo.labstack.com/), 
[HTMX](https://htmx.org/), [Alpine](https://alpinejs.dev/), [Bootstrap](https://getbootstrap.com/), [Air](https://github.com/air-verse/air) and [bbolt](https://github.com/etcd-io/bbolt).

## Getting Started

We integrate with [Akahu](https://www.akahu.nz/) in order to allow you to see your accounts and transactions. You will need to 
create a profile, verify your identity, and set up MFA in order to create a personal app for use with Budge. 

More information can be found [here](https://developers.akahu.nz/docs/personal-apps)

Once you have a user and app token, navigate to settings, load in your credentials, and perform your first sync!

## Deployment

The Budge Docker image is published to the [GitHub Container Registry](https://github.com/TheQueenIsDead/budge/pkgs/container/budge).

```shell
# Latest release
docker pull ghcr.io/thequeenisdead/budge:latest

# Or pin to a specific version
docker pull ghcr.io/thequeenisdead/budge:1.12.0
```

Images are tagged with the full version (`1.12.0`), the major and minor version
(`1.12`), the major version (`1`), and `latest`.

### Configuration

Budge is configured with environment variables, all optional.

| Variable | Default | What it does |
|---|---|---|
| `BUDGE_BOLT_PATH` | `.` | The directory Budge keeps its database, and its telemetry, in. |
| `BUDGE_TELEMETRY` | `true` | Set to `false` to turn telemetry off: nothing is traced or kept, and the admin page is gone. OpenTelemetry's own `OTEL_SDK_DISABLED=true` does the same. |
| `BUDGE_TELEMETRY_RETENTION` | `7d` | How long metrics, logs and traces are kept in `budge.telemetry.db`, such as `36h` or `14d`. `0` keeps them in memory only, until a restart. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | | Also sends telemetry to an OpenTelemetry collector, along with the other standard `OTEL_*` variables. |
| `AKAHU_USER_TOKEN`, `AKAHU_APP_TOKEN` | | Akahu tokens to use until some are saved in settings. |

> **Note:** Budge was previously published to Docker Hub. That repository is no
> longer updated, so please switch to `ghcr.io/thequeenisdead/budge`.

## Development

To develop Budge you will need an Akahu account, as well as Golang and pnpm installed:
- Akahu: https://developers.akahu.nz/docs/personal-apps
- Golang: https://go.dev/doc/install
- pnpm: https://pnpm.io/installation

The libraries the pages load, such as htmx and Bootstrap, are listed in
`web/package.json`. `pnpm install` in `web` fetches them into
`web/node_modules`, which `web/embed.go` embeds in the binary, so Budge does
not build without it. Run it again after the libraries change; Air does so
before each build.

Air is recommended in order to perform live-reloading of the build while changing files.

```shell
git clone https://github.com/TheQueenIsDead/budge.git
cd budge
pnpm install --dir web
go install github.com/air-verse/air@latest
air
# Budge is now accessible at http://localhost:1337
```

### Browser tests

The tests in `e2e/` run Budge's image and drive it in Chrome, both in
containers, on made-up data. They need Docker or Podman, and an image to test:

```shell
docker build -t budge:e2e .
BUDGE_E2E_IMAGE=budge:e2e go test -tags e2e ./e2e/...
```

With Podman, point the tests at its socket, and turn off the container
testcontainers starts to clean up after a crashed run, which expects Docker's
network names:

```shell
podman build -t budge:e2e .
export DOCKER_HOST=unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')
export TESTCONTAINERS_RYUK_DISABLED=true
BUDGE_E2E_IMAGE=budge:e2e go test -tags e2e ./e2e/...
```

Add `-artifacts` to keep a screenshot and the HTML of each page a failed test
was on, in `_artifacts`.
