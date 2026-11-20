# Isolated signal binary: only cmd/slime-signal, internal/signal and the leaf packages it imports are copied in.
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/slime-signal ./cmd/slime-signal
COPY internal/signal ./internal/signal
COPY internal/wire ./internal/wire
COPY internal/sharelink ./internal/sharelink
COPY internal/randid ./internal/randid
COPY internal/env ./internal/env
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /slime-signal ./cmd/slime-signal

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /slime-signal /usr/local/bin/slime-signal
EXPOSE 8080
CMD ["slime-signal"]
