FROM golang:1.26 as build

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/mailcloak ./cmd/mailcloak \
  && CGO_ENABLED=0 go build -o /out/mailcloakctl ./cmd/mailcloakctl

FROM debian:trixie-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    jq \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/mailcloak /usr/local/bin/mailcloak
COPY --from=build /out/mailcloakctl /usr/local/bin/mailcloakctl
COPY tests/e2e/mailcloak/seed_sqlite.sh /tmp/seed_sqlite.sh

RUN /bin/sh /tmp/seed_sqlite.sh

WORKDIR /
ENTRYPOINT ["/usr/local/bin/mailcloak"]
