package env

import (
	"runtime"
	"strings"
	"testing"
)

// TestInstallHint 锁定「缺工具 → 可执行命令」的确定性映射。
// 背景：任务报 tool_missing，错误卡只说"到环境页安装"，页面又不给命令，
// 用户只能自己猜 apt 包名。这里保证常用工具都能给出正确包名。
func TestInstallHint(t *testing.T) {
	cases := map[string]string{
		"conntrack":   "conntrack",
		"chisel":      "chisel",
		"sshpass":     "sshpass",
		"proxychains": "proxychains4", // 命令名带 4，包名不带
		"ldapsearch":  "ldap-utils",   // 命令名与包名不同
		"netexec":     "netexec",
		"nxc":         "netexec", // 别名指向同一个包
		"go":          "golang-go",
		"DIG":         "dnsutils", // 大小写不敏感
	}
	for name, wantPkg := range cases {
		pkg, cmd := InstallHint(name)
		if pkg != wantPkg {
			t.Fatalf("%s: pkg=%q want %q", name, pkg, wantPkg)
		}
		if runtime.GOOS == "linux" && !strings.HasPrefix(cmd, "sudo apt install -y "+wantPkg) {
			t.Fatalf("%s: 命令不对: %q", name, cmd)
		}
	}

	// 不认识的名字：不许瞎给包名（装错包比装不上更糟），但要给"怎么自己找"。
	pkg, cmd := InstallHint("some-unknown-tool")
	if pkg != "" {
		t.Fatalf("未知工具不该给出包名，实际 %q", pkg)
	}
	if runtime.GOOS == "linux" && !strings.Contains(cmd, "apt search") {
		t.Fatalf("未知工具应给搜索建议，实际 %q", cmd)
	}
	if _, cmd := InstallHint("  "); cmd != "" {
		t.Fatalf("空名字应返回空命令")
	}
}

// TestInstallHintBatch 锁定批量命令：去重、排序、未收录的单独列在注释里，
// 不能把不存在的包名塞进同一条 apt install（那会让整条命令失败）。
func TestInstallHintBatch(t *testing.T) {
	got := InstallHintBatch([]string{"conntrack", "chisel", "sshpass", "conntrack", "unknown-x"})
	if got == "" {
		t.Fatal("不应为空")
	}
	if runtime.GOOS == "linux" {
		if !strings.HasPrefix(got, "sudo apt install -y chisel conntrack sshpass") {
			t.Fatalf("批量命令不对（应按包名排序、去重）: %q", got)
		}
		if strings.Contains(got, "unknown-x\n") {
			t.Fatalf("未收录工具不该混进 apt 命令主体: %q", got)
		}
		if !strings.Contains(got, "#") || !strings.Contains(got, "unknown-x") {
			t.Fatalf("未收录工具应在注释里提示: %q", got)
		}
	}
	if got := InstallHintBatch(nil); got != "" {
		t.Fatalf("空输入应为空，实际 %q", got)
	}
}

// TestDescribeMissing 锁定前端要的数据形状（每个缺的工具带包名与命令）。
func TestDescribeMissing(t *testing.T) {
	out := DescribeMissing([]string{"conntrack", "unknown-y"})
	if len(out) != 2 {
		t.Fatalf("want 2 got %d", len(out))
	}
	// Windows 上不假装知道包管理器：Install 为空是预期，只要包名正确。
	wantInstall := runtime.GOOS == "linux" || runtime.GOOS == "darwin"
	if out[0].Name != "conntrack" || out[0].Package != "conntrack" || (wantInstall && out[0].Install == "") {
		t.Fatalf("known 工具字段不全: %+v", out[0])
	}
	if out[1].Name != "unknown-y" || out[1].Package != "" {
		t.Fatalf("unknown 工具不该给包名: %+v", out[1])
	}
}
