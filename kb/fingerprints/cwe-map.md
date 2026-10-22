---
id: fingerprints/cwe-map
type: fingerprint
tags: [cwe, 缺陷分类, 根因, 报告]
source: cwe
license: public
version: 1
updated: 2026-10-22
---

# CWE 常见缺陷 → 对应手法与检查点

> 用途：报告「根因分析」直接引用 CWE 编号（显专业）；`kb_surface` 用它给缺陷分类。
> **注意**：本表只保证 CWE 编号与含义的对应关系，不声称排名；排名请现查 [cwe.mitre.org/top25](https://cwe.mitre.org/top25/)。

| CWE | 名称 | 本方手法 | 第一检查点 |
|---|---|---|---|
| CWE-79 | 跨站脚本（XSS） | `tactics/` 注入类 | 输出是否按上下文编码；存储型要二次访问 |
| CWE-89 | SQL 注入 | `tactics/sqli` | 参数是否拼接进 SQL；布尔/时间差异 |
| CWE-352 | CSRF | 认证类 | 状态变更接口是否有 token/SameSite |
| CWE-862 | 缺失授权 | `tactics/access-control` | 无 token 请求是否仍返回数据 |
| CWE-863 | 授权不正确 | `tactics/access-control` | 换身份后是否只校验"登录"不校验"归属" |
| CWE-639 | 通过用户可控键的授权绕过（IDOR） | `tactics/access-control` | 换 id 是否返回他人数据 |
| CWE-22 | 路径穿越 | `tactics/upload-to-rce` | `../` 是否被规范化；编码变体 |
| CWE-434 | 危险类型文件上传 | `tactics/upload-to-rce` | 扩展名/Content-Type/解析点三件套 |
| CWE-502 | 不可信数据反序列化 | 注入类 | 是否有 `readObject`/`unserialize`/`@type` |
| CWE-918 | SSRF | 注入类 | URL 参数是否限制出站；OAST 回连 |
| CWE-94 | 代码注入 | 注入类 | 模板/表达式是否可控 |
| CWE-78 | OS 命令注入 | 注入类 | 拼接 shell；`;` `\|` `$()` 回显或延时 |
| CWE-611 | XXE | 注入类 | XML 解析是否禁外部实体 |
| CWE-287 | 认证不当 | 认证类 | JWT `alg=none`、会话固定、弱口令策略 |
| CWE-798 | 硬编码凭据 | 侦察类 | JS/配置/镜像里搜 key |
| CWE-200 | 敏感信息泄露 | 侦察类 | 报错堆栈、备份文件、`/.git/` |
| CWE-306 | 关键功能缺失认证 | 认证类 | `/actuator`、`/admin` 直连 |
| CWE-601 | 开放重定向 | 注入类 | `?next=` `?url=` 是否白名单 |
| CWE-1333 | 正则 ReDoS | 业务逻辑 | 超长输入是否拖死服务（**谨慎，可能致业务中断**） |
| CWE-362 | 竞态条件 | 业务逻辑 | 并发请求同一资源（优惠券/余额/库存） |
| CWE-840 | 业务逻辑错误 | 业务逻辑 | 负数/0/超大数/步骤跳过 |
| CWE-532 | 日志泄露敏感信息 | 侦察类 | 日志接口/调试页 |
| CWE-269 | 权限管理不当 | 提权类 | 服务账户权限过大 |
| CWE-250 | 不必要的权限执行 | 提权类 | SUID/capability/sudo 配置 |
| CWE-732 | 关键资源权限分配错误 | 提权类 | 可写配置/可写服务二进制 |

## 纪律

- CWE 是**分类**，不是证据。写进报告时必须配复现步骤与请求/响应原文。
- 同一 finding 只引用**一个主 CWE**（最多加一个次 CWE），堆编号反而不专业。
