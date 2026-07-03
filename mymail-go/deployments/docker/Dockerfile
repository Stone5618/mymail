# MyMail Go 后端多阶段 Dockerfile
# 修复：P1-8（HEALTHCHECK /healthz）、P1-10（UID/GID 1000）、8.7（//go:embed dist/）
#
# 构建上下文需为项目根目录（mymail/），以访问 mymail-go/ 和 mymail-vue/：
#   docker build -t mymail -f mymail-go/deployments/docker/Dockerfile .

# ===== Stage 1: 前端构建 =====
FROM node:22-alpine AS frontend-builder

WORKDIR /web

# 先复制依赖文件，利用 Docker 层缓存
COPY mymail-vue/package.json mymail-vue/package-lock.json ./
RUN npm ci --no-audit --no-fund

# 复制源码并构建（输出到 /web/dist/）
COPY mymail-vue/ .
RUN npm run build

# ===== Stage 2: 后端构建（含嵌入前端） =====
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

RUN apk add --no-cache git

# 先复制依赖文件，利用 Docker 层缓存
COPY mymail-go/go.mod mymail-go/go.sum ./
RUN go mod download

# 复制源码
COPY mymail-go/ .

# 将前端构建产物复制到 web/dist/（覆盖 .gitkeep 占位文件）
COPY --from=frontend-builder /web/dist ./web/dist/

# 构建（CGO_ENABLED=0 纯静态链接，适配 scratch/alpine 运行时）
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=docker" \
    -o /mymail ./cmd/mymail

# ===== Stage 3: 运行时 =====
FROM alpine:3.20

# 运行时依赖：ca-certificates（TLS）、tini（PID 1 信号处理）、wget（HEALTHCHECK）
RUN apk add --no-cache ca-certificates tini wget

# P1-10 修复：创建非 root 用户，固定 UID/GID 1000
# 与 Dovecot user_query 中的 uid/gid 1000 一致，确保 maildir 文件权限匹配
RUN addgroup -g 1000 -S mymail && adduser -u 1000 -S mymail -G mymail

WORKDIR /app

# 复制 Go 二进制
COPY --from=backend-builder /mymail ./mymail

# 复制 Dovecot 和 Nginx 配置（供 dovecot/nginx 容器挂载使用）
COPY mymail-go/config/dovecot/ ./config/dovecot/
COPY mymail-go/config/nginx/ ./config/nginx/

# 创建数据目录并设置权限
RUN mkdir -p /app/data/maildir /app/data/attachments && \
    chown -R mymail:mymail /app

# P1-10：以 UID 1000 运行
USER mymail

EXPOSE 3000 25

VOLUME ["/app/data"]

# P1-8 修复：HEALTHCHECK 用 /healthz（无需认证，区别于原 /api/auth/me）
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/healthz || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["./mymail"]
