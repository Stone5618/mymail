#!/bin/bash
# MyMail 蓝绿 + 金丝雀发布脚本
#
# 发布策略：
#   1. 构建新版本镜像（green）
#   2. 启动新版本容器（不同端口，与当前线上 blue 版本并行）
# 3. 健康检查通过后，按灰度比例配置 Nginx 分流
#   4. 等待观察期（10% 等 30min，50% 等 1h，100% 直接切换）
#   5. 切换完成，停止旧版本容器
#   6. 任一阶段失败自动回滚到旧版本
#
# 用法：
#   ./scripts/deploy.sh <版本号> <灰度比例>
#   示例：
#     ./scripts/deploy.sh v1.2.0 10     # 金丝雀 10% 流量
#     ./scripts/deploy.sh v1.2.0 50     # 扩大至 50% 流量
#     ./scripts/deploy.sh v1.2.0 100    # 全量切换
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
LOG_FILE="$LOG_DIR/deploy_$(date +%Y%m%d_%H%M%S).log"
STATE_FILE="$PROJECT_DIR/data/.deploy-state"  # 发布状态文件（记录当前/下一版本）

# 端口规划：blue 8080（线上），green 8081（待发布）
BLUE_PORT=8080
GREEN_PORT=8081
HEALTH_PATH="/healthz"
HEALTH_TIMEOUT=120  # 健康检查超时（秒）
HEALTH_INTERVAL=2   # 健康检查间隔（秒）

# ============ 工具函数 ============
log() {
  local level="$1"
  shift
  local msg="$*"
  local timestamp
  timestamp="$(date '+%Y-%m-%d %H:%M:%S')"
  echo -e "${timestamp} [${level}] ${msg}" | tee -a "$LOG_FILE"
}

info()    { log "INFO"  "${GREEN}$*${NC}"; }
warn()    { log "WARN"  "${YELLOW}$*${NC}"; }
error()   { log "ERROR" "${RED}$*${NC}"; }
step()    { log "STEP"  "${BLUE}$*${NC}"; }

# 失败时自动回滚并退出
fail_and_rollback() {
  local reason="$1"
  error "发布失败：${reason}"
  error "开始自动回滚..."
  rollback_to_blue
  exit 1
}

# 回滚到 blue 版本（恢复 Nginx 全量指向 blue）
rollback_to_blue() {
  warn "回滚中：将 Nginx 全量切回 blue (${BLUE_PORT})..."
  write_nginx_config 100 0
  if ! reload_nginx; then
    error "Nginx reload 失败，请手动检查配置: $NGINX_CONF"
    return 1
  fi
  # 停止 green 容器（如果存在）
  if docker ps --format '{{.Names}}' | grep -q 'mymail-app-green'; then
    docker stop mymail-app-green >/dev/null 2>&1 || true
    docker rm mymail-app-green >/dev/null 2>&1 || true
    info "green 容器已停止并移除"
  fi
  info "回滚完成，blue 版本继续提供服务"
}

# 校验参数
validate_args() {
  if [ $# -lt 2 ]; then
    echo "用法: $0 <版本号> <灰度比例 10|50|100>"
    echo "示例: $0 v1.2.0 10"
    exit 1
  fi
  VERSION="$1"
  CANARY_RATIO="$2"
  if [[ ! "$CANARY_RATIO" =~ ^(10|50|100)$ ]]; then
    error "灰度比例必须为 10 / 50 / 100，当前: $CANARY_RATIO"
    exit 1
  fi
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

# 生成 Nginx upstream 分流配置（基于 split_clients 按比例分流）
# 参数: $1 = blue 权重百分比, $2 = green 权重百分比
write_nginx_config() {
  local blue_pct="$1"
  local green_pct="$2"
  local backup
  backup="$NGINX_CONF.bak.$(date +%s)"
  cp "$NGINX_CONF" "$backup" 2>/dev/null || true
  info "Nginx 配置已备份至 $backup"

  cat > "$NGINX_CONF" << NGINX_EOF
# MyMail Nginx 反向代理配置（自动生成 - 蓝绿金丝雀分流）
# 当前分流：blue ${blue_pct}% / green ${green_pct}%
# 生成时间：$(date '+%Y-%m-%d %H:%M:%S')

# 蓝绿上游
upstream mymail_blue {
    server mymail-app-blue:${BLUE_PORT};
}
upstream mymail_green {
    server mymail-app-green:${GREEN_PORT};
}

# 按客户端 IP 哈希分流，保证会话粘性
split_clients "\${remote_addr}\${http_user_agent}" \$mymail_upstream {
    ${green_pct}%  mymail_green;
    *              mymail_blue;
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

    # WebSocket support（按分流策略转发）
    location /ws {
        proxy_pass http://\$mymail_upstream;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400;
    }

    # Login endpoint with stricter rate limiting
    location /api/auth/login {
        proxy_pass http://\$mymail_upstream;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        limit_req zone=login burst=3 nodelay;
    }

    # Static assets caching
    location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)\$ {
        proxy_pass http://\$mymail_upstream;
        proxy_set_header Host \$host;
        expires 7d;
        add_header Cache-Control "public, immutable";
    }

    # Default: reverse proxy with canary split
    location / {
        proxy_pass http://\$mymail_upstream;
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
  info "Nginx 配置已生成：blue ${blue_pct}% / green ${green_pct}%"
}

# 重载 Nginx（先测试配置再 reload）
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

# 等待观察期（按灰度比例确定）
# 10% -> 30min, 50% -> 1h, 100% -> 0（直接切换）
get_observation_seconds() {
  case "$CANARY_RATIO" in
    10)  echo 1800 ;;  # 30 分钟
    50)  echo 3600 ;;  # 1 小时
    100) echo 0 ;;
  esac
}

# 记录发布状态
# 参数: $1 = 当前线上版本, $2 = 下一版本
save_state() {
  local current="$1"
  local next="$2"
  mkdir -p "$(dirname "$STATE_FILE")"
  cat > "$STATE_FILE" << EOF
current_version=$current
next_version=$next
last_deploy_at=$(date '+%Y-%m-%d %H:%M:%S')
last_canary_ratio=$CANARY_RATIO
EOF
}

# 读取当前线上版本
get_current_version() {
  if [ -f "$STATE_FILE" ]; then
    grep '^current_version=' "$STATE_FILE" | cut -d'=' -f2
  else
    echo "unknown"
  fi
}

# ============ 主流程 ============
main() {
  validate_args "$@"

  mkdir -p "$LOG_DIR"
  info "======================================"
  info "📧 MyMail 发布开始"
  info "   版本:      $VERSION"
  info "   灰度比例:  $CANARY_RATIO%"
  info "   日志文件:  $LOG_FILE"
  info "======================================"

  local current_version
  current_version="$(get_current_version)"
  info "当前线上版本: $current_version"

  # ===== 步骤 1：构建新版本镜像 =====
  step "[1/6] 构建新版本镜像 mymail:${VERSION}..."
  if ! docker build -t "mymail:${VERSION}" \
    -f "$PROJECT_DIR/deployments/docker/Dockerfile" \
    "$PROJECT_DIR/.." 2>&1 | tee -a "$LOG_FILE"; then
    fail_and_rollback "镜像构建失败"
  fi
  info "镜像构建完成: mymail:${VERSION}"

  # ===== 步骤 2：启动新版本容器（green，端口 8081） =====
  step "[2/6] 启动 green 容器（端口 ${GREEN_PORT}）..."
  # 若已存在则先移除
  docker rm -f mymail-app-green >/dev/null 2>&1 || true

  if ! docker run -d \
    --name mymail-app-green \
    --network mymail-net \
    -p "${GREEN_PORT}:${GREEN_PORT}" \
    -v mymail-data:/app/data \
    -e ENV=prod \
    -e DB_PATH=/app/data/mymail.db \
    -e HTTP_PORT="${GREEN_PORT}" \
    "mymail:${VERSION}" 2>&1 | tee -a "$LOG_FILE"; then
    fail_and_rollback "green 容器启动失败"
  fi
  info "green 容器已启动 (mymail-app-green:${GREEN_PORT})"

  # ===== 步骤 3：健康检查 =====
  step "[3/6] green 健康检查..."
  if ! wait_for_health "$GREEN_PORT" "green"; then
    fail_and_rollback "green 健康检查未通过"
  fi

  # ===== 步骤 4：按比例配置 Nginx 分流 =====
  step "[4/6] 配置 Nginx 分流（blue $((100 - CANARY_RATIO))% / green ${CANARY_RATIO}%）..."
  # 确保 blue 容器别名存在（首次发布时将 mymail-app 重命名）
  if ! docker ps --format '{{.Names}}' | grep -q 'mymail-app-blue'; then
    if docker ps --format '{{.Names}}' | grep -q '^mymail-app$'; then
      info "首次发布：将 mymail-app 重命名为 mymail-app-blue"
      docker rename mymail-app mymail-app-blue || true
    fi
  fi

  write_nginx_config $((100 - CANARY_RATIO)) "$CANARY_RATIO"
  if ! reload_nginx; then
    fail_and_rollback "Nginx 分流配置失败"
  fi
  info "金丝雀分流已生效：${CANARY_RATIO}% 流量指向 green"

  # ===== 步骤 5：观察期 =====
  local observe_seconds
  observe_seconds="$(get_observation_seconds)"
  if [ "$observe_seconds" -gt 0 ]; then
    step "[5/6] 观察期 ${observe_seconds}s（$(printf '%dh%02dm' $((observe_seconds/3600)) $(((observe_seconds%3600)/60)))）..."
    info "观察期间请关注 Grafana 看板与告警通知"
    local waited=0
    while [ $waited -lt $observe_seconds ]; do
      sleep 60
      waited=$((waited + 60))
      # 观察期间若 green 容器异常退出，立即回滚
      if ! docker ps --format '{{.Names}}' | grep -q 'mymail-app-green'; then
        fail_and_rollback "green 容器在观察期内异常退出"
      fi
      info "观察进度: ${waited}/${observe_seconds}s ($(printf '%d%%' $((waited * 100 / observe_seconds))))"
    done
    info "观察期结束，green 运行稳定"
  else
    info "[5/6] 100% 切换，跳过观察期"
  fi

  # ===== 步骤 6：全量切换并停止旧版本 =====
  step "[6/6] 全量切换至 green，停止 blue..."
  write_nginx_config 0 100
  if ! reload_nginx; then
    fail_and_rollback "全量切换 Nginx reload 失败"
  fi
  info "Nginx 已全量指向 green"

  # 等待 10s 确保新流量稳定后再停 blue
  sleep 10

  # 停止 blue 容器
  if docker ps --format '{{.Names}}' | grep -q 'mymail-app-blue'; then
    docker stop mymail-app-blue >/dev/null 2>&1 || true
    info "blue 容器已停止（保留以便回滚: mymail-app-blue）"
  fi

  # green 升格为线上版本
  save_state "$VERSION" "$VERSION"
  info "======================================"
  info "✅ 发布完成！版本 ${VERSION} 已全量上线"
  info "   灰度比例: 100%"
  info "   旧版本 blue 已停止（可手动清理）"
  info "   回滚命令: ./scripts/rollback.sh"
  info "======================================"
}

main "$@"
