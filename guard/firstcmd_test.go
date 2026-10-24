package guard

import "testing"

// 2026-10-22 真靶场实测：Windows 上 agent 的 8 条命令被"缺工具"钩子误拦，
// 每条都白烧一轮 token。这些用例就是那 8 条的原样输入。
func TestFirstCommandNoFalsePositive(t *testing.T) {
	// 期望 ""（不做存在性检查）
	skip := []string{
		`cd /d/tmp/p8real/tasks/1/i2 2>/dev/null || cd "D:/tmp/p8real/tasks/1/i2"; pwd`,
		`pwd; ls -la; echo "--- DNS ---"`,
		`$ProgressPreference='SilentlyContinue'; try { $r = Invoke-WebRequest -Uri 'http://x/' } catch {}`,
		`Get-ChildItem | Select-Object Name,Length | Format-Table -AutoSize`,
		`sudo -u root ls -la`,
		`export FOO=1; echo ok`,
		`echo "hi"`,
		`try { throw "x" } catch { }`,
		// R3 实测仍会误拦的两类（P9 收尾时补）
		`1..3 | ForEach-Object { $_ }`,
		`1..$n | % { $_ }`,
		`import os, sys`,
		`from urllib.parse import urljoin`,
		`def main(): pass`,
		// 2026-10-24 VM 实测：这两个首词都是 shell 内建/探测器，
		// 原来会被当成"缺工具 command"写进 task_error_events。
		`command -v proxychains4 proxychains 2>/dev/null; echo "---"`,
		`command -v nmap`,
		`which sqlmap`,
		`type -a nc`,
	}
	for _, cmd := range skip {
		if got := firstCommand(cmd); got != "" {
			t.Errorf("firstCommand(%q)=%q，应为 \"\"（不该做存在性检查）", cmd, got)
		}
	}
}

func TestFirstCommandRealBinary(t *testing.T) {
	// 期望返回真实命令名（去掉路径/扩展名/包装器）
	cases := map[string]string{
		`curl.exe -sS -I -m 20 http://x/`:         "curl",
		`timeout 60 sqlmap -u http://x --batch`:   "sqlmap",
		`nmap -sV 1.2.3.4`:                        "nmap",
		`FOO=bar curl http://x`:                   "curl",
		`proxychains4 nmap -sT 10.0.0.1`:          "nmap",
		`/usr/bin/python3 pages.py`:               "python3",
		`powershell -NoProfile -Command "whoami"`: "powershell",
		`curl -s http://x && nmap 1.1.1.1`:        "curl",
		`  "gobuster" dir -u http://x -w w.txt`:   "gobuster",
	}
	for cmd, want := range cases {
		if got := firstCommand(cmd); got != want {
			t.Errorf("firstCommand(%q)=%q want %q", cmd, got, want)
		}
	}
}
