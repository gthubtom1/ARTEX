package guard

import (
	"strings"
	"testing"
)

// TestClassifyAction 锁定"哪些命令属于需要用户授权的越界动作"。
// 判据（docs/20 §1 失败显形）：未授权时这类命令必须被拦，并在 task_error_events
// 写 class=policy_blocked + tool=GrantGate；侦察类命令一律不拦。
func TestClassifyAction(t *testing.T) {
	cases := map[string]string{
		// file_write：向目标写文件 / 传 webshell
		`curl -s -F "file=@shell.php" http://t/upload.php`: "file_write",
		`curl -X PUT --data-binary @x.jsp http://t/x.jsp`:  "file_write",
		`echo '<?php system($_GET[c]); ?>' > webshell.php`: "file_write",
		// data_dump：整库/整表导出
		`sqlmap -u http://t/a?id=1 --batch --dump`: "data_dump",
		`mysqldump -h db -u root -p x > all.sql`:   "data_dump",
		// persist：留账号/计划任务
		`useradd -m backdoor && echo 'p' | chpasswd`:   "persist",
		`echo "* * * * * curl x" | crontab -`:          "persist",
		`echo "ssh-rsa AAA" >> ~/.ssh/authorized_keys`: "persist",
		// data_write：改业务数据（改钱/改状态/开管理员号）—— 2026-10-25 外部审查 P0-03 补
		`mysql -h db -u root -p x -e "UPDATE users SET balance=1000 WHERE id=3"`: "data_write",
		`sqlmap -u http://t/a?id=1 --sql-query="INSERT INTO users VALUES(1,2)"`:  "data_write",
		`curl -s -X POST -d '{"role":"admin"}' http://t/api/user/create`:         "data_write",
		`psql -h db -c "create user backdoor with superuser"`:                    "data_write",
		// lateral：横移
		`sshpass -p p ssh root@10.0.0.5`:       "lateral",
		`impacket-psexec a/b@10.0.0.5`:         "lateral",
		`nxc smb 10.0.0.5 -u a -p b -x whoami`: "lateral",
	}
	for cmd, want := range cases {
		if got := classifyAction(cmd); got != want {
			t.Fatalf("classifyAction(%q)=%q want %q", cmd, got, want)
		}
	}

	// 侦察/只读命令一律不要求授权
	for _, cmd := range []string{
		`nmap -sV -p- 10.0.0.5`,
		`curl -s -o /dev/null -w "%{http_code}" http://t/admin/`,
		`ffuf -u http://t/FUZZ -w w.txt`,
		`sqlmap -u http://t/a?id=1 --batch --banner`,
		`mysql -h db -e "SELECT id,balance FROM users LIMIT 3"`,
		`curl -s -X POST -d '{"user":"a","pass":"b"}' http://t/doLogin`,
		`sqlmap -u http://t/a?id=1 --sql-query="select version()"`,
		// 回归实测：proxychains4 是出口包装器，包着侦察命令时必须放行
		`proxychains4 -q curl -s -o /dev/null -w "%{http_code}" https://t/`,
		`proxychains4 nmap -sT -p80,443 10.0.0.5`,
		`cat /etc/passwd`,
		`grep -n "form" index.html`,
	} {
		if got := classifyAction(cmd); got != "" {
			t.Fatalf("侦察命令不该要求授权: %q → %q", cmd, got)
		}
	}
}

// P0-04：拦截提示不许指向已经不存在的「允许」按钮（用户会找不到 → 任务静默卡死）。
func TestBlockMessageHasNoGhostButton(t *testing.T) {
	for _, cls := range []string{"file_write", "data_dump", "data_write", "persist", "lateral"} {
		msg := grantBlockMessage(cls)
		if strings.Contains(msg, "允许") || strings.Contains(msg, "拒绝") {
			t.Fatalf("%s：提示里还有按钮文案：%q", cls, msg)
		}
		if !strings.Contains(msg, "对话") {
			t.Fatalf("%s：提示必须告诉人在对话里说一句：%q", cls, msg)
		}
	}
}
