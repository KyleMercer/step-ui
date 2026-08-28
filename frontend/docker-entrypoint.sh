#!/bin/sh
set -eu

# Runtime substitution for NEXT_PUBLIC_* variables.
#
# Next.js inlines NEXT_PUBLIC_* variables into the client-side JS bundle at
# `next build` time. That happens once, during the Docker image build, and
# the result is baked into static files (.next/static, .next/standalone) --
# a normal `environment:` entry in docker-compose.yml or a Dockhand env-var
# edit at container-start time has no effect on those files, because the
# container is never rebuilt, just recreated from the same image.
#
# To let these still behave like ordinary runtime environment variables,
# the Dockerfile bakes in a distinctive placeholder token at build time
# instead of a real value (see the builder stage). Every time this
# container starts, before the Next.js server runs, this script walks the
# compiled output and replaces that placeholder with whatever value the
# container actually started with. Changing NEXT_PUBLIC_API_URL and
# recreating the container (no rebuild needed) now works as expected.
#
# Add more entries to RUNTIME_VARS if the frontend ever exposes additional
# NEXT_PUBLIC_* variables that need the same treatment -- each one just
# needs a matching `ENV <NAME>=__RUNTIME_<NAME>__` line in the Dockerfile's
# builder stage before `npm run build`.
#
# NEXT_PUBLIC_API_AUTH_TOKEN needs this too, for the same reason as
# NEXT_PUBLIC_API_URL: without it, the shared-secret token the frontend
# sends to the backend would only ever be whatever it was at image build
# time, so rotating/setting API_AUTH_TOKEN later would need a full image
# rebuild rather than just recreating the container.
RUNTIME_VARS="NEXT_PUBLIC_API_URL NEXT_PUBLIC_API_AUTH_TOKEN"

for var_name in $RUNTIME_VARS; do
  placeholder="__RUNTIME_${var_name}__"
  # POSIX sh has no ${!var} indirection, so use eval for the lookup.
  eval "value=\${${var_name}:-}"

  if [ -z "$value" ]; then
    echo "docker-entrypoint: warning: ${var_name} is not set; leaving the build-time placeholder in place" >&2
    continue
  fi

  echo "docker-entrypoint: substituting ${var_name}"
  grep -rlF -- "$placeholder" /app/.next /app/server.js 2>/dev/null | while IFS= read -r file; do
    sed -i "s#${placeholder}#${value}#g" "$file"
  done
done

exec "$@"
