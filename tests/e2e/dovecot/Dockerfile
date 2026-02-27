FROM debian:trixie-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    dovecot-core \
    dovecot-imapd \
    dovecot-lmtpd \
    dovecot-sqlite \
    ca-certificates \
    curl \
  && rm -rf /var/lib/apt/lists/*

EXPOSE 143

CMD ["/usr/sbin/dovecot", "-F"]
