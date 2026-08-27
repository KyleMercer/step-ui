#!/bin/bash

# Build both images with a version + git-commit + timestamp build identity
# baked in, and a full, unfolded build log saved to disk.
#
# Usage: ./build.sh [extra docker compose build args...]
# Example: ./build.sh backend        (build just the backend service)
#
# Why this exists: the image tag alone (e.g. "step-ui-backend-new:local")
# doesn't change from build to build, so there's no way to look at a running
# container and tell which actual build produced it -- exactly the kind of
# mixup that caused the orphaned-container confusion earlier on. This script
# reads the single VERSION file at the repo root and forwards it, alongside
# the git commit and build time, to both images as OCI labels (see the
# LABEL lines near the bottom of each Dockerfile) -- so `docker inspect`,
# and any UI that reads standard image labels (Portainer, and likely
# Dockhand too), always shows the truth regardless of what the tag says,
# and backend/frontend built together always agree on the version.

set -e

cd "$(dirname "$0")"

APP_VERSION="$(cat VERSION 2>/dev/null || echo unknown)"
GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

# Flag (not block) building from a dirty tree -- the image would then
# reflect uncommitted changes that the git SHA alone can't account for.
GIT_DIRTY=""
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
    GIT_DIRTY="-dirty"
fi

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BUILD_ID="${GIT_SHA}${GIT_DIRTY}"
LOG_FILE="build-v${APP_VERSION}-${BUILD_ID}-$(date -u +%Y%m%d-%H%M%S).log"

echo "========================================="
echo "Building step-ui images"
echo "  version:     ${APP_VERSION}"
echo "  git commit:  ${BUILD_ID}"
echo "  build date:  ${BUILD_DATE}"
echo "  full log:    ${LOG_FILE}"
echo "========================================="

if [ -n "$GIT_DIRTY" ]; then
    echo ""
    echo "WARNING: working tree has uncommitted changes -- this image won't"
    echo "exactly match any commit in git history. Fine for local testing,"
    echo "but commit first if this build is meant to be reproducible later."
    echo ""
fi

docker compose build --no-cache --progress=plain \
    --build-arg APP_VERSION="${APP_VERSION}" \
    --build-arg GIT_SHA="${BUILD_ID}" \
    --build-arg BUILD_DATE="${BUILD_DATE}" \
    "$@" 2>&1 | tee "${LOG_FILE}"

echo ""
echo "Done. To confirm what a running container is actually built from later:"
echo "  docker inspect <container-or-image> --format '{{json .Config.Labels}}'"
echo "Or check the Labels/Image details panel in Portainer/Dockhand directly."
