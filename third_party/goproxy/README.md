# 本地 Go 模块代理（norma 镜像）

## 为什么有这东西

`go.mod` 依赖 `github.com/Autumn-27/norma v0.4.3`，而**该上游仓库已不可达（HTTP 404）**，
与 `Autumn-27/ARTEX` 同批消失。仓库内没有 `vendor/`，所以新机编译原本完全依赖公共模块代理
是否仍缓存着这个版本。

Go 代理**必须响应 takedown 请求** —— 上游作者仍保有该仓库控制权，也就保留了发起 takedown 的
能力。一旦发起，`proxy.golang.org` / `goproxy.cn` 都会删除该模块，届时**所有新机都会编译失败**，
且没有任何退路。

这里存放该模块 v0.4.3 的完整副本（`.info` / `.mod` / `.zip` / `list`），构成一个符合
Go module proxy 协议的文件式代理。

## 目录结构

```
third_party/goproxy/github.com/!autumn-27/norma/@v/
├── list           版本列表
├── v0.4.3.info    版本元信息
├── v0.4.3.mod     该版本的 go.mod
└── v0.4.3.zip     模块源码归档
```

`!autumn-27` 的大小写转义是 Go 模块代理协议的规定：路径中的大写字母编码为 `!` + 小写。

## 怎么用

**不需要手工设置。** `build-image.sh` 会自动把本目录注入 `GOPROXY` 链的首位：

```bash
GOPROXY="file://$(pwd)/third_party/goproxy,<原有代理链>"
```

语义是「先查本地副本，查不到再走公共代理」。因此：

- norma 从本目录解析
- 其余 50 个依赖照常从 goproxy.cn / proxy.golang.org 取

手工构建时同理：

```bash
GOPROXY="file://$PWD/third_party/goproxy,https://goproxy.cn,direct" go build ./cmd/artex
```

## 安全性说明

**校验未被削弱。** `go.sum` 里已有 norma v0.4.3 的哈希，Go 会用 `go.sum` 做**本地**比对，
无需访问 `sum.golang.org`，因此**不需要 `GOSUMDB=off`**、`GONOSUMDB` 或任何跳过校验的设置。

已实测：模块从缓存中删除 + `GOPROXY="file://…,off"`（完全断网）+ 空模块缓存 →
`go build` 成功。

## 来源与许可

镜像自公共 Go 模块代理，内容与 `github.com/Autumn-27/norma@v0.4.3` 逐字节一致
（哈希可用 `go mod download` 或 `dirhash` 复核）。采集时间 2026-10-08，
彼时该上游仓库已 404。

**上游模块未附带任何 LICENSE 文件**，因此其再分发授权状态不明。本目录存在的目的是保证
**本项目自身可继续构建**（可用性需要），而非对外再分发。若要把本目录推送到公开仓库，
请先确认已获得相应授权。

仅镜像 `norma` 一个模块：它自身的依赖（`creack/pty`、`hinshun/vt10x`、`golang.org/x/net`、
`gopkg.in/yaml.v3`）均为第三方社区库，作者无消失动机；且我们解析出的 `x/net v0.58.0`
高于其声明的 `v0.55.0`，要求已被满足。

## 维护

若将来升级 norma 版本：

```bash
V=v0.4.4
D="third_party/goproxy/github.com/!autumn-27/norma/@v"
curl -sS -o "$D/$V.info" "https://goproxy.cn/github.com/!autumn-27/norma/@v/$V.info"
curl -sS -o "$D/$V.mod"  "https://goproxy.cn/github.com/!autumn-27/norma/@v/$V.mod"
curl -sS -o "$D/$V.zip"  "https://goproxy.cn/github.com/!autumn-27/norma/@v/$V.zip"
printf '{"Version":"%s"}\n' "$V" > "$D/list"
```

同时更新 `go.mod` 的 require 行，然后 `go mod tidy` 会写入新的 `go.sum` 哈希。
`verify-handoff.sh` 第 9 项会校验本目录与 `go.mod` 声明的版本是否一致。