# Isolated signal binary: only cmd/keel-signal, internal/signal and the leaf packages it imports are copied in.
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/keel-signal ./cmd/keel-signal
COPY internal/signal ./internal/signal
COPY internal/wire ./internal/wire
COPY internal/sharelink ./internal/sharelink
COPY internal/randid ./internal/randid
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /keel-signal ./cmd/keel-signal

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /keel-signal /usr/local/bin/keel-signal
EXPOSE 8080
CMD ["keel-signal"]
