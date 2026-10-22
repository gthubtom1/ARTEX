# 知识库索引（自动生成，勿手改）

- 生成时间：2026-10-22 05:10 中国标准时间
- 条目：13 条（有效 13 / 重复 0）
- 字符量：30,777

> 重建：`python kb/scripts/kb_index.py`　检索：`python kb/scripts/kb_search.py "关键词"`

## capability（1）

| id | 标签 | 摘要 |
|---|---|---|
| `capabilities/taxonomy` | capability, taxonomy, 能力清单, 后渗透 | ```yaml |

## doc（1）

| id | 标签 | 摘要 |
|---|---|---|
| `prompts/kb-update` | meta, 维护, 提示词, 知识库 | 你是一个渗透知识库的**维护员**。目标不是"多写点内容"，而是让这个库**每补一次，判断命中率就高一点，且永远不出现重复与失效条目**。 |

## fingerprint（2）

| id | 标签 | 摘要 |
|---|---|---|
| `fingerprints/cwe-map` | cwe, 缺陷分类, 根因, 报告 | - CWE 是**分类**，不是证据。写进报告时必须配复现步骤与请求/响应原文。 |
| `fingerprints/tech-vuln-map` | fingerprint, tech, cve, default-login | **用法**：写下含 `tech/version` 的 fact 后立刻查本表；命中族名后**必须实际触发**才算漏洞，只匹配版本只算线索（ |

## payload（1）

| id | 标签 | 摘要 |
|---|---|---|
| `payloads/minimal-web` | payload, sqli, ssti, ssrf | ```sql |

## surface（2）

| id | 标签 | 摘要 |
|---|---|---|
| `surfaces/api-shape-checklist` | surface, api, idor, 形状 | **用法**：worker 拿到一个 endpoint 时先按形状查这张表，**最多挑 3 条去验证**，不要全测。 |
| `surfaces/attack-classes` | attack-class, taxonomy, payloadsallthethings, 攻击类 | - Hidden Parameters |

## tactic（6）

| id | 标签 | 摘要 |
|---|---|---|
| `tactics/access-control` | auth, idor, 越权, 未授权 | **一句话结论**：越权的证据永远是**两份响应体的差异**（A 身份 vs B 身份 / 有 token vs 无 token），不是状态 |
| `tactics/data-access-proof` | data, database, dump, 数据库 | **一句话结论**：证明数据库可达，只需要三样东西——**表名、行数量级、一列敏感字段的样本证明**。**永远不要 dump 全量**：那是 |
| `tactics/lateral-pivot` | lateral, pivot, 横移, 跳板 | **一句话结论**：横移不是"再打一个 web"，是**用已有立足点当路由器**，把不可达变成可达；先建通道，再扫，再复用凭据。 |
| `tactics/post-exploit-foothold` | post-exploit, shell, privesc, foothold | **一句话结论**：拿到立足点后不要继续打 web，**立刻把"这台机器能给我什么"榨干**——这是从"打点"到"拿全"的分水岭。 |
| `tactics/sqli` | inject, sqli, mysql, 布尔盲注 | **一句话结论**：先用**两次可对照的请求**证明参数改变了 SQL 语义（布尔差异或时间差异），再谈利用；只有报错文案不算注入。 |
| `tactics/upload-to-rce` | upload, rce, 文件上传, 解析 | **一句话结论**：上传成功不算漏洞，**服务端能解析并执行你上传的内容**才算；判定要靠"访问后返回了只有执行才会产生的东西"。 |
