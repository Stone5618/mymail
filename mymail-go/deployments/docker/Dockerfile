# MyMail Go 后端多阶段 Dockerfile
#
# 构建上下文需为项目根目录（mymail/），以访问 mymail-go/ 和 mymail-vue/：
#   docker build -t mymail -f mymail-go/deployments/docker/Dockerfile .

# ===== Stage 1: 前端构建 =====
FROM node:22-alpine AS frontend-builder

WORKDIR /web
COPY mymail-vue/package.json mymail-vue/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY mymail-vue/ .
RUN npm run build

# ===== Stage 2: 后端构建 =====
FROM golang:1.25-alpine AS backend-builder

ARG VERSION=docker
ARG BUILD_TIME=
ARG COMMIT_SHA=

WORKDIR /app
RUN apk add --no-cache git
COPY mymail-go/go.mod mymail-go/go.sum ./
RUN go mod download
COPY mymail-go/ .
COPY --from=frontend-builder /web/dist ./web/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.CommitSHA=${COMMIT_SHA}" \
    -o /mymail ./cmd/mymail

# ===== Stage 3: 运行时 =====
FROM alpine:3.20

# 运行时依赖：ca-certificates（TLS）、tini（PID 1）、wget（HEALTHCHECK）
RUN apk add --no-cache ca-certificates tini wget

# P1-10：固定 UID/GID 1000
RUN addgroup -g 1000 -S mymail && adduser -u 1000 -S mymail -G mymail

WORKDIR /app
COPY --from=backend-builder /mymail ./mymail
COPY mymail-go/config/dovecot/ ./config/dovecot/
COPY mymail-go/config/nginx/ ./config/nginx/

RUN mkdir -p /app/data/maildir /app/data/attachments /app/data/avatars && \
    chown -R mymail:mymail /app

USER mymail

EXPOSE 3000 25

VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/healthz || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["./mymail"]
