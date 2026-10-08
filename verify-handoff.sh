#!/usr/bin/env bash
# ARTEX 定制补丁 —— 移植后的自检脚本
#
# 用法：在已应用补丁的 ARTEX 仓库根目录执行
#   bash /path/to/verify-handoff.sh
#
# 退出码 0 = 全部通过；非 0 = 有失败项。
# 只读检查，不会修改任何东西。

set -uo pipefail

# 定位仓库根目录：优先用脚本所在目录（若其中有 go.mod），否则用调用者的 cwd。
# 脚本常被放在仓库外的交付目录里，故不能无条件 cd 到脚本目录。
script_dir=$(cd "$(dirname "$0")" && pwd)
if [ -f "$script_dir/go.mod" ]; then
  cd "$script_dir" || exit 1
elif [ ! -f "./go.mod" ]; then
  echo "❌ 未找到 go.mod —— 请在 ARTEX 仓库根目录运行本脚本" >&2
  exit 1
fi
info_root=$(pwd)
echo "仓库根目录：$info_root"
echo

fail=0
pass() { printf '  ✅ %s\n' "$1"; }
bad()  { printf '  ❌ %s\n' "$1"; fail=$((fail+1)); }
info() { printf '  ·  %s\n' "$1"; }

if [ "$info_root" != "$(pwd)" ]; then
  bad "无法进入仓库根目录"
  exit 1
fi

echo "=== 1. 工具链 ==="
if command -v go >/dev/null 2>&1; then
  v=$(go version | awk '{print $3}')
  case "$v" in
    go1.2[6-9]*) pass "Go $v" ;;
    *) bad "Go $v —— 需要 1.26 或更高（-tags embedui 与 go.mod 的 go 1.26.3 要求）" ;;
  esac
else
  bad "未找到 go"
fi
command -v node >/dev/null 2>&1 && pass "Node $(node -v)" || bad "未找到 node"
command -v npm  >/dev/null 2>&1 && pass "npm $(npm -v)"  || bad "未找到 npm"
if command -v rsync >/dev/null 2>&1; then info "有 rsync（可选，无则用 cp -a）"; else info "无 rsync，用 cp -a 替代"; fi

echo "=== 2. 补丁落点（关键代码存在性）==="
check() {
  f="$1"; pat="$2"; desc="$3"
  if [ ! -f "$f" ]; then bad "$desc —— 文件缺失：$f"; return; fi
  n=$(grep -c "$pat" "$f" 2>/dev/null || echo 0)
  if [ "$n" -gt 0 ]; then pass "$desc（$f 命中 $n 处）"; else bad "$desc —— $f 里找不到 '$pat'"; fi
}
check agent/constraints.go       "renderConstraintGroups"  "约束渲染：授权段可测函数"
check agent/tools.go             "type=override(操作员明确授权)只能由主 agent" "授权写入来源校验"
check guard/guard.go             "TaskAuthorizations"      "拦截层豁免：授权查表"
check intercept/review_context.go "OperatorAuthorizations" "裁判输入：独立授权字段"
check intercept/prompt.go        "operator_authorizations" "裁判提示词：授权契约"
check server/intercept.go        "wireTaskAuthorizations"  "服务端接线"
check db/schema.sql              "honor_override"           "表结构：豁免列"
check db/schema.sql              "'allow','deny','override'" "表结构：授权类型"

echo "=== 3. 新增测试文件 ==="
for f in agent/constraints_auth_test.go agent/constraints_override_test.go \
         guard/honor_override_test.go server/prompt_tools_test.go; do
  [ -f "$f" ] && pass "$f" || bad "$f 缺失"
done

echo "=== 4. 编译 ==="
if command -v go >/dev/null 2>&1; then
  if go build ./cmd/... ./agent/... ./server/... ./db/... ./intercept/... ./guard/... 2>&1; then
    pass "go build 全部通过"
  else
    bad "go build 失败"
  fi
fi

echo "=== 5. 测试 ==="
if command -v go >/dev/null 2>&1; then
  if go test ./agent/... ./intercept/... ./db/... ./guard/... -count=1 2>&1 | grep -qE "^(FAIL)"; then
    bad "agent/intercept/db/guard 有测试失败（运行上面的命令看详情）"
  else
    pass "agent / intercept / db / guard 测试通过"
  fi
  go test ./server/ -run TestPromptToolNamesExist -count=1 2>&1 | grep -q "^ok" \
    && pass "提示词工具名一致性检查通过" \
    || bad "TestPromptToolNamesExist 失败（提示词引用了不存在的工具）"
fi

echo "=== 6. 前端 ==="
if [ -d web/node_modules ]; then
  (cd web && npx tsc --noEmit -p tsconfig.json) 2>&1 | head -5
  if (cd web && npx tsc --noEmit -p tsconfig.json) >/dev/null 2>&1; then
    pass "TypeScript 类型检查通过"
  else
    bad "tsc 有错误（InterceptRule 缺 honor_override 字段是最常见原因）"
  fi
else
  info "未装前端依赖，跳过（需先 cd web && npm ci）"
fi

echo "=== 7. 运行时 ==="
PORT=${ARTEX_PORT:-8787}
if curl -s -m 5 "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
  pass "服务在线：$(curl -s -m 5 "http://127.0.0.1:${PORT}/api/health")"
else
  info "服务未响应（:${PORT}）—— 若尚未部署可忽略"
fi

# 数据库检查需要容器名，默认沿用 ARTEX 的 compose 命名
PG=${ARTEX_PG_CONTAINER:-artex-postgres-1}
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${PG}$"; then
  echo "=== 8. 数据库 ==="
  q() { docker exec "$PG" psql -U artex -d artex -t -A -c "$1" 2>/dev/null; }
  ho=$(q "SELECT column_default FROM information_schema.columns WHERE table_name='intercept_rules' AND column_name='honor_override';")
  [ "$ho" = "false" ] && pass "intercept_rules.honor_override 已建，默认 false" \
                      || bad "honor_override 列异常（读到 '$ho'）"
  kinds=$(q "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='task_constraints_kind_check';")
  case "$kinds" in
    *override*) pass "task_constraints CHECK 已含 override" ;;
    *) bad "task_constraints CHECK 未含 override（读到 '$kinds'）" ;;
  esac
  on=$(q "SELECT count(*) FROM intercept_rules WHERE honor_override;")
  [ "$on" = "0" ] && info "honor_override 开启数：0（与升级前行为一致）" \
                  || info "honor_override 开启数：$on（已手动开启过规则）"
  over=$(q "SELECT count(*) FROM task_constraints WHERE kind='override';")
  info "override 授权条目数：$over"
else
  info "未找到数据库容器 ${PG}，跳过数据库检查"
fi

echo "=== 9. 部署配置（断供自检）==="
# 上游 Docker Hub 镜像 autumn27/artex 已下线。任何仍指向它的配置都会让新机
# 在 docker compose pull 阶段直接失败，这是实际踩过的坑，所以固化成检查项。
compose_img=$(grep -E "^\s+image:" docker-compose.yml 2>/dev/null | grep -v postgres | head -1 | sed 's/.*image:[[:space:]]*//' || true)
case "$compose_img" in
  *autumn27/artex*)
    bad "docker-compose.yml 仍指向已下线的 $compose_img —— 新机无法拉取。
         改成 \${ARTEX_IMAGE:-artex}:\${ARTEX_TAG:-custom}（本地构建产物）" ;;
  ''|*:*) pass "docker-compose.yml 镜像：${compose_img:-未指定}" ;;
esac
if [ -f .env ] && grep -qE '^ARTEX_TAG=latest' .env 2>/dev/null; then
  bad ".env 里 ARTEX_TAG=latest —— 与本地构建标签(custom)不符，compose 找不到镜像"
elif [ -f .env ]; then
  pass ".env 镜像标签：$(grep -E '^ARTEX_TAG=' .env | head -1 | cut -d= -f2-)"
else
  info "无 .env（新机首次部署时用 .env.example 创建）"
fi
base_from=$(grep -E "^FROM " Dockerfile.local 2>/dev/null | head -1 | awk '{print $2}' || true)
case "$base_from" in
  *autumn27/artex*)
    bad "Dockerfile.local 基础镜像仍是 $base_from —— 该源已 404，改用 ghcr.io/darkkid0/artex:base@sha256:…" ;;
  *ghcr.io/darkkid0/artex*)
    pass "Dockerfile.local 基础镜像：${base_from##*@}" ;;
  '') info "无 Dockerfile.local" ;;
esac
for f in build-image.sh build.sh install.sh .env.example; do
  [ -f "$f" ] && pass "$f 就位" || bad "缺 $f"
done

echo
if [ "$fail" -eq 0 ]; then
  echo "全部通过。"
else
  echo "有 $fail 项失败 —— 见上方 ❌ 标记。"
fi
exit "$fail"