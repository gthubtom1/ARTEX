package reviewer

import (
	"strings"
	"testing"
)

func TestReviewDeniesOutOfScopeTarget(t *testing.T) {
	res := Review(Request{
		Tool:            "Bash",
		Target:          "evil.example.net",
		Command:         "nmap -sT evil.example.net",
		TaskConstraints: []string{"只允许测试 example.com"},
	})
	if res.Decision != Deny {
		t.Fatalf("decision=%s reason=%s, want deny", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "不在任务约束允许范围内") {
		t.Fatalf("unexpected reason: %s", res.Reason)
	}
}

func TestReviewDeniesConstraintTarget(t *testing.T) {
	res := Review(Request{
		Tool:            "Bash",
		Target:          "admin.example.com",
		Command:         "curl -s https://admin.example.com",
		TaskConstraints: []string{"禁止访问 admin.example.com"},
	})
	if res.Decision != Deny || !strings.Contains(res.Reason, "deny") {
		t.Fatalf("decision=%s reason=%s, want constraint deny", res.Decision, res.Reason)
	}
}

func TestReviewDeniesDestructiveCommands(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"sudo rm -rf / --no-preserve-root", "rm -rf /"},
		{"mkfs.ext4 /dev/sda1", "mkfs"},
		{"dd if=/dev/zero of=/dev/sda bs=1M", "dd of=/dev/"},
		{"shutdown -h now", "shutdown"},
		{`psql -c "DROP DATABASE prod"`, "DROP DATABASE"},
		{"mysqldump --all-databases > dump.sql", "mysqldump --all-databases"},
		{"sqlmap -u https://x/y --dump", "sqlmap --dump"},
		{`mysql -e "DELETE FROM users"`, "DELETE FROM 无 WHERE"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			res := Review(Request{Tool: "Bash", Command: tc.command})
			if res.Decision != Deny {
				t.Fatalf("decision=%s reason=%s, want deny", res.Decision, res.Reason)
			}
			if !strings.Contains(res.Reason, tc.want) {
				t.Fatalf("reason=%q does not mention %q", res.Reason, tc.want)
			}
			if res.Confidence != "high" {
				t.Fatalf("confidence=%s, want high", res.Confidence)
			}
		})
	}
}

func TestReviewAllowsReadOnlyCommands(t *testing.T) {
	cases := []string{
		"nmap -sT 10.10.10.10",
		"nmap -sV -p 80,443 example.com",
		"curl -s https://example.com/",
		"nc -z example.com 443",
		"dig example.com",
		"whois example.com",
		"ls -la /tmp",
		"cat /etc/hosts",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			res := Review(Request{Tool: "Bash", Command: cmd})
			if res.Decision != Allow {
				t.Fatalf("decision=%s reason=%s, want allow", res.Decision, res.Reason)
			}
		})
	}
}

func TestReviewAsksForWriteAndExploitActions(t *testing.T) {
	cases := []string{
		"curl -X POST https://example.com/upload -d @shell.php",
		"curl -s -o out.txt https://example.com/",
		"hydra -l admin -P rockyou.txt ssh://10.10.10.10",
		"python3 exploit.py --target 10.10.10.10",
		"nmap -sV --script http-shellshock 10.10.10.10",
		"echo '<?php system($_GET[c]); ?>' > shell.php",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			res := Review(Request{Tool: "Bash", Command: cmd})
			if res.Decision != Ask {
				t.Fatalf("decision=%s reason=%s, want ask", res.Decision, res.Reason)
			}
			if !Uncertain(res) {
				t.Fatal("Uncertain should be true for ask")
			}
		})
	}
}

func TestReviewEmptyConstraintsDoesNotImplyOutOfScope(t *testing.T) {
	res := Review(Request{Tool: "Bash", Target: "anything.example.net", Command: "id"})
	if res.Decision == Deny {
		t.Fatalf("empty constraints must not be treated as out of scope: %+v", res)
	}
}

func TestReviewDenyWinsOverReadOnly(t *testing.T) {
	res := Review(Request{
		Tool:            "Bash",
		Target:          "example.com",
		Command:         "cat /etc/hosts", // read-only, but target is forbidden
		TaskConstraints: []string{"禁止访问 example.com"},
	})
	if res.Decision != Deny {
		t.Fatalf("decision=%s, want deny", res.Decision)
	}
}

func TestUncertainOnlyForAsk(t *testing.T) {
	if Uncertain(Result{Decision: Allow}) || Uncertain(Result{Decision: Deny}) {
		t.Fatal("only ask is uncertain")
	}
	if !Uncertain(Result{Decision: Ask}) {
		t.Fatal("ask must be uncertain")
	}
}
