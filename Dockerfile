FROM golang:1.27 AS build

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . .
RUN ls -la
RUN CGO_ENABLED=0 go build -o /go/bin/budge ./cmd/budge/...

FROM gcr.io/distroless/static-debian12

COPY --from=build /go/bin/budge /

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s CMD ["/budge", "healthcheck"]
CMD ["/budge"]