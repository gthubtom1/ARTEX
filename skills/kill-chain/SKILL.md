---

## name: kill-chain
description: 给自主 agent 的战役纪律：8 阶段状态机（不许跳级）、能力图推理、shell 后的固定动作、横移前置、凭据复用上限、CTF flag-first 时间纪律。当任务是"拿下多台靶机/打穿一个网段/竞赛靶场"而不是单点验漏洞时使用。

# 战役纪律（Kill Chain）

你的任务不是"找到漏洞"，是**用最少的动作推进到目标**（flag / 数据库 / 管理员）。
每做一件事之前先问：**我现在处于哪个阶段？这一步能不能解锁下一阶段？**

## 一、八个阶段（不可跳级）

```
recon → access → foothold → privesc → pivot → lateral → objective → persist(可选)
```

| 阶段 | 进入条件 | 完成判据（必须有证据） |
|---|---|---|
| recon | 无 | 有端口/服务/指纹/入口清单 |
| access | 有入口 | 确认一个可利用缺陷（有请求响应） |
| foothold | 有可利用缺陷 | **能稳定执行命令并拿到输出** |
| privesc | 有 foothold | 权限提升（或已确认当前权限够用） |
| pivot | 有 foothold | 经代理/隧道能触达新网段 |
| lateral | 有 pivot | 在新主机上执行命令并拿到输出 |
| objective | 有 foothold 以上 | 拿到目标物（flag / 数据 / 管理员） |

**硬规则**：**没有 foothold 就不许派"横移/内网扫描"类动作**；没有 pivot 就不许说"打进内网"。
违反这条 = 空转，浪费的每一分钟都是名次。

## 二、能力图：把"发现"翻译成"我现在能做什么"

每次确认一个发现，立刻问：**它给了我什么能力？**

| 发现 | 能力 |
|---|---|
| RCE / 命令执行 / 上传可解析 | `shell` |
| 任意文件读取 | `file_read`（下一步：读配置拿凭据） |
| SQL 注入可读 | `db`（下一步：读用户表拿凭据） |
| 弱口令 / 默认口令 | `creds` |
| 进得去后台 | `admin` |
| SSRF 可出站 | `net_reach`（**只是半跳，不是 shell**） |

**能力才是推进的燃料**，不是漏洞数量。判断"下一步"时看能力，不看 finding 数。

## 三、拿到 shell 后的前 5 分钟（固定动作，别自由发挥）

```bash
id; sudo -l; uname -a
find / -perm -4000 -type f 2>/dev/null | head -30; getcap -r / 2>/dev/null | head
cat /etc/crontab; ls -la /etc/cron.* 2>/dev/null
ls -la ~; cat ~/.bash_history 2>/dev/null | tail -50
find / -name "*.conf" -o -name "*.env" -o -name "id_rsa" 2>/dev/null | grep -v proc | head -30
ip a; ip route; arp -a; cat /etc/hosts; ss -tlnp
```

**做完这 6 条再决定下一步**。顺序就是优先级：
提权可能 → 凭据 → 内网位置。**不要拿到 shell 后继续在 web 上找洞。**

## 四、横移（前置检查先做）

必须同时满足：① 稳定立足点 ② 知道所在网段 ③ 能起代理（chisel/ligolo/ssh -D）。

```
建代理 → 走代理扫（nmap -sT，-sS 走不了 socks）→ 用已有凭据打新主机 → 新立足点再建代理
```

- **一条凭据最多试 3 个目标**，之后入池交给下一轮（喷第 4 个会触发锁定，直接判失败）
- **一次只加一跳**，不要一次铺开 /16
- 用 `curl 通了一个页面`≠ 横移成功；**新主机上能执行命令**才算

## 五、CTF / 竞赛时间纪律

1. **flag-first**：拿到 shell 先找 flag（`/root`、`/home/*`、环境变量、数据库、备份），不是先提权。
2. **3 跳原则**：一条思路 3 跳内没有实证 → 降级，换方向。
3. **每 10 分钟 checkpoint**：有 shell 吗？有新凭据吗？有新网段吗？三问全否 → 强制换机器。
4. **单机 40 分钟上限**：超时冻结，全局进度 > 单点完美。
5. **保留通道**：任何时刻至少一个可执行命令的立足点，丢了先回来重建。

## 六、每次写回时问三句

- 这条写回的是**能力**还是**噪音**？（写能力：shell/creds/db/admin/net_reach）
- 它**解锁了哪个阶段**？（没有就别写"重要发现"）
- 证据能不能**让别人重放**？（不能就标 inferred）

## 七、禁止

- 打范围外 IP、D 服务、上传破坏性 payload、真实持久化/勒索手法 —— 判违规直接零分。
- 把"版本匹配 CVE""参数看起来可注"当已确认漏洞。
- 平台拦截（"ARTEX 平台管控"字样）当成目标防护去绕过 —— 那是策略，换路线。
