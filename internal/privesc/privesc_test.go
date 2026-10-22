package privesc

import (
	"strings"
	"testing"
)

const linpeasSample = `
[+] SUID - Check writable & exploitable with GTFOBins
-rwsr-xr-x 1 root root /usr/bin/find
-rwsr-xr-x 1 root root /usr/bin/vim.basic
[+] Sudo version
User www-data may run the following commands on box:
    (root) NOPASSWD: /usr/bin/tar
[+] Capabilities
/usr/bin/python3.9 = cap_setuid+ep
[+] Kernel
Linux box 5.4.0-42-generic
`

func TestFromLinPEAS(t *testing.T) {
	fs := FromLinPEAS(linpeasSample)
	if len(fs) < 4 {
		t.Fatalf("want >=4 findings, got %d: %+v", len(fs), fs)
	}
	kinds := map[FindingKind]int{}
	for _, f := range fs {
		kinds[f.Kind]++
	}
	if kinds[KindSUID] == 0 || kinds[KindSudo] == 0 || kinds[KindCap] == 0 || kinds[KindKernel] == 0 {
		t.Fatalf("missing kinds: %+v", kinds)
	}
}

func TestPlanOrdersLowRiskFirst(t *testing.T) {
	paths := Plan(FromLinPEAS(linpeasSample), "linux", 4)
	if len(paths) != 4 {
		t.Fatalf("want 4 paths, got %d", len(paths))
	}
	if paths[0].Risk == "high" {
		t.Fatalf("kernel exploit must not be first: %+v", paths[0])
	}
	if paths[len(paths)-1].Risk != "high" {
		t.Fatalf("kernel exploit should be last: %+v", paths)
	}
	for _, p := range paths {
		if strings.TrimSpace(p.Cmd) == "" {
			t.Fatalf("command empty for %q", p.Name)
		}
	}
}

func TestWindowsTokenPath(t *testing.T) {
	fs := FromWindows("whoami /priv -> SeImpersonatePrivilege Enabled\nUnquoted Service Path: C:\\Program Files\\x\\y.exe")
	if len(fs) < 2 {
		t.Fatalf("want 2 findings, got %+v", fs)
	}
	paths := Plan(fs, "windows", 3)
	if paths[0].Score < 70 {
		t.Fatalf("SeImpersonate should rank high: %+v", paths)
	}
}

func TestPlanFallback(t *testing.T) {
	paths := Plan(nil, "linux", 3)
	if len(paths) != 1 || !strings.Contains(paths[0].Cmd, "sudo") {
		t.Fatalf("fallback should give enumeration command: %+v", paths)
	}
}
