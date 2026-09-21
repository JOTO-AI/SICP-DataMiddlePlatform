# syntax=docker/dockerfile:1
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG SERVICE=query-service
RUN test "$SERVICE" = query-service || test "$SERVICE" = sync-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /service ./cmd/${SERVICE}

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /service /usr/local/bin/service
USER 65532:65532
ENV HTTP_ADDRESS=0.0.0.0:8080
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/service"]
