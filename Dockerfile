# The page's libraries, installed with pnpm from web/package.json and copied
# into public/vendor, to be embedded with budge's own static files.
FROM node:26-alpine AS web

# pnpm's own build, from the image pnpm publishes. Renovate moves it with the
# packageManager version in web/package.json.
COPY --from=ghcr.io/pnpm/pnpm:12.16.1-alpine /usr/local/bin/pnpm /usr/local/bin/pnpm

WORKDIR /web

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/vendor.mjs ./
RUN pnpm install --frozen-lockfile

FROM golang:1.27 AS build

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . .
COPY --from=web /web/public/vendor web/public/vendor
RUN ls -la
# The release's version, which the admin page and the telemetry file record
# for each run.
ARG VERSION=""
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /go/bin/budge ./cmd/budge/...

FROM gcr.io/distroless/static-debian12

COPY --from=build /go/bin/budge /

CMD ["/budge"]