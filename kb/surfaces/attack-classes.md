---
id: surfaces/attack-classes
type: surface
tags: [attack-class, taxonomy, payloadsallthethings, 攻击类]
source: pat
license: MIT
version: 1
updated: 2026-10-22
---

# 攻击类 → 手法映射（来自 PayloadsAllTheThings 顶层目录，67 项实测）

> 用途：`kb_surface` 与 planner 用「这个接口/参数像什么形状」反查该查哪些类。
> 生成方式：`scripts/kb_bootstrap.py` 从 PAT 顶层目录自动生成，新增类会出现在「未归类」里。

## recon（4）
- Hidden Parameters
- Insecure Source Code Management
- Methodology and Resources
- Virtual Hosts

## auth（8）
- API Key Leaks
- Account Takeover
- Brute Force Rate Limit
- Insecure Randomness
- JSON Web Token
- OAuth Misconfiguration
- SAML Injection
- Type Juggling

## inject（16）
- CRLF Injection
- CSS Injection
- CSV Injection
- Command Injection
- GraphQL Injection
- LDAP Injection
- LaTeX Injection
- NoSQL Injection
- ORM Leak
- Regular Expression
- SQL Injection
- Server Side Include Injection
- Server Side Template Injection
- XPATH Injection
- XSLT Injection
- XXE Injection

## upload（5）
- Client Side Path Traversal
- Directory Traversal
- File Inclusion
- Upload Insecure Files
- Zip Slip

## ssrf（3）
- DNS Rebinding
- Open Redirect
- Server Side Request Forgery

## deser（5）
- DOM Clobbering
- Google Web Toolkit
- Insecure Deserialization
- Java RMI
- Prototype Pollution

## webmis（17）
- Business Logic Errors
- CORS Misconfiguration
- Clickjacking
- Dependency Confusion
- Encoding Transformations
- External Variable Modification
- HTTP Parameter Pollution
- Insecure Management Interface
- Mass Assignment
- Prompt Injection
- Race Condition
- Request Smuggling
- Reverse Proxy Misconfigurations
- Tabnabbing
- Web Cache Deception
- Web Sockets
- XS-Leak

## access（1）
- Insecure Direct Object References

## rce（3）
- CVE Exploits
- Denial of Service
- Headless Browser

## 未归类（2）

- Cross-Site Request Forgery
- XSS Injection
