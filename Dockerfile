# Isolated signal binary — do not build cmd/keel (Wails/CGO).
FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/keel-signal ./cmd/keel-signal
COPY internal/signal ./internal/signal
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /keel-signal ./cmd/keel-signal

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /keel-signal /usr/local/bin/keel-signal
EXPOSE 8080
CMD ["keel-signal"]
