package adapter

import (
	"strings"
	"testing"
)

const nmapFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nmaprun>
<nmaprun scanner="nmap" args="nmap -oX - 10.0.0.0/30" start="1700000000">
  <host starttime="1700000001">
    <status state="up" reason="arp-response"/>
    <address addr="10.0.0.1" addrtype="ipv4"/>
    <address addr="AA:BB:CC:DD:EE:01" addrtype="mac"/>
    <hostnames><hostname name="edge.example.com" type="PTR"/></hostnames>
    <ports>
      <port protocol="tcp" portid="22">
        <state state="open" reason="syn-ack"/>
        <service name="ssh" product="OpenSSH" version="8.9p1">
          <cpe>cpe:/a:openbsd:openssh:8.9p1</cpe>
          <cpe>cpe:/o:linux:linux_kernel</cpe>
        </service>
      </port>
      <port protocol="tcp" portid="80">
        <state state="open" reason="syn-ack"/>
        <service name="http" product="nginx" version="1.24.0">
          <cpe>cpe:/a:igor_sysoev:nginx:1.24.0</cpe>
        </service>
      </port>
      <port protocol="tcp" portid="443">
        <state state="closed" reason="reset"/>
        <service name="https" product="nginx" version="1.24.0"/>
      </port>
    </ports>
  </host>
  <host starttime="1700000002">
    <status state="up" reason="echo-reply"/>
    <address addr="10.0.0.2" addrtype="ipv4"/>
    <ports>
      <port protocol="udp" portid="53">
        <state state="open" reason="udp-response"/>
        <service name="domain" product="BIND" version="9.18"/>
      </port>
    </ports>
  </host>
  <runstats><finished time="1700000003"/></runstats>
</nmaprun>`

func TestNmapXML(t *testing.T) {
	res, err := NmapXML().Parse(nmapFixture, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK || res.Tool != "nmap" {
		t.Fatalf("bad result header: %+v", res)
	}
	if len(res.Items) != 3 {
		t.Fatalf("want 3 open ports, got %d: %+v", len(res.Items), res.Items)
	}
	for _, it := range res.Items {
		if it.Kind != "port" {
			t.Fatalf("want kind=port, got %q", it.Kind)
		}
		if it.Data["state"] != "open" {
			t.Fatalf("closed port leaked: %+v", it.Data)
		}
	}
	first := res.Items[0].Data
	if first["host"] != "edge.example.com" {
		t.Errorf("host = %v, want edge.example.com", first["host"])
	}
	if first["ip"] != "10.0.0.1" {
		t.Errorf("ip = %v, want 10.0.0.1", first["ip"])
	}
	if first["port"] != 22 || first["proto"] != "tcp" || first["service"] != "ssh" {
		t.Errorf("bad port fields: %+v", first)
	}
	if first["product"] != "OpenSSH" || first["version"] != "8.9p1" {
		t.Errorf("bad service fields: %+v", first)
	}
	cpes, ok := first["cpe"].([]string)
	if !ok || len(cpes) != 2 {
		t.Errorf("cpe = %#v, want 2 strings", first["cpe"])
	}
	second := res.Items[1].Data
	if second["port"] != 80 || second["service"] != "http" {
		t.Errorf("bad second port: %+v", second)
	}
	third := res.Items[2].Data
	if third["host"] != "10.0.0.2" {
		t.Errorf("hostname fallback = %v, want 10.0.0.2", third["host"])
	}
	if third["port"] != 53 || third["proto"] != "udp" || third["service"] != "domain" {
		t.Errorf("bad udp port: %+v", third)
	}
	if cpes, ok := third["cpe"].([]string); !ok || len(cpes) != 0 {
		t.Errorf("missing cpe should be empty slice, got %#v", third["cpe"])
	}
}

func TestNmapXMLInvalid(t *testing.T) {
	res, err := NmapXML().Parse("<nmaprun><host><ports>", "")
	if err == nil {
		t.Fatal("want error for malformed XML, got nil")
	}
	if res.OK {
		t.Fatal("want OK=false on parse error")
	}
	if res.Err == "" {
		t.Fatal("want Err filled on parse error")
	}
}

const httpxFixture = `{"url":"https://a.example.com","status":200,"title":"Alpha","tech":["nginx","React"],"cdn":false,"webserver":"nginx","content_length":1234}
{"url":"https://b.example.com","status":301,"title":"Beta","tech":["IIS"],"cdn":true,"webserver":"Microsoft-IIS/10.0","content_length":0}

{"url":"http://c.example.com:8080","status":403,"title":"Gamma","tech":"Tomcat","cdn":false,"webserver":"Apache-Coyote/1.1","content_length":42}
{"url":"https://d.example.com","status":200,`

func TestHttpxJSONL(t *testing.T) {
	res, err := HttpxJSONL().Parse(httpxFixture, "")
	if err != nil {
		t.Fatalf("dirty line must not fail the whole parse: %v", err)
	}
	if !res.OK || res.Tool != "httpx" {
		t.Fatalf("bad result header: %+v", res)
	}
	if len(res.Items) != 3 {
		t.Fatalf("want 3 items, got %d: %+v", len(res.Items), res.Items)
	}
	for _, it := range res.Items {
		if it.Kind != "web" {
			t.Fatalf("want kind=web, got %q", it.Kind)
		}
	}
	first := res.Items[0].Data
	if first["url"] != "https://a.example.com" || first["status"] != 200 {
		t.Errorf("bad first item: %+v", first)
	}
	if first["title"] != "Alpha" || first["webserver"] != "nginx" || first["content_length"] != 1234 {
		t.Errorf("bad first item: %+v", first)
	}
	if tech, ok := first["tech"].([]string); !ok || len(tech) != 2 || tech[0] != "nginx" {
		t.Errorf("tech = %#v", first["tech"])
	}
	if first["cdn"] != false {
		t.Errorf("cdn = %v, want false", first["cdn"])
	}
	if res.Items[1].Data["cdn"] != true {
		t.Errorf("cdn = %v, want true", res.Items[1].Data["cdn"])
	}
	third := res.Items[2].Data
	if tech, ok := third["tech"].([]string); !ok || len(tech) != 1 || tech[0] != "Tomcat" {
		t.Errorf("string tech should normalize to []string, got %#v", third["tech"])
	}
	if third["status"] != 403 || third["content_length"] != 42 {
		t.Errorf("bad third item: %+v", third)
	}
}

func TestHttpxAllDirty(t *testing.T) {
	res, err := HttpxJSONL().Parse("not json\n{\"url\": \"x\",\n", "")
	if err == nil {
		t.Fatal("want error when every non-empty line is invalid")
	}
	if res.OK || res.Err == "" {
		t.Fatalf("want OK=false + Err, got %+v", res)
	}
}

func TestHttpxEmpty(t *testing.T) {
	res, err := HttpxJSONL().Parse("\n \n\r\n", "")
	if err != nil {
		t.Fatalf("empty output is not an error: %v", err)
	}
	if !res.OK || len(res.Items) != 0 {
		t.Fatalf("want ok with zero items, got %+v", res)
	}
}

const nucleiFixture = `{"template-id":"CVE-2021-41773","info":{"name":"Apache Path Traversal","severity":"High"},"matched-at":"http://a.example.com/cgi-bin/.%2e/.%2e/etc/passwd","type":"http","curl-command":"curl -X GET http://a.example.com/","host":"a.example.com"}
{"template-id":"tech-detect","info":{"name":"Nginx Detection","severity":"MeDiUm"},"matched-at":"https://b.example.com","type":"http","host":"b.example.com"}
{"template-id":"broken","info":`

func TestNucleiJSONL(t *testing.T) {
	res, err := NucleiJSONL().Parse(nucleiFixture, "")
	if err != nil {
		t.Fatalf("dirty line must be skipped: %v", err)
	}
	if !res.OK || res.Tool != "nuclei" {
		t.Fatalf("bad result header: %+v", res)
	}
	if len(res.Items) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(res.Items), res.Items)
	}
	for _, it := range res.Items {
		if it.Kind != "finding" {
			t.Fatalf("want kind=finding, got %q", it.Kind)
		}
	}
	first := res.Items[0].Data
	if first["template_id"] != "CVE-2021-41773" || first["name"] != "Apache Path Traversal" {
		t.Errorf("bad first finding: %+v", first)
	}
	if first["severity"] != "high" {
		t.Errorf("severity = %v, want lower-case high", first["severity"])
	}
	if first["matched_at"] != "http://a.example.com/cgi-bin/.%2e/.%2e/etc/passwd" {
		t.Errorf("matched_at = %v", first["matched_at"])
	}
	if first["type"] != "http" || first["curl"] == "" || first["host"] != "a.example.com" {
		t.Errorf("bad first finding: %+v", first)
	}
	if res.Items[1].Data["severity"] != "medium" {
		t.Errorf("severity = %v, want medium", res.Items[1].Data["severity"])
	}
	if res.Items[1].Data["curl"] != "" {
		t.Errorf("missing curl should be empty string, got %v", res.Items[1].Data["curl"])
	}
}

func TestNucleiAllDirty(t *testing.T) {
	if _, err := NucleiJSONL().Parse("oops\n{\"template-id\":\n", ""); err == nil {
		t.Fatal("want error when every non-empty line is invalid")
	}
}

func TestSummarize(t *testing.T) {
	res, err := NmapXML().Parse(nmapFixture, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	summary := Summarize(res)
	if summary == "" {
		t.Fatal("summary must not be empty")
	}
	if lines := strings.Count(summary, "\n") + 1; lines < 3 || lines > 5 {
		t.Fatalf("want 3-5 lines, got %d:\n%s", lines, summary)
	}
	if !strings.Contains(summary, "3") {
		t.Fatalf("summary should contain port count 3:\n%s", summary)
	}
	if !strings.Contains(summary, "nmap") {
		t.Fatalf("summary should contain tool name:\n%s", summary)
	}

	httpxRes, err := HttpxJSONL().Parse(httpxFixture, "")
	if err != nil {
		t.Fatalf("httpx parse: %v", err)
	}
	if s := Summarize(httpxRes); !strings.Contains(s, "3") {
		t.Fatalf("httpx summary should contain target count 3:\n%s", s)
	}

	nucleiRes, err := NucleiJSONL().Parse(nucleiFixture, "")
	if err != nil {
		t.Fatalf("nuclei parse: %v", err)
	}
	if s := Summarize(nucleiRes); !strings.Contains(s, "2") {
		t.Fatalf("nuclei summary should contain finding count 2:\n%s", s)
	}

	failed := Summarize(Result{Tool: "nmap", OK: false, Err: "XML 解析失败"})
	if !strings.Contains(failed, "XML 解析失败") {
		t.Fatalf("failure summary should keep the reason:\n%s", failed)
	}
}

func TestOversizeOutputRejected(t *testing.T) {
	huge := strings.Repeat("a", maxStdoutBytes+1)
	for _, p := range All() {
		res, err := p.Parse(huge, "")
		if err == nil {
			t.Fatalf("%s: want error for oversize stdout", p.Name())
		}
		if res.OK || res.Err == "" {
			t.Fatalf("%s: want OK=false + Err, got %+v", p.Name(), res)
		}
	}
}

func TestAll(t *testing.T) {
	parsers := All()
	if len(parsers) != 3 {
		t.Fatalf("want 3 parsers, got %d", len(parsers))
	}
	want := map[string]bool{"nmap": true, "httpx": true, "nuclei": true}
	for _, p := range parsers {
		if p.Name() == "" || !want[p.Name()] {
			t.Errorf("unexpected parser name %q", p.Name())
		}
		delete(want, p.Name())
	}
	if len(want) != 0 {
		t.Errorf("missing parsers: %v", want)
	}
}
