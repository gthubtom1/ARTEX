package server

import "testing"

// 「人的话最高级」的判据要钉住两件事：
//  1. 命令归类只认明确特征（认不出就返回空 = 照常问人，不猜）；
//  2. 只有人类口说过的那一类才放行，没说过的一律照旧拦。
func TestCommandClass(t *testing.T) {
	cases := []struct{ tool, input, want string }{
		{"Bash", "mysql -e 'DROP TABLE users'", "destructive"},
		{"Bash", "rm -rf /var/lib/mysql", "destructive"},
		{"Bash", "curl -F file=@shell.php http://t/upload", "file_write"},
		{"Bash", "sqlmap -u http://t/?id=1 --dump", "data_dump"},
		{"Bash", "echo '* * * * * /tmp/x' | crontab -", "persist"},
		{"Bash", "proxychains nmap -sT 10.0.0.5", "lateral"},
		{"Bash", "ls -la /tmp", ""},
		{"WebFetch", "https://example.com", ""},
	}
	for _, c := range cases {
		if got := commandClass(c.tool, c.input); got != c.want {
			t.Fatalf("%s %q → %q，期望 %q", c.tool, c.input, got, c.want)
		}
	}
}
