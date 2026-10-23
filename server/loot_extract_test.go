package server

import (
	"strings"
	"testing"
)

// findLootObject 按名字（大小写不敏感）取抽取结果，找不到返回 nil。
func findLootObject(objs []lootObject, name string) *lootObject {
	for i := range objs {
		if strings.EqualFold(objs[i].Name, name) {
			return &objs[i]
		}
	}
	return nil
}

func TestExtractObjectsRejectsCode(t *testing.T) {
	t.Run("code identifiers are not objects", func(t *testing.T) {
		ev := `报错：conn.Execute("SELECT * FROM users") 失败；Request.QueryString("item") 可取；Server.MapPath(".") 暴露物理路径`
		objs := extractLootObjects([]lootFinding{{id: 1, evidence: ev}})
		for _, o := range objs {
			low := strings.ToLower(o.Name)
			for _, bad := range []string{"conn", "request", "server", "mappath", "querystring", "execute"} {
				if strings.Contains(low, bad) {
					t.Fatalf("黑名单/代码标识符被当成对象: %q (bad=%q, all=%v)", o.Name, bad, objs)
				}
			}
			for _, part := range strings.Split(low, ".") {
				if lootObjectBlacklist[part] {
					t.Fatalf("黑名单项被当成对象: %q (all=%v)", o.Name, objs)
				}
			}
		}
		if findLootObject(objs, "users") == nil {
			t.Fatalf("users 应该被抽出，实际: %v", objs)
		}
	})

	t.Run("schema qualified object and nearby fields", func(t *testing.T) {
		ev := "跨库注入可读到 acublog.dbo.users 里的 用户名、密码、卡密"
		objs := extractLootObjects([]lootFinding{{id: 2, evidence: ev}})
		o := findLootObject(objs, "acublog.dbo.users")
		if o == nil {
			t.Fatalf("没抽出 acublog.dbo.users，实际: %v", objs)
		}
		if !strings.Contains(o.Fields, "用户名") {
			t.Fatalf("acublog.dbo.users 的 fields 应含 用户名，实际: %q", o.Fields)
		}
	})

	t.Run("fields only within 120 chars after object", func(t *testing.T) {
		ev := "返回了 forums " + strings.Repeat("测", 200) + "标题"
		objs := extractLootObjects([]lootFinding{{id: 3, evidence: ev}})
		o := findLootObject(objs, "forums")
		if o == nil {
			t.Fatalf("没抽出 forums，实际: %v", objs)
		}
		if o.Fields != "" {
			t.Fatalf("标题在 120 字符窗口之外，fields 应为空，实际: %q", o.Fields)
		}
	})

	t.Run("same object is deduped and fields merged", func(t *testing.T) {
		ev := "users 用户名 " + strings.Repeat("测", 200) + " users 密码"
		objs := extractLootObjects([]lootFinding{{id: 4, evidence: ev}})
		if len(objs) != 1 {
			t.Fatalf("users 应去重成一条，实际: %v", objs)
		}
		if objs[0].Name != "users" {
			t.Fatalf("对象名应为 users，实际: %q", objs[0].Name)
		}
		for _, w := range []string{"用户名", "密码"} {
			if !strings.Contains(objs[0].Fields, w) {
				t.Fatalf("fields 应合并出 %q，实际: %q", w, objs[0].Fields)
			}
		}
	})

	t.Run("plain data sentence uses chinese object names", func(t *testing.T) {
		ev := "acublog.dbo.users 用户名、密码；orders 订单、金额；users 手机号"
		objs := extractLootObjects([]lootFinding{{id: 5, evidence: ev}})
		got := lootPlain("data", "数据拉取（用户/订单/卡密）", "已确认", objs, "done")
		want := "能把它的用户表、订单表拉出来"
		if got != want {
			t.Fatalf("plain 句式不对，want %q, got %q (objs=%v)", want, got, objs)
		}
		if got := lootPlain("data", "数据拉取（用户/订单/卡密）", "已确认", nil, "done"); got != "能读到它的业务数据" {
			t.Fatalf("无对象时应兜底，实际: %q", got)
		}
	})
}
