---
id: tactics/data-access-proof
type: tactic
tags: [data, database, dump, 数据库, 用户信息, 手机号, 证据]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 数据库可达性证明：怎么"看到有多少数据"而不导出

**一句话结论**：证明数据库可达，只需要三样东西——**表名、行数量级、一列敏感字段的样本证明**。**永远不要 dump 全量**：那是越界，也是被溯源的第一证据。

## 判定分级（能力卡的 proof_level）

| 级别 | 做到什么 | 允许的操作 |
|---|---|---|
| **D1 结构可见** | 知道有哪些表、哪些列 | `information_schema.tables/columns`、`SHOW TABLES` |
| **D2 规模可知** | 知道每张表多少行 | `SELECT COUNT(*) FROM t`（**只 count，不 select 数据**） |
| **D3 敏感字段确认** | 确认存在手机号/邮箱/身份证/地址等列 | `SELECT col FROM t LIMIT 1`（**最多 1 行，脱敏显示**） |
| **D4 可导出** | 能批量取（有导出接口/权限） | **只记录"具备该能力"，不执行导出** |

**报告里能写"数据库有 12.4 万条用户记录，含手机号与收货地址"——这就是 D3。**
写到 D3 就够交赏金报告了；D4 只做定性判断。

## 检查顺序

```sql
-- D1 结构
SELECT table_schema, table_name, table_rows FROM information_schema.tables
WHERE table_schema NOT IN ('mysql','information_schema','performance_schema');
SELECT column_name, data_type FROM information_schema.columns WHERE table_name='users';

-- D2 规模（只 count）
SELECT COUNT(*) FROM users;
SELECT COUNT(*) FROM orders;

-- D3 敏感字段（抽样 1 行，输出时打码）
SELECT id, LEFT(phone,3), LEFT(email,2) FROM users LIMIT 1;
```

非 SQL 路径（同样是 D1–D3）：

```bash
# 泄露的备份/导出文件
curl -sI "http://TARGET/backup/users.sql" | head -5     # 只看头，不下载
curl -s "http://TARGET/backup/users.sql" -r 0-2048      # 只取 2KB 看结构

# 接口分页暴露
curl -s "http://TARGET/api/users?page=1&size=1"         # 只取 1 条
curl -s "http://TARGET/api/users?page=1&size=1" -o /dev/null -w '%{size_download}\n'
# 通过 total 字段判断规模（很多接口直接返回 total）
```

## 判定标准

**算命中**：能读到表名 + 行数 + 至少一个敏感列名（D2/D3）。
**不算命中**：
- 只有 `information_schema` 可见但没有业务库（空壳）
- 只有接口返回 `[]` 或 `total:0`
- 只有"疑似数据库端口开放"（3306 开着不等于能进）

## 边界（硬规则）

1. **不 dump 全量**：`mysqldump` / `sqlmap --dump` / 全表 `SELECT *` 在侦察阶段一律禁止。
2. **样本 ≤ 1 行**，输出时必须打码（`138****1234`），原文不进日志、不进报告、不进知识库。
3. **不碰用户隐私全文**：只证明"这一列存在且是手机号格式"，不抄内容。
4. **不写库**：`UPDATE/DELETE/INSERT` 在任何阶段都禁止（除非任务明确要求且有书面批准）。
5. 一旦涉及真实个人信息，**立即停止深入**，只记录"可达 + 规模"，转报告。

## 能力卡输出模板

```
【能看数据】用户库可达，规模 12.4 万条
  表：users(124,301) / orders(88,102) / addresses(31,450)
  敏感列：phone / email / address / id_card（已确认存在）
  证据：information_schema 查询结果 + SELECT COUNT(*) 返回 124301
  能做到：可读取全部用户手机号与收货地址（D3 已验证，D4 未执行）
  风险：读 1 条也会留访问日志；批量导出会触发风控并违规
```

## 关联

- 能力卡设计：`docs/00` 附录 C + `使用教程.md` §6.1（D1–D4 分级）
- 注入类入口：`tactics/sqli.md`
- 越权读取：`tactics/access-control.md`
