# syntax=docker/dockerfile:1
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG SERVICE=query-service
RUN test "$SERVICE" = query-service || test "$SERVICE" = sync-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /service ./cmd/${SERVICE}
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /db-migrate ./cmd/db-migrate

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /service /usr/local/bin/service
COPY --from=build /db-migrate /usr/local/bin/db-migrate
COPY deploy/worker-entrypoint.sh /usr/local/bin/worker-entrypoint.sh
USER 65532:65532
ENV HTTP_ADDRESS=0.0.0.0:8080
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/service"]
