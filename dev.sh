#!/usr/bin/env bash
# ローカルデモ（API + 管理画面）を起動し、ブラウザーで開く。
#   ./dev.sh             チュートリアル付きで開く（?tutorial=1）
#   ./dev.sh calendar    指定したページを開く（例: tasks, settings）
#   NO_OPEN=1 ./dev.sh   ブラウザーを開かない
# Goが無い場合は .local/go に取得する（システムには入れない）。
set -euo pipefail
cd "$(dirname "$0")"

GO_VERSION=1.27.3
URL="http://127.0.0.1:4200/${1:-?tutorial=1}"

if ! command -v go >/dev/null 2>&1; then
  if [ ! -x .local/go/bin/go ]; then
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)
    case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
    echo "Go が見つからないため go${GO_VERSION} (${os}-${arch}) を .local/go に取得します…"
    mkdir -p .local
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.${os}-${arch}.tar.gz" | tar xz -C .local
  fi
  export PATH="$PWD/.local/go/bin:$PATH"
fi

[ -d node_modules ] || npm ci

for port in 4200 8080; do
  if lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "ポート $port は使用中です。先に止めてください: lsof -iTCP:$port -sTCP:LISTEN" >&2
    exit 1
  fi
done

if [ -z "${NO_OPEN:-}" ]; then
  (
    for _ in $(seq 1 180); do
      if curl -fs http://127.0.0.1:4200/api/health >/dev/null 2>&1; then
        echo "→ $URL"
        if command -v open >/dev/null; then open "$URL"; else xdg-open "$URL" >/dev/null 2>&1 || true; fi
        exit 0
      fi
      sleep 1
    done
    echo "起動を確認できませんでした。手動で $URL を開いてください。" >&2
  ) &
fi

echo "停止は Ctrl+C。デモのデータはAPIのメモリーにあり、停止すると消えます。"
exec npm run dev
