FROM node:18-alpine AS base

# Install dependencies only when needed
FROM base AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Install dependencies based on the preferred package manager
COPY package.json package-lock.json* ./
RUN npm install

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

ENV NODE_ENV production

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

ENV PORT 3000
ENV HOSTNAME "0.0.0.0"

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["node", "server.js"]
