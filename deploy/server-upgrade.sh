#!/bin/sh
# 在生产服务器上原地构建并升级 AI Bot 容器。
#
# 用法（在服务器上，源码目录中执行）：
#   sh deploy/server-upgrade.sh [部署目录，默认 /opt/ai-bot/deploy]
#
# 步骤：备份数据库 → 构建新镜像 → 保留旧镜像用于回滚 → 重建容器 → 健康检查。
# 健康检查失败时自动回滚到旧镜像（数据库迁移只新增表/列，旧版本可继续运行）。
set -eu

DEPLOY_DIR="${1:-/opt/ai-bot/deploy}"
SRC_DIR="$(cd "$(dirname "$0")/.." && pwd)"
STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="${BACKUP_DIR:-/opt/ai-bot/backups}"

[ -f "$DEPLOY_DIR/.env" ] || { echo "找不到 $DEPLOY_DIR/.env"; exit 1; }
cd "$DEPLOY_DIR"
IMAGE="$(grep -E '^AI_BOT_IMAGE=' .env | cut -d= -f2- || true)"
IMAGE="${IMAGE:-ai-bot:latest}"
DATABASE_URL="$(grep -E '^DATABASE_URL=' .env | cut -d= -f2-)"

echo "==> 1/5 备份数据库到 $BACKUP_DIR"
mkdir -p "$BACKUP_DIR"
HOST_DB_URL="$(echo "$DATABASE_URL" | sed 's/host\.docker\.internal/127.0.0.1/')"
if command -v pg_dump >/dev/null 2>&1 && pg_dump "$HOST_DB_URL" -Fc -f "$BACKUP_DIR/ai_bot-$STAMP.dump"; then
  :
elif command -v sudo >/dev/null 2>&1 && sudo -u postgres pg_dump -Fc ai_bot > "$BACKUP_DIR/ai_bot-$STAMP.dump"; then
  :
else
  echo "数据库备份失败，已停止升级（不会在没有备份的情况下迁移数据库）"
  exit 1
fi
ls -lh "$BACKUP_DIR/ai_bot-$STAMP.dump"

echo "==> 2/5 构建新镜像"
docker build -t "ai-bot:$STAMP" "$SRC_DIR"

echo "==> 3/5 保留旧镜像用于回滚"
if docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker tag "$IMAGE" "ai-bot:rollback-$STAMP"
  ROLLBACK="ai-bot:rollback-$STAMP"
else
  ROLLBACK=""
fi
docker tag "ai-bot:$STAMP" "$IMAGE"

echo "==> 4/5 重建容器"
docker compose up -d --force-recreate app

echo "==> 5/5 健康检查"
ok=""
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then ok=1; break; fi
  sleep 2
done
if [ -z "$ok" ]; then
  echo "健康检查失败，最近日志："
  docker compose logs --tail 80 app || true
  if [ -n "$ROLLBACK" ]; then
    echo "==> 回滚到 $ROLLBACK"
    docker tag "$ROLLBACK" "$IMAGE"
    docker compose up -d --force-recreate app
  fi
  exit 1
fi
echo "升级完成：$IMAGE（构建 ai-bot:$STAMP），回滚镜像：${ROLLBACK:-无}，数据库备份：$BACKUP_DIR/ai_bot-$STAMP.dump"
docker compose logs --tail 20 app
