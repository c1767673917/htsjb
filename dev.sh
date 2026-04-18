#!/usr/bin/env bash
# 开发模式一键启动前后端
# - 后端: go run ./cmd/server -config ./config.yaml  (监听 :8080)
# - 前端: cd frontend && npm run dev                 (监听 :5173, 代理 /api /files 到后端)
# Ctrl-C 可同时停止两端。

set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

LOG_DIR="$ROOT_DIR/.dev-logs"
mkdir -p "$LOG_DIR"
BACKEND_LOG="$LOG_DIR/backend.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"

BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
  echo ""
  echo "[dev] 正在停止前后端..."
  if [[ -n "$FRONTEND_PID" ]] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
    kill "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [[ -n "$BACKEND_PID" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
  # 给进程一点时间优雅退出,再强制清理整个进程组
  sleep 1
  [[ -n "$FRONTEND_PID" ]] && kill -9 "$FRONTEND_PID" 2>/dev/null || true
  [[ -n "$BACKEND_PID" ]] && kill -9 "$BACKEND_PID" 2>/dev/null || true
  # 清理日志转发 pipeline 的残留子进程(tail -F | sed 两侧都是 $$ 的直接子进程)
  pkill -P $$ tail 2>/dev/null || true
  pkill -P $$ sed 2>/dev/null || true
  echo "[dev] 已退出。日志保留在 $LOG_DIR"
}
trap cleanup EXIT INT TERM

# --- 依赖检查 ---
command -v go >/dev/null 2>&1 || { echo "[dev] 未检测到 go,请先安装 Go。"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "[dev] 未检测到 npm,请先安装 Node.js。"; exit 1; }

# --- 前端依赖 ---
if [[ ! -d "$ROOT_DIR/frontend/node_modules" ]]; then
  echo "[dev] 首次运行,安装前端依赖 (npm install)..."
  (cd "$ROOT_DIR/frontend" && npm install) || { echo "[dev] npm install 失败"; exit 1; }
fi

# --- 启动后端 ---
echo "[dev] 启动后端: go run ./cmd/server -config ./config.yaml  (日志 -> $BACKEND_LOG)"
( go run ./cmd/server -config ./config.yaml ) >"$BACKEND_LOG" 2>&1 &
BACKEND_PID=$!

# --- 启动前端 ---
echo "[dev] 启动前端: npm run dev  (日志 -> $FRONTEND_LOG)"
( cd "$ROOT_DIR/frontend" && npm run dev ) >"$FRONTEND_LOG" 2>&1 &
FRONTEND_PID=$!

echo ""
echo "[dev] 后端 PID=$BACKEND_PID  http://127.0.0.1:8080"
echo "[dev] 前端 PID=$FRONTEND_PID  http://127.0.0.1:5173"
echo "[dev] 合并日志输出中,Ctrl-C 退出。"
echo "------------------------------------------------------------"

# 合并日志到终端,带上前缀便于区分
tail -n 0 -F "$BACKEND_LOG"  | sed -u 's/^/[backend]  /' &
tail -n 0 -F "$FRONTEND_LOG" | sed -u 's/^/[frontend] /' &

# 任一进程退出就整体收尾
while kill -0 "$BACKEND_PID" 2>/dev/null && kill -0 "$FRONTEND_PID" 2>/dev/null; do
  sleep 1
done

# tail/sed pipeline 交给 cleanup 通过 pkill -P 统一清理

if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
  echo "[dev] 后端进程已退出,查看 $BACKEND_LOG"
fi
if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
  echo "[dev] 前端进程已退出,查看 $FRONTEND_LOG"
fi
