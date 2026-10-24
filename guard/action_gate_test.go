package guard

import "testing"

// TestClassifyAction 锁定"哪些命令属于需要用户授权的越界动作"。
// 判据（docs/20 §1 失败显形）：未授权时这类命令必须被拦，并在 task_error_events
// 写 class=policy_blocked + tool=GrantGate；侦察类命令一律不拦。
func TestClassifyAction(t *testing.T) {
	cases := map[string]string{
		// file_write：向目标写文件 / 传 webshell
		`curl -s -F "file=@shell.php" http://t/upload.php`: "file_write",
		`curl -X PUT --data-binary @x.jsp http://t/x.jsp`:   "file_write",
		`echo '<?php system($_GET[c]); ?>' > webshell.php`:  "file_write",
		// data_dump：整库/整表导出
		`sqlmap -u http://t/a?id=1 --batch --dump`: "data_dump",
		`mysqldump -h db -u root -p x > all.sql`:   "data_dump",
		// persist：留账号/计划任务
		`useradd -m backdoor && echo 'p' | chpasswd`: "persist",
		`echo "* * * * * curl x" | crontab -`:        "persist",
		`echo "ssh-rsa AAA" >> ~/.ssh/authorized_keys`: "persist",
		// lateral：横移
		`sshpass -p p ssh root@10.0.0.5`:            "lateral",
		`proxychains4 nmap -sT 10.0.0.0/24`:          "lateral",
		`nxc smb 10.0.0.5 -u a -p b -x whoami`:       "lateral",
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
		`cat /etc/passwd`,
		`grep -n "form" index.html`,
	} {
		if got := classifyAction(cmd); got != "" {
			t.Fatalf("侦察命令不该要求授权: %q → %q", cmd, got)
		}
	}
}
