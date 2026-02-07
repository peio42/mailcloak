FROM golang:1.24 as build

WORKDIR /src
COPY . .
RUN go build -o /out/mailcloak ./cmd/mailcloak

FROM debian:trixie-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    python3 \
    python3-pip \
    python3-argon2 \
    curl \
    jq \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/mailcloak /usr/local/bin/mailcloak
COPY mailcloakctl /usr/local/bin/mailcloakctl
COPY tests/e2e/mailcloak/config.yaml /etc/mailcloak/config.yaml
COPY tests/e2e/mailcloak/seed_sqlite.sh /tmp/seed_sqlite.sh

RUN chmod +x /usr/local/bin/mailcloakctl
RUN /bin/sh /tmp/seed_sqlite.sh

WORKDIR /
ENTRYPOINT ["/usr/local/bin/mailcloak"]
