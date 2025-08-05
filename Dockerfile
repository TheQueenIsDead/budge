FROM golang:1.24 AS build

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . .
RUN ls -la
RUN CGO_ENABLED=0 go build -o /go/bin/budge ./cmd/budge/...

FROM gcr.io/distroless/static-debian12

COPY --from=build /go/bin/budge /
COPY ./web/ web/

CMD ["/budge"]