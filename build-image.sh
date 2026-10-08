#!/usr/bin/env bash
# 一键构建可部署镜像：预检工具链 → 编译二进制 → docker build。
#
# 这是新机部署的主入口。之所以需要它：官方原版的流程依赖已下线的
# autumn27/artex 预构建镜像，新机上 `docker compose pull` 必然失败；
# 本仓库改为从源码自建，基础镜像取自公开的 ghcr.io/darkkid0/artex:base。
#
# 用法：
#   ./build-image.sh                    # 用 .env 里的镜像名构建
#   ./build-image.sh -t mytag           # 构建为 artex:mytag
#   ./build-image.sh -i myrepo/img      # 构建为 myrepo/img:mytag
#   ./build-image.sh --check            # 只跑预检，不构建
#   ./build-image.sh --skip-frontend    # 复用已有的 server/webui/dist
#
# 产物：docker 镜像（默认 artex:custom），随后 `docker compose up -d` 即可。
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){ printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){ printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

IMAGE=""
TAG=""
CHECK_ONLY=0
SKIP_FRONTEND=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    -i|--image) IMAGE="${2:-}"; shift 2 ;;
    -t|--tag)   TAG="${2:-}";   shift 2 ;;
    --check)          CHECK_ONLY=1; shift ;;
    --skip-frontend)  SKIP_FRONTEND=1; shift ;;
    -h|--help)
      sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) die "未知参数：$1（用 --help 查看用法）" ;;
  esac
done

# ── 镜像名：命令行 > .env > 默认值 ────────────────────────────────
env_get() { # env_get KEY DEFAULT
  local v=""
  [ -f .env ] && v="$(grep -E "^$1=" .env 2>/dev/null | head -1 | cut -d= -f2- || true)"
  v="${v%\"}"; v="${v#\"}"; v="${v%\'}"; v="${v#\'}"
  printf '%s' "${v:-$2}"
}
[ -n "$IMAGE" ] || IMAGE="$(env_get ARTEX_IMAGE artex)"
[ -n "$TAG" ]   || TAG="$(env_get ARTEX_TAG custom)"

# ── 预检 ────────────────────────────────────────────────────────
# 这些是实测会挡住构建的东西，逐条给出可执行的修复命令，而不是让构建
# 在后面某处报一句看不懂的错。
MISSING=()
command -v docker >/dev/null 2>&1 || MISSING+=("docker|docker|https://docs.docker.com/engine/install/")
command -v go     >/dev/null 2>&1 || MISSING+=("go|apt-get install -y golang-go  # 或 https://go.dev/dl/（需 1.26+）")

HAVE_NPM=0
command -v npm >/dev/null 2>&1 && HAVE_NPM=1
if [ "$SKIP_FRONTEND" -eq 0 ] && [ "$HAVE_NPM" -eq 0 ]; then
  MISSING+=("npm|apt-get install -y nodejs npm  # 或 nvm install 20")
fi
command -v zip >/dev/null 2>&1 || warn "未检测到 zip：仅影响 --package/--release 打包，不影响本脚本"

if [ "${#MISSING[@]}" -gt 0 ]; then
  printf '\033[31m[x]\033[0m 缺少前置依赖：\n' >&2
  for m in "${MISSING[@]}"; do
    printf '      - %-8s → %s\n' "${m%%|*}" "${m#*|}" >&2
  done
  die "装齐后重试"
fi

# Go 版本要够高，否则 go.mod 的 go 1.26.x 指令直接报错
GO_VER="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
GO_MAJOR="${GO_VER%%.*}"; GO_REST="${GO_VER#*.}"; GO_MINOR="${GO_REST%%.*}"
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 26 ]; }; then
  die "Go 版本过低：当前 ${GO_VER}，本项目需要 1.26+（否则 go.mod 的 go 1.26.x 指令会直接报错）"
fi

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64|amd64) GOARCH_TARGET=amd64 ;;
  aarch64|arm64) GOARCH_TARGET=arm64 ;;
  *) die "不支持的架构：$HOST_ARCH" ;;
esac

info "Go ${GO_VER} · 目标 linux/${GOARCH_TARGET} · 镜像 ${IMAGE}:${TAG}"

if [ "$CHECK_ONLY" -eq 1 ]; then
  ok "预检通过（--check，未做任何构建）"
  echo "下一步：./build-image.sh"
  exit 0
fi

if [ "$SKIP_FRONTEND" -eq 1 ]; then
  [ -d server/webui/dist ] || die "--skip-frontend 但 server/webui/dist 不存在"
  ok "复用已有前端产物"
else
  command -v npm >/dev/null 2>&1 || die "--skip-frontend 需要先构建过一次前端"
  if [ ! -d web/node_modules ]; then
    info "安装前端依赖（首次较慢）"
    (cd web && npm ci)
  else
    ok "前端依赖已就位"
  fi
  info "构建前端静态资源"
  (cd web && npm run build:static)
fi

# `npm run build:static` 产出在 web/out/，而 server/webui_embed.go 的
# //go:embed all:webui/dist 要求它们位于 server/webui/dist/ —— 这一步漏了
# go build 会直接报 "pattern all:webui/dist: no matching files found"。
if [ "$SKIP_FRONTEND" -eq 0 ]; then
  [ -d web/out ] || die "前端构建未产出 web/out/"
  info "同步前端资源到 server/webui/dist"
  mkdir -p server/webui/dist
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete web/out/ server/webui/dist/
  else
    find server/webui/dist -mindepth 1 -delete
    cp -a web/out/. server/webui/dist/
  fi
fi
[ -n "$(ls -A server/webui/dist 2>/dev/null || true)" ] || die "server/webui/dist 为空，-tags embedui 会编译失败"

# ── 编译二进制 ──────────────────────────────────────────────────
info "编译后端二进制"
mkdir -p "dist/${GOARCH_TARGET}"
CGO_ENABLED=0 GOARCH="$GOARCH_TARGET" \
  go build -tags embedui -trimpath \
  -ldflags "-s -w -buildid= -X main.version=$(git rev-parse --short HEAD 2>/dev/null || echo local)" \
  -o "dist/${GOARCH_TARGET}/artex" ./cmd/artex
ok "二进制 dist/${GOARCH_TARGET}/artex ($(du -h "dist/${GOARCH_TARGET}/artex" | cut -f1))"

# ── 构建镜像 ────────────────────────────────────────────────────
info "构建镜像 ${IMAGE}:${TAG}（基础镜像 ghcr.io/darkkid0/artex:base，首次会拉取 ~2.4GB）"
docker build -f Dockerfile.local -t "${IMAGE}:${TAG}" .

ok "构建完成：${IMAGE}:${TAG}"
cat <<EOF

下一步：
  cp .env.example .env      # 若还没有 .env
  编辑 .env 填 POSTGRES_PASSWORD
  docker compose up -d
  → http://localhost:8787

镜像名与 .env 不一致时，在 .env 里设：
  ARTEX_IMAGE=${IMAGE}
  ARTEX_TAG=${TAG}
EOF