package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/snapshot"
)

// TestSnapshotRestoreEndpoint 走一遍真实闭环：
// 留档 → 改坏文件 → 列表能看到 → POST restore（带 confirm）→ 内容还原。
// 这条覆盖的正是 D1/D2 审查点出来的"Restore 零调用 = 可回滚是空话"。
func TestSnapshotRestoreEndpoint(t *testing.T) {
	root := t.TempDir()
	st := snapshot.New(filepath.Join(root, "snapshots"))
	srv := &Server{snapshots: st}

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("原始内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := st.Create(42, "写前留档", []string{target})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 改坏
	if err := os.WriteFile(target, []byte("被改坏了"), 0o644); err != nil {
		t.Fatal(err)
	}

	// ① 列表
	rec := httptest.NewRecorder()
	srv.listSnapshots(rec, httptest.NewRequest(http.MethodGet, "/api/snapshots?task_id=42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Snapshots []snapshotRow `json:"snapshots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Snapshots) != 1 || listed.Snapshots[0].ID != snap.ID {
		t.Fatalf("列表不对: %+v", listed.Snapshots)
	}
	if listed.Snapshots[0].TaskID != 42 || listed.Snapshots[0].Files != 1 {
		t.Fatalf("列表字段不对: %+v", listed.Snapshots[0])
	}

	// ② 不带 confirm 必须拒绝（破坏性动作的守门）
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/snapshots/x/restore", strings.NewReader(`{}`))
	req.SetPathValue("id", snap.ID)
	srv.restoreSnapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无 confirm 竟然放行: code=%d", rec.Code)
	}
	if b, _ := os.ReadFile(target); string(b) != "被改坏了" {
		t.Fatal("无 confirm 居然改了文件")
	}

	// ③ 带 confirm 真回滚
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/snapshots/x/restore", strings.NewReader(`{"confirm":true}`))
	req.SetPathValue("id", snap.ID)
	srv.restoreSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore code=%d body=%s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "原始内容" {
		t.Fatalf("回滚后内容=%q，应为「原始内容」", string(got))
	}

	// ④ 回滚前系统会自己再留一份档（误操作能退回来）
	all, err := st.List(42)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("回滚前应自动留档，实际只有 %d 份", len(all))
	}
	found := false
	for _, s := range all {
		if strings.HasPrefix(s.Reason, "restore-before:") {
			found = true
		}
	}
	if !found {
		t.Fatal("没找到 restore-before 留档")
	}
}
