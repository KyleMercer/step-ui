FROM node:24-alpine AS base

# Install dependencies only when needed
FROM base AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Install dependencies based on the preferred package manager
COPY package.json package-lock.json* ./
RUN npm ci

# Rebuild the source code only when needed
FROM base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# NEXT_PUBLIC_* variables are inlined into the client JS bundle at build
# time, so a real value here would get permanently baked into the image.
# Bake a placeholder instead -- docker-entrypoint.sh swaps in the real
# runtime value on every container start, so NEXT_PUBLIC_API_URL keeps
# working as a normal environment variable set in docker-compose.yml.
ENV NEXT_PUBLIC_API_URL=__RUNTIME_NEXT_PUBLIC_API_URL__

# Build the application
RUN npm run build

# Ensure public directory exists
RUN mkdir -p public

# Production image, copy all the files and run next
FROM base AS runner
WORKDIR /app

ENV NODE_ENV=production

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public

# Set the correct permission for prerender cache
RUN mkdir .next
RUN chown nextjs:nodejs .next

# Automatically leverage output traces to reduce image size
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

# WORKDIR creates /app as root; fix its ownership (not just its contents')
# so the nextjs user can write the temp files `sed -i` needs when the
# entrypoint script substitutes the runtime value into /app/server.js.
RUN chown nextjs:nodejs /app

COPY --chown=nextjs:nodejs docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

USER nextjs

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

ENTRYPOINT ["/app/docker-entrypoint.sh"]

# wget is part of BusyBox and is already present on node:*-alpine, so this
# doesn't need an extra package install (also surfaced by Docker UIs like
# Dockhand).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- --spider http://localhost:3000/ || exit 1

# Build identity, baked into the image so `docker inspect` -- and any UI
# that reads standard image labels, like Portainer or Dockhand -- always
# shows exactly what produced it, regardless of what tag it's under or
# whether a tag got reused/overwritten. APP_VERSION comes from the single
# VERSION file at the repo root (build.sh reads it and forwards it here, so
# both this image and the backend image always agree); GIT_SHA/BUILD_DATE
# are the exact commit/time. Anything left unset just reads "unknown"
# rather than silently lying about it.
ARG APP_VERSION=unknown
ARG GIT_SHA=unknown
ARG BUILD_DATE=unknown
RUN echo "Building step-ui-frontend version ${APP_VERSION} (commit ${GIT_SHA}, built ${BUILD_DATE})"
LABEL org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.title="step-ui-frontend"

CMD ["node", "server.js"]
