package verify

import "testing"

func TestLooksLikeTrace(t *testing.T) {
	cases := []struct {
		name string
		ev   string
		want bool
	}{
		{"http 请求行", "GET /admin?x=1 HTTP/1.1\nHost: t", true},
		{"http 状态行", "HTTP/1.1 200 OK\nServer: nginx", true},
		{"shell 提示符", "$ id\nuid=0(root) gid=0(root)", true},
		{"命令回显", "total 12\ndrwxr-xr-x 2 root root 4096", true},
		{"工具输出头", "nmap scan report for 10.0.0.1\n22/tcp open ssh", true},
		{"curl 命令行", "curl -s 'http://t/api?id=1' -H 'Cookie: x'", true},
		{"纯版本号断言", "ThinkPHP 5.0.23 存在 CVE-2018-20062，应该可以 RCE", false},
		{"纯描述", "这个站看起来是 Spring Boot，可能有 actuator 泄露", false},
		{"空串", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := LooksLikeTrace(c.ev)
			if ok != c.want {
				t.Fatalf("LooksLikeTrace(%q)=%v want %v", c.ev, ok, c.want)
			}
		})
	}
}

func TestCVEClaimWithoutTrace(t *testing.T) {
	if !CVEClaimWithoutTrace("ThinkPHP 5.0.23 存在 CVE-2018-20062") {
		t.Fatal("只有 CVE 断言应被识别")
	}
	if !CVEClaimWithoutTrace("version 5.0.23 has a known issue") {
		t.Fatal("只有版本号应被识别")
	}
	// 有触发记录就不算"只有断言"
	if CVEClaimWithoutTrace("GET /index.php HTTP/1.1\nCVE-2018-20062 已触发，回显 uid=33") {
		t.Fatal("带请求行的证据不应被判为只有断言")
	}
	if CVEClaimWithoutTrace("") {
		t.Fatal("空串不应判定")
	}
}

// 三条最典型的幻觉必须被判 rejected（不是 inferred）。
func TestCheckRejectsHallucinationPatterns(t *testing.T) {
	for _, ev := range []string{
		"ThinkPHP 5.0.23 存在 CVE-2018-20062",
		"该站 version 3.2.1 存在反序列化漏洞",
		"可能存在上传点，未验证是否解析 php",
		"",
	} {
		v := Check(Claim{Title: "x", Evidence: ev, ProofLevel: "P2"}, Options{})
		if v.Level != "rejected" {
			t.Errorf("evidence=%q 应判 rejected，实际 %s（%v）", ev, v.Level, v.Reasons)
		}
	}
}

// 真实证据必须能过 grounding。
func TestCheckAcceptsRealTraces(t *testing.T) {
	for _, ev := range []string{
		"GET /api/order/detail?id=1002 HTTP/1.1\nCookie: token=A\n\nHTTP/1.1 200 OK\n{phone:138****1234}",
		"$ id\nuid=33(www-data) gid=33(www-data) groups=33(www-data)",
		"nuclei -t cves/ -u http://t\n[log4j-rce] [critical] http://t/api",
	} {
		v := Check(Claim{Title: "x", Evidence: ev, ProofLevel: "P2"}, Options{ControlTested: true})
		if v.Gates[0].Name != "grounding" || !v.Gates[0].OK {
			t.Errorf("evidence=%q 应过 grounding，实际 %+v", ev, v.Gates[0])
		}
	}
}
