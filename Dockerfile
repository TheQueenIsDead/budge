# pnpm's and Go's versions. The _build workflow passes the ones
# web/package.json and go.mod name; a build by hand takes the latest.
ARG PNPM_VERSION=latest
ARG GO_VERSION=latest

# The page's libraries, installed with pnpm from web/package.json, for
# web/embed.go to embed from node_modules. pnpm's image is its standalone
# build, which needs no Node.
FROM ghcr.io/pnpm/pnpm:${PNPM_VERSION} AS web

WORKDIR /web

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# budge's code, with its modules and the page's libraries, for the test and
# build stages to share.
FROM golang:${GO_VERSION} AS source

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . .
COPY --from=web /web/node_modules web/node_modules

# The tests, which _build runs by building this stage before the image. The
# image's stages don't depend on it, so a build by hand skips it.
FROM source AS test

# With the race detector, as syncs, the scheduler and requests share state.
RUN go test -race ./...

FROM source AS build

# The release's version, which the admin page and the telemetry file record
# for each run.
ARG VERSION=""
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /go/bin/budge ./cmd/budge/...

FROM gcr.io/distroless/static-debian12

COPY --from=build /go/bin/budge /

CMD ["/budge"]