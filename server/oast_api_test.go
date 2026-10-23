package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/internal/settings"
)

// TestOASTLoop 走一遍真实闭环：配置监听 → 生成探测地址 → 真的访问它 →
// 命中被记录 → GET /api/oast 能看到。这条覆盖"盲 SSRF 判据"最核心的一跳。
func TestOASTLoop(t *testing.T) {
	st := settings.New(nil)
	if err := st.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := &Server{settings: st}

	// 未配置时：通道关闭，工具为空（不占 worker token 预算）
	srv.startOAST()
	if srv.oastSrv != nil {
		t.Fatal("空配置不应启动通道")
	}
	if len(srv.oastTools()) != 0 {
		t.Fatal("通道关闭时不应暴露 oast 工具")
	}

	// 配一个随机端口（:0）启动
	if err := st.Set(context.Background(), "oast.http_addr", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	srv.startOAST()
	if srv.oastSrv == nil {
		t.Fatal("配了 http_addr 却没启动")
	}
	if len(srv.oastTools()) != 2 {
		t.Fatal("通道启用后应暴露 2 个 oast 工具")
	}

	tok := srv.oastSrv.NewToken()
	if tok.HTTPURL == "" {
		t.Fatal("token 没有 http_url")
	}

	// 真的去访问那个地址（模拟"目标被诱导回连"）
	resp, err := http.Get(tok.HTTPURL)
	if err != nil {
		t.Fatalf("访问探测地址失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// 命中要有记录（异步写入，给一点时间）
	var hits int
	for i := 0; i < 50; i++ {
		if hits = len(srv.oastSrv.Hits(tok.ID)); hits > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if hits == 0 {
		t.Fatal("访问了探测地址却没有命中记录")
	}

	// GET /api/oast 应该能读出来
	rec := httptest.NewRecorder()
	srv.getOAST(rec, httptest.NewRequest(http.MethodGet, "/api/oast", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var body struct {
		Enabled bool `json:"enabled"`
		Hits    []struct {
			TokenID string `json:"token_id"`
			Source  string `json:"source"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled || len(body.Hits) == 0 {
		t.Fatalf("enabled=%v hits=%d", body.Enabled, len(body.Hits))
	}
	if body.Hits[0].TokenID != tok.ID {
		t.Fatalf("命中归因错了: %+v", body.Hits[0])
	}

	// 非法 public_base 必须拒绝（否则会生成目标访问不到的地址）
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/oast", strings.NewReader(`{"http_addr":"127.0.0.1:0","public_base":"1.2.3.4:8888"}`))
	srv.updateOAST(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 public_base 竟然接受: code=%d", rec.Code)
	}
}
