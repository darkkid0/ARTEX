# ARTEX 定制版 —— 部署与维护

本仓库是 ARTEX 的定制分支，包含一组自研改动（操作员授权机制 + 提示词修复）。
相对官方版的行为差异见文末「与官方版的差异」。

- 上游：`Autumn-27/ARTEX`
- 本定制版分支：`main`（`artex-custom` 是同一 commit 的别名）

## 新机器部署

### 1. 取代码

```bash
git clone https://github.com/darkkid0/ARTEX.git   # 默认分支 main 即定制版
cd ARTEX
git log --oneline -1        # 确认是定制提交，非官方 fd86f3c
```

### 2. 装工具链

需要 Go 1.26+ 与 Node 20+。网络受限时用镜像源：

```bash
export PATH=/usr/local/go/bin:$PATH
export GOPROXY=https://goproxy.cn,direct

# Go（go.dev 直连不通时）
curl -O https://mirrors.aliyun.com/golang/go1.26.8.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.26.8.linux-amd64.tar.gz

# Node 20
curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt-get install -y nodejs
npm config set registry https://registry.npmmirror.com/
```

### 3. 编译

```bash
cd web && npm ci && npm run build:static && cd ..

mkdir -p server/webui/dist
rsync -a --delete web/out/ server/webui/dist/      # 无 rsync 用 cp -a

mkdir -p dist/amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embedui -trimpath \
  -o dist/amd64/artex ./cmd/artex
```

`-tags embedui` 会把 `server/webui/dist/` 内嵌进二进制，所以前端必须先构建。

### 4. 构建镜像

**必须用 `Dockerfile.local`**，不能用仓库自带的 `Dockerfile` —— 后者要拉 dockerfile
frontend、python 基础镜像和 apt/Playwright 全套，受限网络下跑不通。理由见该文件头部注释。

```bash
docker build -f Dockerfile.local -t artex:custom .
```

`Dockerfile.local` 的基础镜像：

```
ghcr.io/darkkid0/artex:base@sha256:5d6eb7231231af84c7da357d36c9e1fcd46dd881a3058d9185e08481cb624e10
```

用 **digest 而非 tag** 锁定 —— tag 随时可能被覆盖，digest 不会。该镜像只含运行时
工具链（python / node 20 / playwright / ripgrep / nmap 等），`Dockerfile.local` 只往里
替换 `/app/artex` 一个二进制。

**若 `docker pull` 失败，三条退路**（按优先级）：

```bash
# ① 直接用我们的镜像
docker pull ghcr.io/darkkid0/artex:base

# ② 离线存档（~611MB，与本仓库同批维护）
gunzip -c artex-base-2.37G.tar.gz | docker load
docker tag autumn27/artex:latest ghcr.io/darkkid0/artex:base

# ③ 网络正常时改用仓库自带的 Dockerfile 从头构建
#    不依赖任何人的镜像，但需拉 python 基础镜像 + apt/Playwright 全套
```

> **关于上游**：官方仓库（`Autumn-27/ARTEX`，后改名 `mssky9527/ARTEX`）与其 Docker Hub
> 镜像（`autumn27/artex`）目前均已不可用。本仓库已不依赖其中任何一个——代码在
> `darkkid0/ARTEX`，运行时在 `ghcr.io/darkkid0/artex:base`，两者都由我们自行维护。

### 5. 部署

`.env`：
```
ARTEX_TAG=custom
```

`docker-compose.yml` 的 artex 服务镜像名须指向本仓库名：
```diff
-   image: autumn27/artex:${ARTEX_TAG:-latest}
+   image: artex:${ARTEX_TAG:-custom}
```

不改这行的话 compose 会去找 `autumn27/artex:custom`，那个 tag 不存在。

```bash
docker compose up -d --force-recreate artex
```

### 6. 验证

```bash
bash verify-handoff.sh
```

检查工具链、关键代码落点、编译、测试、前端类型、运行时健康、数据库迁移共 8 类，
全绿才算部署成功。退出码非 0 表示有失败项。

## 后续开发

上游已不可用，本仓库即事实上的上游。日常开发流程：

```bash
git clone https://github.com/darkkid0/ARTEX.git
cd ARTEX
git checkout -b <你的分支>
# ...开发...
go build ./cmd/... ./agent/... ./server/... ./db/... ./intercept/... ./guard/...
go test ./agent/... ./intercept/... ./db/... ./guard/... -count=1
git push fork <你的分支>
```

**本仓库的 remote 约定**：

| 名称 | 指向 | 用途 |
|---|---|---|
| `fork` | `darkkid0/ARTEX` | 推送目标，日常 `git push fork <branch>` |
| `origin` | 原官方地址 | **已失效**，保留仅为记录；`git fetch origin` 会 404 |

合并回主线：
```bash
git checkout main && git merge <你的分支>
git push fork main:main
```

若官方仓库将来恢复，把新地址加回来即可（历史不冲突）：
```bash
git remote add upstream <新地址>
git fetch upstream --tags
git merge upstream/main
```

## 备份

以下资产需自行保管，GitHub 与 Docker Hub 都不保证长期可用：

| 资产 | 位置 |
|---|---|
| 运行时基础镜像 | `ghcr.io/darkkid0/artex:base`（digest 见「编译与构建镜像」） |
| 同上离线副本 | `artex-base-2.37G.tar.gz`（~611MB，含 `.sha256`） |
| 定制版镜像 | `ghcr.io/darkkid0/artex:<commit>`，或本地 `artex:custom` |
| 数据库 | `docker exec artex-postgres-1 pg_dump -U artex -d artex -Fc > backup.dump` |

## 数据库迁移

**无需手动执行。** `db/schema.sql` 中两条迁移是幂等的，服务启动时自动应用：

- `task_constraints` 的 CHECK 增加 `'override'`（操作员授权）
- `intercept_rules` 增加 `honor_override` 列（默认 false）

另有一次性迁移会在首次启动时把内置 agent 的提示词刷成代码默认（日志里可见
`[prompts] xxx 提示词已追加新默认版本`）。旧版本仍保留在 `agent_prompts` 历史里。

**升级前建议备份数据库**：

```bash
docker exec artex-postgres-1 pg_dump -U artex -d artex -Fc > before-upgrade.dump
```

## 同步上游

```bash
git fetch origin                # origin 指向官方 Autumn-27/ARTEX
git merge origin/main
go test ./agent/... ./intercept/... ./db/... ./guard/... -count=1
git push fork artex-custom:main  # fork 指向 darkkid0/ARTEX
```

冲突只可能落在 `db/schema.sql`、`intercept/prompt.go`、`server/orchestration.go`
—— 本仓库改过这三处，而上游 v0.3.15 仅动CHANGELOG，短期内不会冲突。

**改内置 agent 提示词默认值后，务必同步 `server/orchestration.go` 的
`builtinPromptReseedTargets()` 并 bump `reseedBuiltinPrompts` 的 flag**，
否则存量库不会更新。`TestReseedCoversEveryBuiltinAgent` 会强制这个不变量。

## 与官方版的差异

### 1. 操作员授权（task_constraints.kind='override'）

约束表新增第三种类型。`allow`/`deny` 框定探索边界，`override` 是操作员对具体动作的
明确放行，**优先于前两者**，并排在提示词最前（`agent/constraints.go`）。

**只有 `origin='human'` 能写 override** —— goals / planner / worker 一律被拒
（`agent/tools.go` 的 `addOneConstraint`），模型无法给自己发授权。

### 2. 拦截豁免（intercept_rules.honor_override，默认全关）

原先操作员授权只在模型决定阶段生效：命中 deny 规则时 guard 直接 block，LLM 裁判
根本没被调用。现在对 `honor_override=true` 的规则，会把该任务的授权交给裁判复核，
确认覆盖才放行（`guard/guard.go` 的 `applyIntercept`）。

授权走 `ReviewInput.operator_authorizations` 独立字段（Version 4→5），**不塞
background** —— `JudgeContextBoundary` 明文禁止 background 改变裁决。

**默认全部关闭，与官方版行为完全一致。** 要开启须在「系统 → 命令拦截」逐条勾选。

### 3. 提示词修复

- 悬空工具引用：`asset_neighbors`（该工具从未被实现）与 `update_skill`
  （实际名为 `update_skill_file`）已从提示词与 `node_detail` 报错中移除
- 消除了 planner / reporter / auto 内部的自相矛盾条款
- `server/prompt_tools_test.go` 会把提示词里所有 snake_case 名字与真实工具清单
  对撞，防止再引入悬空引用

### 4. 回归测试

`server/prompt_tools_test.go`、`server/prompt_reseed_test.go`、
`agent/constraints_auth_test.go`、`agent/constraints_override_test.go`、
`guard/honor_override_test.go`

## 已知遗留（官方版同样存在，非本仓库引入）

1. **20 条内置拦截规则中 19 条处于禁用状态** —— `rm -rf`、`dd of=/dev/`、
   fork 炸弹、`DROP DATABASE`、`TRUNCATE`、Mongo drop、Redis FLUSH 均未生效。
   官方播种逻辑（`db/db.go`）就是这么写的。详见 `BUILTIN_RULES.md`。

2. **`goals` 会把输出格式要求误抽成操作约束** —— 例如「全程必须使用中文回复」被登记为
   `deny`，进入「最高优先级红线」清单。修法是加第四种类型 `note`（单独渲染、不参与
   操作边界判定），尚未实现。

3. **`honor_override` 只能压过规则层，压不过模型自身的价值判断** —— 若模型认为某个
   动作超出授权范围而自我否决，授权条目无法覆盖。