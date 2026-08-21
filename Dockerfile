# Build with the latest stable Go release.
FROM golang:1.27.0-alpine AS build

ARG VERSION=dev
WORKDIR /src

# Dependencies resolve in their own layer so source edits do not re-download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/stufently/zabbix-ai-cli/internal/cli.Version=${VERSION}" \
    -o /out/zabbix-ai-cli ./cmd/zabbix-ai-cli

# distroless has no shell, so the state directory has to be built here and
# copied in already owned by the unprivileged user the image runs as.
RUN mkdir -p /out/state && chmod 700 /out/state

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/zabbix-ai-cli /usr/local/bin/zabbix-ai-cli

# Plans and the audit log live here. Mount a writable volume over it to keep
# them; the root filesystem is expected to be read-only.
COPY --from=build --chown=65532:65532 /out/state /var/lib/zabbix-ai-cli

ENV ZABBIX_AI_CLI_STATE_DIR=/var/lib/zabbix-ai-cli
VOLUME /var/lib/zabbix-ai-cli

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/zabbix-ai-cli"]
CMD ["mcp"]
