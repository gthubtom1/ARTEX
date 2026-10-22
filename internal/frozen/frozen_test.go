package frozen

import (
	"reflect"
	"sync"
	"testing"
)

func TestNewDedupAndIgnoreNonPositive(t *testing.T) {
	s := New([]int64{1, 2, 2, 0, -3}, []int64{2, 3}, nil, []int64{})
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3（去重并忽略 <=0）", s.Len())
	}
	for _, id := range []int64{1, 2, 3} {
		if !s.Has(id) {
			t.Fatalf("应包含 %d：%v", id, s.IDs())
		}
	}
	if s.Has(0) || s.Has(-3) || s.Has(4) {
		t.Fatalf("不应包含 0/-3/4：%v", s.IDs())
	}
	if got := New().Len(); got != 0 {
		t.Fatalf("New() 应空，得到 %d", got)
	}
}

func TestFromJSON(t *testing.T) {
	ok := []struct {
		name string
		in   string
		want []int64
	}{
		{"普通数组", "[1,2,3]", []int64{1, 2, 3}},
		{"带空格", " [ 1 , 2 , 3 ] ", []int64{1, 2, 3}},
		{"含非正数/重复", "[0,-1,2,2,null]", []int64{2}},
		{"字符串数字", `["4","5"]`, []int64{4, 5}},
		{"空数组", "[]", nil},
	}
	for _, tc := range ok {
		t.Run("ok/"+tc.name, func(t *testing.T) {
			s := FromJSON(tc.in)
			got := s.IDs()
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("FromJSON(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	empty := []struct {
		name string
		in   string
	}{
		{"空串", ""},
		{"纯空白", "   \n\t "},
		{"null 文本", "null"},
		{"JSON null", "null "},
		{"非法 JSON", "[1,2,"},
		{"不是数组", `{"a":1}`},
		{"垃圾", "oops"},
	}
	for _, tc := range empty {
		t.Run("empty/"+tc.name, func(t *testing.T) {
			s := FromJSON(tc.in)
			if s == nil {
				t.Fatal("应返回非 nil 空集合（可直接 Add）")
			}
			if s.Len() != 0 {
				t.Fatalf("FromJSON(%q) 应空，得到 %v", tc.in, s.IDs())
			}
		})
	}
}

func TestAddReturnValue(t *testing.T) {
	s := New()
	if !s.Add(7) {
		t.Fatal("首次 Add 应返回 true")
	}
	if s.Add(7) {
		t.Fatal("重复 Add 应返回 false")
	}
	if s.Add(0) || s.Add(-1) {
		t.Fatal("非正数 Add 应返回 false 且不入集")
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
	// nil Set 是只读空集：Add 静默 no-op，不 panic。
	var nilSet Set
	if nilSet.Add(1) {
		t.Fatal("nil Set 的 Add 应返回 false")
	}
	if nilSet.Len() != 0 || nilSet.Has(1) {
		t.Fatal("nil Set 应保持空")
	}
}

func TestHasBoundaries(t *testing.T) {
	s := New([]int64{1, 2})
	cases := map[int64]bool{0: false, -1: false, 1: true, 2: true, 3: false}
	for id, want := range cases {
		if got := s.Has(id); got != want {
			t.Fatalf("Has(%d) = %v, want %v", id, got, want)
		}
	}
	var nilSet Set
	if nilSet.Has(1) {
		t.Fatal("nil Set Has 应 false")
	}
}

func TestAny(t *testing.T) {
	s := New([]int64{10, 20, 30})
	if id, ok := s.Any([]int64{99, 20, 30}); !ok || id != 20 {
		t.Fatalf("Any 应命中第一个 20，得到 %d/%v", id, ok)
	}
	if id, ok := s.Any([]int64{1, 2, 3}); ok || id != 0 {
		t.Fatalf("Any 不应命中，得到 %d/%v", id, ok)
	}
	if id, ok := s.Any(nil); ok || id != 0 {
		t.Fatalf("nil ids 不应命中，得到 %d/%v", id, ok)
	}
	if id, ok := s.Any([]int64{0, -10}); ok || id != 0 {
		t.Fatalf("非正数不应命中，得到 %d/%v", id, ok)
	}
}

func TestIDsSorted(t *testing.T) {
	s := New([]int64{5, 3, 9, 1, 3})
	got := s.IDs()
	want := []int64{1, 3, 5, 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	// 返回的是副本：修改不影响集合。
	got[0] = 999
	if s.Has(999) || !s.Has(1) {
		t.Fatal("IDs 应返回副本")
	}
	var nilSet Set
	if nilSet.IDs() != nil {
		t.Fatal("nil Set 的 IDs 应为 nil")
	}
}

func TestMerge(t *testing.T) {
	s := New([]int64{1, 2})
	o := New([]int64{2, 3, 0, -4})
	got := s.Merge(o)
	if got.Len() != 3 || !got.Has(1) || !got.Has(2) || !got.Has(3) {
		t.Fatalf("Merge 结果不对：%v", got.IDs())
	}
	// 幂等：再合并一次不变。
	before := got.IDs()
	got = got.Merge(o)
	if !reflect.DeepEqual(before, got.IDs()) {
		t.Fatalf("Merge 应幂等：%v → %v", before, got.IDs())
	}
	// 与 nil 合并是 no-op。
	if got = got.Merge(nil); got.Len() != 3 {
		t.Fatalf("Merge(nil) 应 no-op，得到 %v", got.IDs())
	}
	// FromJSON 的脏数据合进来也会被过滤。
	got.Merge(FromJSON("garbage"))
	if got.Len() != 3 {
		t.Fatalf("Merge(空集) 应 no-op，得到 %v", got.IDs())
	}
}

// 100 个 goroutine 只读 Has/Any/IDs/Len（配合 -race 运行）。
func TestSetConcurrentRead(t *testing.T) {
	s := New([]int64{1, 2, 3, 4, 5})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if !s.Has(int64(i%5 + 1)) {
				t.Errorf("goroutine %d: 应命中 %d", i, i%5+1)
			}
			if _, ok := s.Any([]int64{0, -1, int64(i%5 + 1)}); !ok {
				t.Errorf("goroutine %d: Any 应命中", i)
			}
			if s.Len() != 5 {
				t.Errorf("goroutine %d: Len = %d", i, s.Len())
			}
			_ = s.IDs()
		}(i)
	}
	wg.Wait()
}
