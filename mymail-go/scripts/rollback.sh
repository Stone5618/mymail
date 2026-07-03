#!/bin/bash
# MyMail 回滚脚本
#
# 回滚策略：
#   1. 记录当前版本
#   2. 停止当前容器
#   3. 启动回滚版本容器（默认上一版本）
#   4. 健康检查
#   5. Nginx 切换流量
#   6. 验证服务正常
#
# 用法：
#   ./scripts/rollback.sh              # 回滚到上一版本
#   ./scripts/rollback.sh v1.1.0       # 回滚到指定版本
#
# 依赖：docker、docker compose、nginx
set -euo pipefail

# ============ 颜色定义 ============
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ============ 路径与全局变量 ============
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$PROJECT_DIR/deployments/docker/docker-compose.yml"
NGINX_CONF_DIR="$PROJECT_DIR/config/nginx"
NGINX_CONF="$NGINX_CONF_DIR/default.conf"
LOG_DIR="$PROJECT_DIR/data/logs"
LOG_FILE="$LOG_DIR/rollback_$(date +%Y%m%d_%H%M%S).log"
STATE_FILE="$PROJECT_DIR/data/.deploy-state"

# 端口规划
ROLLBACK_PORT=8082      # 回滚版本临时端口
LIVE_PORT=8080          # 线上端口
HEALTH_PATH="/healthz"
HEALTH_TIMEOUT=120
HEALTH_INTERVAL=2

# ============ 工具函数 ============
log() {
  local level="$1"
  shift
  local msg="$*"
  local timestamp
  timestamp="$(date '+%Y-%m-%d %H:%M:%S')"
  echo -e "${timestamp} [${level}] ${msg}" | tee -a "$LOG_FILE"
}

info()  { log "INFO"  "${GREEN}$*${NC}"; }
warn()  { log "WARN"  "${YELLOW}$*${NC}"; }
error() { log "ERROR" "${RED}$*${NC}"; }
step()  { log "STEP"  "${BLUE}$*${NC}"; }

# 失败退出：输出错误并返回非零
die() {
  error "$*"
  error "回滚失败！请手动检查容器与 Nginx 配置"
  error "  - 容器状态: docker ps -a | grep mymail"
  error "  - Nginx 配置: $NGINX_CONF"
  exit 1
}

# 健康检查：轮询指定端口直到 /healthz 返回 200
# 参数: $1 = 端口, $2 = 容器名
wait_for_health() {
  local port="$1"
  local name="$2"
  local elapsed=0
  step "健康检查 ${name} (端口 ${port})..."
  while [ $elapsed -lt $HEALTH_TIMEOUT ]; do
    if curl -sf -o /dev/null "http://127.0.0.1:${port}${HEALTH_PATH}"; then
      info "${name} 健康检查通过（耗时 ${elapsed}s）"
      return 0
    fi
    sleep $HEALTH_INTERVAL
    elapsed=$((elapsed + HEALTH_INTERVAL))
  done
  error "${name} 健康检查超时（${HEALTH_TIMEOUT}s）"
  return 1
}

# 读取当前线上版本
get_current_version() {
  if [ -f "$STATE_FILE" ]; then
    grep '^current_version=' "$STATE_FILE" | cut -d'=' -f2
  else
    echo "unknown"
  fi
}

# 读取上一版本（next_version 字段在 deploy.sh 中记录为最终版本，
# 回滚时若未指定目标版本，则尝试使用 blue 容器对应的镜像）
get_previous_version() {
  local target="$1"
  if [ -n "$target" ]; then
    echo "$target"
    return
  fi
  # 默认：使用已停止的 blue 容器镜像
  if docker ps -a --format '{{.Names}}\t{{.Image}}' | grep -q 'mymail-app-blue'; then
    docker ps -a --format '{{.Names}}\t{{.Image}}' | grep 'mymail-app-blue' | cut -f2
    return
  fi
  echo ""
}

# 生成 Nginx 全量指向指定上游的配置
# 参数: $1 = 上游名称 (mymail_blue / mymail_green / mymail_rollback)
write_nginx_config() {
  local upstream="$1"
  local backup
  backup="$NGINX_CONF.bak.$(date +%s)"
  cp "$NGINX_CONF" "$backup" 2>/dev/null || true
  info "Nginx 配置已备份至 $backup"

  cat > "$NGINX_CONF" << NGINX_EOF
# MyMail Nginx 反向代理配置（回滚后生成）
# 全量指向: ${upstream}
# 生成时间：$(date '+%Y-%m-%d %H:%M:%S')

upstream mymail_rollback {
    server ${upstream};
}

# Rate limiting zone
limit_req_zone \$binary_remote_addr zone=general:10m rate=10r/s;
limit_req_zone \$binary_remote_addr zone=login:10m rate=5r/m;

# HTTP -> HTTPS redirect
server {
    listen 80;
    server_name _;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

# HTTPS server
server {
    listen 443 ssl http2;
    server_name _;

    ssl_certificate     /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;

    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    client_max_body_size 30M;

    location /ws {
        proxy_pass http://mymail_rollback;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400;
    }

    location /api/auth/login {
        proxy_pass http://mymail_rollback;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        limit_req zone=login burst=3 nodelay;
    }

    location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)\$ {
        proxy_pass http://mymail_rollback;
        proxy_set_header Host \$host;
        expires 7d;
        add_header Cache-Control "public, immutable";
    }

    location / {
        proxy_pass http://mymail_rollback;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header X-Forwarded-Host \$host;
        limit_req zone=general burst=20 nodelay;
    }

    location ~ /\\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
NGINX_EOF
  info "Nginx 配置已生成（指向 ${upstream}）"
}

# 重载 Nginx
reload_nginx() {
  step "测试并重载 Nginx 配置..."
  if ! docker exec mymail-nginx nginx -t 2>&1 | tee -a "$LOG_FILE"; then
    error "Nginx 配置语法错误"
    return 1
  fi
  if ! docker exec mymail-nginx nginx -s reload 2>&1 | tee -a "$LOG_FILE"; then
    error "Nginx reload 失败"
    return 1
  fi
  info "Nginx 已重载"
  return 0
}

# ============ 主流程 ============
main() {
  local target_version="${1:-}"
  mkdir -p "$LOG_DIR"

  info "======================================"
  info "↩️  MyMail 回滚开始"
  info "   目标版本:  ${target_version:-自动（上一版本）}"
  info "   日志文件:  $LOG_FILE"
  info "======================================"

  # ===== 步骤 1：记录当前版本 =====
  local current_version
  current_version="$(get_current_version)"
  info "当前线上版本: $current_version"

  if [ "$current_version" = "unknown" ]; then
    warn "未找到发布状态文件，无法确定当前版本，将尝试从运行容器推断"
  fi

  # ===== 步骤 2：确定回滚目标版本 =====
  local rollback_image
  rollback_image="$(get_previous_version "$target_version")"
  if [ -z "$rollback_image" ]; then
    die "无法确定回滚目标版本：未指定版本且未找到 blue 容器。请手动指定版本: $0 <版本号>"
  fi
  info "回滚目标镜像: $rollback_image"

  # ===== 步骤 3：启动回滚版本容器（临时端口 8082） =====
  step "[1/5] 启动回滚容器（端口 ${ROLLBACK_PORT}）..."
  docker rm -f mymail-app-rollback >/dev/null 2>&1 || true

  if ! docker run -d \
    --name mymail-app-rollback \
    --network mymail-net \
    -p "${ROLLBACK_PORT}:${ROLLBACK_PORT}" \
    -v mymail-data:/app/data \
    -e ENV=prod \
    -e DB_PATH=/app/data/mymail.db \
    -e HTTP_PORT="${ROLLBACK_PORT}" \
    "$rollback_image" 2>&1 | tee -a "$LOG_FILE"; then
    die "回滚容器启动失败（镜像: $rollback_image）"
  fi
  info "回滚容器已启动 (mymail-app-rollback:${ROLLBACK_PORT})"

  # ===== 步骤 4：健康检查 =====
  step "[2/5] 回滚容器健康检查..."
  if ! wait_for_health "$ROLLBACK_PORT" "rollback"; then
    # 健康检查失败，清理回滚容器后退出
    docker rm -f mymail-app-rollback >/dev/null 2>&1 || true
    die "回滚容器健康检查未通过，已清理。当前线上服务未受影响"
  fi

  # ===== 步骤 5：切换 Nginx 流量至回滚版本 =====
  step "[3/5] 切换 Nginx 流量至回滚版本..."
  write_nginx_config "mymail-app-rollback:${ROLLBACK_PORT}"
  if ! reload_nginx; then
    # Nginx 切换失败，但回滚容器健康，保留以便排查，退出
    die "Nginx 切换失败。回滚容器仍在运行 (mymail-app-rollback)，请手动修复 Nginx 配置"
  fi
  info "Nginx 已全量指向回滚版本"

  # 等待流量稳定
  sleep 10

  # ===== 步骤 6：停止旧版本容器 =====
  step "[4/5] 停止旧版本容器..."
  local stopped_any=false
  for name in mymail-app mymail-app-green mymail-app-blue; do
    if docker ps --format '{{.Names}}' | grep -q "^${name}$"; then
      docker stop "$name" >/dev/null 2>&1 || true
      info "已停止容器: $name"
      stopped_any=true
    fi
  done
  if [ "$stopped_any" = false ]; then
    warn "未发现运行中的旧版本容器"
  fi

  # ===== 步骤 7：验证服务正常 =====
  step "[5/5] 验证回滚后服务状态..."
  if ! curl -sf -o /dev/null "http://127.0.0.1:${ROLLBACK_PORT}${HEALTH_PATH}"; then
    die "回滚后健康检查失败！请立即检查 mymail-app-rollback 容器"
  fi

  # 更新状态文件
  mkdir -p "$(dirname "$STATE_FILE")"
  cat > "$STATE_FILE" << EOF
current_version=${rollback_image}
next_version=${rollback_image}
last_rollback_at=$(date '+%Y-%m-%d %H:%M:%S')
last_rollback_from=$current_version
EOF

  info "======================================"
  info "✅ 回滚完成！"
  info "   当前版本:  $rollback_image"
  info "   回滚自:    $current_version"
  info "   服务端口:  ${ROLLBACK_PORT}"
  info "   日志文件:  $LOG_FILE"
  info "======================================"
  warn "提示：回滚容器名为 mymail-app-rollback，建议后续重命名为 mymail-app 以恢复正常拓扑"
}

main "$@"
