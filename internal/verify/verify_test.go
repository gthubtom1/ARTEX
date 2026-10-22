package verify

import "testing"

const goodEvidence = "GET /api/order/detail?id=1002 HTTP/1.1 (A 的 token) → 200，响应含 B 的手机号 138****1234"

func TestCheckVerifiedPath(t *testing.T) {
	v := Check(Claim{Title: "订单越权", Surface: "/api/order/detail?id=", Evidence: goodEvidence, ProofLevel: "P2"},
		Options{ControlTested: true, RetestOK: true})
	if v.Level != "verified" {
		t.Fatalf("want verified, got %+v", v)
	}
	if len(v.Gates) != 3 {
		t.Fatalf("want 3 gates, got %d", len(v.Gates))
	}
}

func TestCheckMissingEvidenceIsRejected(t *testing.T) {
	v := Check(Claim{Title: "SQL 注入", Evidence: "", ProofLevel: "P2"}, Options{})
	if v.Level != "rejected" {
		t.Fatalf("empty evidence must be rejected, got %+v", v)
	}
	if v.ProofLevel != "P1" {
		t.Fatalf("rejected claim must drop to P1, got %s", v.ProofLevel)
	}
}

func TestCheckHedgingIsRejected(t *testing.T) {
	v := Check(Claim{Title: "上传 RCE", Evidence: "可能存在上传点，未验证是否解析 php", ProofLevel: "P2"},
		Options{ControlTested: true})
	if v.Level != "rejected" {
		t.Fatalf("hedging must be rejected, got %+v", v)
	}
}

func TestCheckNoControlIsInferred(t *testing.T) {
	v := Check(Claim{Title: "订单越权", Evidence: goodEvidence, ProofLevel: "P2"}, Options{})
	if v.Level != "inferred" {
		t.Fatalf("no control test → inferred, got %+v", v)
	}
}

func TestCheckP3NeedsIndependentRepro(t *testing.T) {
	v := Check(Claim{Title: "反序列化 RCE", Evidence: "POST /api/x (payload=...) → 200，命令输出 uid=33(www-data)", ProofLevel: "P3"},
		Options{ControlTested: true})
	if v.Level != "inferred" {
		t.Fatalf("P3 without independent repro → inferred, got %+v", v)
	}
	v2 := Check(Claim{Title: "反序列化 RCE", Evidence: "POST /api/x (payload=...) → 200，命令输出 uid=33(www-data)", ProofLevel: "P3"},
		Options{ControlTested: true, Independent: true})
	if v2.Level != "verified" || v2.ProofLevel != "P3" {
		t.Fatalf("P3 with independent repro should stay verified/P3, got %+v", v2)
	}
}

func TestCheckToolVerifiedCapsP3(t *testing.T) {
	v := Check(Claim{Title: "CVE-2021-44228", Evidence: "nuclei template log4j-rce 命中 https://t/api，回连 dnslog 收到请求", ProofLevel: "P3"},
		Options{ControlTested: true, ToolVerified: true})
	// 工具级证据可算 verified；但没有独立路径时不得高于 P2。
	if v.Level != "verified" {
		t.Fatalf("tool verified should pass, got %+v", v)
	}
	if v.ProofLevel != "P2" {
		t.Fatalf("tool-only P3 must cap at P2, got %s", v.ProofLevel)
	}
}

func TestDescribe(t *testing.T) {
	v := Check(Claim{Title: "x", Evidence: goodEvidence, ProofLevel: "P2"}, Options{ControlTested: true})
	if d := v.Describe("x"); d == "" {
		t.Fatal("empty describe")
	}
}
