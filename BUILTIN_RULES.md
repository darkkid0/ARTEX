# 附件：内置拦截规则清单（移植后请逐条确认启用状态）

来源：本机生产库实测，基准与另一台服务器一致（官方 v0.3.15 播种逻辑见 `db/db.go`
的 `seedDefaultInterceptRules` / `seedDefaultInterceptRulesV3`）。

**20 条 `[内置]` deny 规则中，只有 1 条是启用的。** 其余 19 条在官方播种时就被写成
`enabled=false` —— 这是升级前就存在的状态，**不是本次补丁造成的**。

`honor_override` 一列当前全为 `false`（本次补丁新增的列，默认关闭）。

## P100 —— 破坏本机（10 条，全部禁用）

| # | 规则名 | 匹配 | 启用 |
|---|---|---|---|
| 1 | 递归强制删除 rm -rf | `\brm\b.{0,80}(?:-[a-z]*r[a-z]*f[a-z]*\|--recursive\|--no-preserve-root)` | ❌ 禁用 |
| 2 | 删除系统关键目录 | `\brm\b[^"'\n]{0,60}["'\s](/\|/etc\|/bin\|/usr\|/boot\|/var\|/lib\|/sys\|/proc\|/dev\|/sbin\|/root)` | ❌ 禁用 |
| 3 | 磁盘格式化 mkfs | `\bmkfs\b` | ❌ 禁用 |
| 4 | 覆写磁盘设备 dd | `\bdd\b[^|\n]{0,100}\bof=\s*/dev/[a-zA-Z]` | ❌ 禁用 |
| 5 | Fork 炸弹 | `:\(\)\s*\{[^}]*:\|:` | ❌ 禁用 |
| 6 | 关机 / 重启 | `\b(?:shutdown\|reboot\|halt\|poweroff\|init\s+[06])\b` | ❌ 禁用 |
| 7 | 杀死全部进程 | `\bkill\s+-9\s+-1\b\|\bkillall\s+-9\b` | ❌ 禁用 |
| 8 | 磁盘擦除 shred / wipe | `\b(?:shred\|wipe)\b[^|\n]{0,80}/dev/[a-zA-Z]` | ❌ 禁用 |
| 9 | 清空防火墙规则 | `\biptables\s+(?:-F\|--flush)\b\|\bnft\s+flush\s+ruleset\b` | ❌ 禁用 |
| 10 | 破坏性系统命令 | 上述的合并正则（`rm -rf /`、mkfs、`dd if=`、fork 炸弹、reboot、`>/dev/sd`） | ❌ 禁用 |

> 第 10 条与第 1–8 条逐条重复，属冗余规则，不影响功能。

## P90 —— 破坏目标数据库（4 条，全部禁用）

| # | 规则名 | 匹配 | 启用 |
|---|---|---|---|
| 11 | SQL DROP DATABASE / TABLE / SCHEMA | `(?i)\bDROP\s+(?:DATABASE\|TABLE\|SCHEMA\|INDEX\|VIEW\|TABLESPACE\|USER\|ROLE)\b` | ❌ 禁用 |
| 12 | SQL TRUNCATE | `(?i)\bTRUNCATE\s+(?:TABLE\s+)?\w` | ❌ 禁用 |
| 13 | MongoDB drop / dropDatabase | `(?i)\.(?:dropDatabase\|dropCollection\|drop)\s*\(` | ❌ 禁用 |
| 14 | Redis FLUSHALL / FLUSHDB | `(?i)\b(?:FLUSHALL\|FLUSHDB)\b` | ❌ 禁用 |

> P90 会拦住**正常的渗透验证动作**（测注入常需 `DROP TABLE`、测破坏面常需 `TRUNCATE`）。
> 启用前请确认测试场景，或依赖本次新增的 `override` 授权通道逐次放行。

## P80 —— HTTP 删除 / 批量清空（6 条，仅第 20 条启用）

| # | 规则名 | 匹配 | 启用 |
|---|---|---|---|
| 15 | curl / wget 发送 DELETE 请求 | `(?i)\bcurl\b[^|\n&;"]{0,300}(?:-X\s*DELETE\|--request\s+DELETE\|-XDELETE)\|...` | ❌ 禁用 |
| 16 | Python HTTP 客户端 DELETE | `(?i)\b(?:requests\|httpx\|aiohttp\|urllib\.request)\.delete\s*\(\|...` | ❌ 禁用 |
| 17 | 脚本中声明 DELETE 方法 | `(?i)axios\.delete\s*\(\|method\s*[:=]\s*['"]DELETE['"]` | ❌ 禁用 |
| 18 | 批量清空 / 清除接口路径 | `(?i)/(?:clear\|wipe\|flush\|purge\|truncate\|drop\|destroy\|factory[-_]reset\|reset[-_]all)(?:[/?#"'\s]\|$)` | ❌ 禁用 |
| 19 | 数据外泄管道 | `(?i)(curl\|wget\|nc\|ncat)\b[^|]*\b(\|\s*(curl\|wget\|nc))` | ❌ 禁用 |
| **20** | **删除类接口路径** | `(?i)/(?:(?:delete\|remove\|unlink\|erase\|destroy)[-\w]*\|del)(?:[/?#"'\s]\|$)` | ✅ **启用** |

> 第 20 条按**路径**匹配、不分 HTTP 方法。官方说明是刻意为之（多数应用的删除接口用
> GET/POST 就能触发）。但它会误伤任何 URL 含 `/delete` 的 GET，包括自建项目的接口名，
> 也会拦住「验证删除类漏洞」这类正常渗透动作。

## 建议的启用顺序

若决定启用，按风险从低到高：

1. **先开 P100 那 10 条**（1–9）。它们拦的是「打坏自己机器」——`rm -rf`、`dd of=/dev/sda`、
   fork 炸弹、关机、清防火墙。与渗透测试无关，误伤面接近零，但能保护 `data/` 目录
   （会话记录、流量证据、任务产物都在里面）。
2. **P90（11–14）与 P80（15–19）逐条评估**。这些会拦正常渗透动作，建议配合本次新增的
   `override` 授权通道：先开规则，再在任务里登记授权条目逐次放行。
3. **第 20 条保持现状**。它是唯一已启用的，且与本次 `honor_override` 机制最相关 ——
   如果将来给它开豁免，等于给「删目标数据」开一个自由文本的绕过口，需要慎重。

## 与 honor_override 的关系

本次补丁给 `intercept_rules` 新增了 `honor_override` 列（默认 `false`）。

- 关闭 → 该规则是**绝对**的，命中即拦截，不接受任何豁免。
- 打开 → 命中时先把该任务的 `operator_authorizations` 交给 LLM 裁判复核，
  确认授权确实覆盖本次动作才放行；不覆盖仍按规则拦截。

授权条目只能由人写入（`origin='human'`），模型无法给自己发授权。