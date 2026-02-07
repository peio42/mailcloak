#!/bin/sh
set -eu

if [ "${POSTFIX_EXTERNAL:-}" = "1" ]; then
  cp /etc/postfix/main-external.cf /etc/postfix/main.cf
fi

chown -R root:root /etc/postfix
chmod 0755 /etc/postfix /etc/postfix/sql
chmod 0644 /etc/postfix/main.cf /etc/postfix/main-external.cf /etc/postfix/transport /etc/postfix/sql/virtual_mailbox_domains.cf


postmap /etc/postfix/transport

mkdir -p /var/spool/postfix/private
chown -R postfix:postfix /var/spool/postfix
chmod 0750 /var/spool/postfix/private

# Disable chroot for rewrite (trivial-rewrite) to debug sqlite access in E2E
postconf -F 'rewrite/unix/chroot=n'

exec /usr/sbin/postfix start-fg
