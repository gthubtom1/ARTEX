package guard

import "testing"

// TestReconForbidden 锁定阶段门（阶段 B）的拦截清单：
// ① 只读侦察阶段必须挡住利用类命令，但不能误伤侦察类命令。
// 判据（docs/20 §3）：①模板任务的 task_error_events 里出现 class=policy_blocked + tool=StageGate，
// 且真实执行记录里不出现利用类命令。
func TestReconForbidden(t *testing.T) {
	blocked := []string{
		`sqlmap -u "http://x/a?id=1" --batch --dump`,
		`msfconsole -q -x "use exploit/windows/smb/ms17_010_eternalblue"`,
		`hydra -l admin -P pass.txt 1.2.3.4 http-post-form "/login:u=^USER^&p=^PASS^:F=err"`,
		`curl -s http://x/shell.php?cmd=id`,
		`nmap -p445 --script smb-vuln-ms17-010 1.2.3.4 && echo win`,
		`python3 -c "import requests" && medusa -h 1.2.3.4 -u admin -P p.txt`,
		`echo '<?php system($_GET[c]); ?>' > webshell.php`,
		`mysql -e "EXEC xp_cmdshell 'whoami'"`,
	}
	for _, c := range blocked {
		hit := false
		for _, re := range reconForbidden {
			if re.MatchString(c) {
				hit = true
				break
			}
		}
		if !hit {
			t.Fatalf("应拦截（漏报）: %s", c)
		}
	}

	allowed := []string{
		`nmap -Pn -sT -p- --min-rate 1000 -T4 --open -oN allports.txt 1.2.3.4`,
		`curl -s -o /dev/null -w "%{http_code}" -H "Host: demo.testfire.net" http://1.2.3.4/`,
		`nuclei -u http://1.2.3.4 -severity critical,high -o out.txt`,
		`ffuf -u http://1.2.3.4/FUZZ -w /usr/share/wordlists/dirb/common.txt`,
		`httpx -l urls.txt -title -tech-detect -o httpx.json`,
		`echo "=== 入口清单 ===" && grep -n "form" index.html`,
		`dig axcheat.com +short`,
	}
	for _, c := range allowed {
		for _, re := range reconForbidden {
			if re.MatchString(c) {
				t.Fatalf("不该拦截（误报）: %s", c)
			}
		}
	}
}
