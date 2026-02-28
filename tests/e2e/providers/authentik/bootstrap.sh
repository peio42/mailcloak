#!/bin/sh
set -eu

export AUTHENTIK_LOG_LEVEL=error

for i in $(seq 1 60); do
  if ak shell -c "from authentik.flows.models import Flow; import sys; sys.exit(0 if Flow.objects.filter(slug='default-authentication-flow').exists() else 1)" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "authentik bootstrap prerequisites not ready" >&2
    exit 1
  fi
  sleep 2
done

for i in $(seq 1 60); do
  if ak shell -c "from authentik.core.models import User; import sys; sys.exit(0 if User.objects.filter(username='akadmin').exists() else 1)" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "authentik admin user not ready" >&2
    exit 1
  fi
  sleep 2
done

ak shell -c "exec(open('/providers/authentik/bootstrap.py').read())"
