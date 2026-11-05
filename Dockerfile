# The page's libraries, installed with the pnpm package.json names and copied
# into public/vendor, to be embedded with budge's own static files.
FROM node:26-alpine AS web

WORKDIR /web

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/vendor.mjs ./
RUN npm install -g "$(node -p 'require("./package.json").packageManager')"
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