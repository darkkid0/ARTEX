#!/usr/bin/env bash
# ARTEX 更新脚本：① Docker 更新（拉新镜像重建）  ② 本地编译更新（重建二进制）
# 与 install.sh 对应：install 负责首次落地，update 负责升级到新版本。
# DB 迁移无需手动执行——artex 每次启动都会幂等重跑 schema.sql（含 ADD COLUMN/CREATE
# INDEX IF NOT EXISTS），所以“重启即迁移”。数据（pgdata 卷、./data、./skills）不受影响。
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }

# ── 可选：同步仓库到最新代码（compose/脚本/本地编译源码都靠它更新）───────
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { warn "非 git 工作副本，跳过 git pull"; return; }
  [ "$(ask '拉取最新代码 (git pull --ff-only)? (y/n)' y)" = y ] || return
  if ! git pull --ff-only; then
    warn "git pull 未能快进（本地有改动或分支分叉）——请手动处理后重试，本次沿用当前代码"
  fi
}

# ── ① Docker 更新 ───────────────────────────────
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
    || die "未检测到 docker / docker compose，请先用 ./install.sh 安装部署"
  [ -f .env ] || die "未找到 .env，请先运行 ./install.sh 完成首次部署"

  # 可选：升级到指定版本 tag（不填则沿用 .env 中的 ARTEX_TAG，缺省为 latest）
  local tag; tag="$(ask '目标镜像 tag（回车沿用 .env / latest）' '')"
  if [ -n "$tag" ]; then
    if grep -q '^ARTEX_TAG=' .env; then
      sed -i.bak "s|^ARTEX_TAG=.*|ARTEX_TAG=${tag}|" .env && rm -f .env.bak
    else
      printf '\nARTEX_TAG=%s\n' "$tag" >> .env
    fi
    ok "已将 ARTEX_TAG 设为 ${tag}"
  fi

  # 只动 artex：postgres 是固定的 16-alpine，不需要跟着升级（拉它纯属浪费带宽，
  # 且大版本变动还会有兼容风险）。artex 声明了 depends_on postgres，所以带服务名
  # up 时若 pg 没起会自动拉起，已在跑的则原样保留、不重建。
  #
  # 镜像来源分两种，不能一律 pull：
  #   · 本地构建产物（默认 artex:custom）—— registry 里根本没有这个 tag，pull 必然失败。
  #     正确做法是重新构建（build-image.sh 会自动按宿主架构选 GOARCH 并复用已装的前端依赖）。
  #   · 远端镜像（.env 里 ARTEX_IMAGE 指向 ghcr.io 等）—— 正常 pull。
  local img_name
  img_name="$(grep -E '^ARTEX_IMAGE=' .env 2>/dev/null | head -1 | cut -d= -f2- | tr -d '"'\''[:space:]' || true)"
  img_name="${img_name:-artex}"

  case "$img_name" in
    */*)  # ghcr.io/... 之类，属远端镜像
      info "拉取新镜像 ${img_name}（仅 artex）…"
      docker compose pull artex || die "拉取失败：${img_name}。请检查 .env 的 ARTEX_IMAGE/ARTEX_TAG 与网络"
      ;;
    *)
      warn "ARTEX_IMAGE=${img_name}（本地镜像名，无对应远端仓库）—— 改为从源码重新构建"
      local build_tag="${tag:-}"
      [ -n "$build_tag" ] || build_tag="$(grep -E '^ARTEX_TAG=' .env | head -1 | cut -d= -f2- | tr -d '"'\''[:space:]' || true)"
      [ -n "$build_tag" ] || build_tag="custom"
      info "重新构建镜像 ${img_name}:${build_tag}…"
      ./build-image.sh -i "$img_name" -t "$build_tag" \
        || die "构建失败，请检查上方输出（工具链缺失时可用 ./build-image.sh --check 自检）"
      ;;
  esac

  info "重建并启动（artex 重启时自动迁移 schema）…"
  docker compose up -d artex
  ok "更新完成 → http://localhost:8787"
  info "查看日志：docker compose logs -f artex"
  info "清理旧镜像（可选）：docker image prune -f"
}

# ── ② 本地编译更新 ──────────────────────────────
update_local(){
  command -v go >/dev/null 2>&1 || die "未检测到 Go（>=1.26）：https://go.dev/dl/"
  [ -f config.json ] || warn "未找到 config.json——若首次部署请改用 ./install.sh"
  ok "Go: $(go version)"

  if command -v npm >/dev/null 2>&1; then
    info "重建前端静态产物…"
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "重新编译内嵌单二进制…"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "未检测到 npm：编译**不内嵌前端**的后端（前端需另跑 npm run dev）"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "编译完成 → ./artex"
  warn "请重启正在运行的 artex 进程以生效（重启时会自动迁移 schema）"
}

echo "=============================="
echo "  ARTEX 更新"
echo "  1) Docker 更新（拉新镜像重建）"
echo "  2) 本地更新（go 重新编译）"
echo "=============================="
case "$(ask '选择' 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "无效选择" ;;
esac
