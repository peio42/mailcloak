FROM debian:trixie-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    postfix \
    postfix-sqlite \
    ca-certificates \
    perl \
    curl \
    netcat-openbsd \
  && rm -rf /var/lib/apt/lists/*

# Install a newer swaks with OAUTHBEARER support
RUN curl -fsSL https://raw.githubusercontent.com/jetmore/swaks/master/swaks \
  -o /usr/local/bin/swaks \
  && chmod +x /usr/local/bin/swaks

ENV PATH="/usr/local/bin:${PATH}"

COPY main.cf /etc/postfix/main.cf
COPY main-external.cf /etc/postfix/main-external.cf
COPY transport /etc/postfix/transport
COPY sql/virtual_mailbox_domains.cf /etc/postfix/sql/virtual_mailbox_domains.cf
COPY run.sh /run.sh

EXPOSE 25

ENTRYPOINT ["/run.sh"]
