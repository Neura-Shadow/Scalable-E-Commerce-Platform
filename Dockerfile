# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/goshop ./cmd/api

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 goshop \
    && adduser -S -D -H -u 10001 -G goshop goshop

WORKDIR /app
COPY --from=build --chown=goshop:goshop /out/goshop /app/goshop

USER 10001:10001
EXPOSE 8888 8889
HEALTHCHECK --interval=10s --timeout=2s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8888/livez || exit 1
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/goshop"]
