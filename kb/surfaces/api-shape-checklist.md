---
id: surfaces/api-shape-checklist
type: surface
tags: [surface, api, idor, 形状, 检查清单]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 接口形状 → 缺陷清单（看到什么形状，就知道查什么）

**用法**：worker 拿到一个 endpoint 时先按形状查这张表，**最多挑 3 条去验证**，不要全测。

| 形状 | 最可能的缺陷 | 第一条验证 |
|---|---|---|
| `GET /x/{id}`、`/x?id=` | IDOR / 水平越权 | 换另一个真实 id（不是 1/2 盲猜） |
| `GET /x?sort=`、`?order=`、`?fields=` | SQLi / 排序注入 | 加 `,1` 或 `(select 1)` 看报错 |
| `GET /x?url=`、`?file=`、`?path=`、`?next=` | SSRF / LFI / 开放重定向 | 换成本机/OAST 域名，看回连 |
| `POST /x` 带 `role`、`is_admin`、`status`、`balance` | 批量赋值 / 提权 | 添加一个不该有的字段，看响应是否回显 |
| `POST /x` 带 `price`、`amount`、`quantity` | 业务逻辑 / 竞态 | 传负数、0、超大数 |
| `PUT/PATCH /x/{id}` | 越权修改 / 批量赋值 | 换 id + 添加敏感字段 |
| `DELETE /x/{id}` | 越权删除 | **不要真删**，先看 403/200 差异 |
| 返回 302 到 `/login` 的接口 | 认证绕过 / 强制浏览 | 直接访问被保护页面 |
| 返回 JSON 但报错含堆栈/路径 | 信息泄露 / 源码泄露 | 看是否有 `.git`、备份文件 |
| `?format=`、`?template=`、`?view=` | SSTI / 路径穿越 | `{{7*7}}` / `../` |
| `?callback=`、`?jsonp=` | XSS / JSONP 劫持 | 注入 `alert(1)` |
| 上传接口 + 可访问目录 | 上传 RCE | 见 `tactics/upload-to-rce.md` |
| GraphQL `/graphql` | 内省泄露 / 越权查询 | introspection 查询 |
| WebSocket `ws://` | 鉴权缺失 / 消息注入 | 不带 token 连一次 |
| `/actuator`、`/debug`、`/metrics` | 未授权信息泄露 | GET 一次看 200 |
| 带 `tenant_id`/`org_id` 的接口 | 跨租户越权 | 换成另一个租户 id |
| 分页 `?page=&size=` | 数据量越权 / 遍历 | `size=10000` 看是否全量返回 |
| 搜索 `?q=` | SQLi / 模板注入 / 反射 XSS | 单引号 + `{{7*7}}` |
| 导出 `?export=csv/xlsx` | 公式注入 / SSRF | `=cmd\|' /'` |
| 第三方回调 `webhook` 配置 | SSRF + 内网探测 | 填 OAST 域名 |

## 顺序纪律

1. **先无认证请求一次**（最便宜的越权测试）。
2. **再用第二个身份打一次**（真越权的唯一证据）。
3. **然后才动 payload**（注入/模板/穿越）。
4. 一条接口最多试 **3 类**缺陷，之后换接口——同接口第 4 类开始，收益断崖式下降。

## 关联

- 攻击类总表：`surfaces/attack-classes.md`（62 类映射）
- 手法判定：`tactics/`
