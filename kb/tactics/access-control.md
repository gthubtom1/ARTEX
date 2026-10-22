---
id: tactics/access-control
type: tactic
tags: [auth, idor, 越权, 未授权, jwt, 逻辑漏洞]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 越权 / 未授权 / 认证绕过：判定标准与检查顺序

**一句话结论**：越权的证据永远是**两份响应体的差异**（A 身份 vs B 身份 / 有 token vs 无 token），不是状态码。

## 判定标准

**算命中**（任一）：
- 水平越权：同一接口用 A 的凭据请求 B 的资源，**返回了 B 的数据**（字段值可对照）
- 垂直越权：普通用户凭据请求管理接口，返回 200 **且含管理数据/可用操作**
- 未授权：去掉 `Authorization`/`Cookie` 后仍返回**业务数据**（不是空数组/登录页）
- 认证绕过：改 JWT `alg=none` / 弱密钥签名 / 篡改 `role` 后仍被接受且行为变化
- 逻辑绕过：把 `price`、`quantity`、`userId`、`step` 改成非法值后**业务结果被改变**（订单价变 0、跳过验证）

**不算命中**：
- 只换了状态码（403→200 但返回空对象 / 登录页 HTML）
- 接口返回"参数错误"（说明校验在工作）
- 仅"响应长度不同"而无业务语义差异（可能是时间戳/随机 nonce）

## 检查顺序

1. **枚举身份**：先拿到两个真实可用的身份（注册两个账号 / 一个管理员一个普通 / 一个 token 一个匿名）。**没有第二个身份就没有越权测试**。
2. **无 token 直接请求**：把同一 URL 去掉认证头，与带 token 的响应逐字段对比。
3. **水平越权**：A 的 token + B 的资源 id。id 要取自 B 真实存在的数据（先从 B 的身份列表接口拿）。
4. **垂直越权**：普通 token 请求 `/admin/*`、`/api/v1/manage/*`；看响应体而不是状态码。
5. **参数替换**：`userId`/`uid`/`accountId`/`tenantId` 出现在 body、query、header、JWT payload 里的都要试。
6. **JWT 专项**：`alg=none`、`HS256` 弱密钥（字典跑）、`kid` 注入、`jku` 劫持、`exp` 不校验。
7. **逻辑绕过**：负数、0、超大数、数组、`null`、重复字段、大小写、Unicode 同形字。

## 最小验证

```bash
# 1) 无 token 是否还能拿到数据
curl -s "http://TARGET/api/user/1002" -H "Authorization: Bearer <A_TOKEN>" | head -c 400
curl -s "http://TARGET/api/user/1002" | head -c 400          # 期待 401/403
# 2) 水平越权：A 的 token 打 B 的 id
curl -s "http://TARGET/api/user/<B_ID>" -H "Authorization: Bearer <A_TOKEN>"
# 3) JWT alg=none
python3 - <<'PY'
import base64,json
h=base64.urlsafe_b64encode(b'{"alg":"none","typ":"JWT"}').rstrip(b'=')
p=base64.urlsafe_b64encode(b'{"sub":"admin","role":"admin"}').rstrip(b'=')
print(h.decode()+'.'+p.decode()+'.')
PY
```

## 失败即停

- 403 且响应体是统一的拒绝结构 → 该接口认证有效，换接口，**不要反复改 header**。
- 同一 `资源 id` 参数试过 3 个不同 id 都 403 → 判定有归属校验，记 fact。
- 一个业务模块连续 2 轮无增量 → 交顾问换模块。

## 关联

- 接口形状清单：`surfaces/api-shape-checklist.md`
- 指纹：`fingerprints/tech-vuln-map.md`
