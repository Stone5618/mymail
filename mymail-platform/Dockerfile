# Stage 1: Build
FROM node:22-alpine AS builder

WORKDIR /app

# Install build dependencies for native modules
RUN apk add --no-cache python3 make g++

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npx tailwindcss -i ./public/css/input.css -o ./public/css/style.css --minify

# Prune dev dependencies in builder
RUN npm prune --omit=dev && npm cache clean --force

# Stage 2: Production
FROM node:22-alpine

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache curl tini

# Create non-root user
RUN addgroup -S mymail && adduser -S mymail -G mymail

# Copy pre-built node_modules from builder (no recompile needed)
COPY --from=builder /app/node_modules ./node_modules

# Copy application files
COPY --from=builder /app/src ./src
COPY --from=builder /app/public ./public
COPY --from=builder /app/scripts ./scripts
COPY --from=builder /app/tailwind.config.js ./
COPY --from=builder /app/postcss.config.js ./
COPY --from=builder /app/package.json ./

# Create data directories and set permissions
RUN mkdir -p /app/data/maildir /app/data/attachments && \
    chown -R mymail:mymail /app

# Switch to non-root user
USER mymail

EXPOSE 3000 25 2525

VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/api/auth/me || exit 1

ENTRYPOINT ["tini", "--", "sh", "-c"]
CMD ["node scripts/init-db.js && exec node src/server.js"]
