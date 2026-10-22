// Package frozen 维护「已判定换目标、不再为该资产派单」的资产 id 集合。
//
// 来源是决策队列里 action=switch 的裁决（decision.Verdict.FreezeAssetIDs），
// 落库在 capabilities.frozen_asset_ids（bigint[]）；读回来时用
// array_to_json(frozen_asset_ids)::text 拿到形如 "[1,2,3]" 的文本，交给 FromJSON。
//
// 本包是纯函数式的（只依赖标准库），方便单测与在 agent 里缓存。
package frozen

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Set 是「已判定换目标、不再为该资产派单」的资产 id 集合。
//
// 零值不可写（map 为 nil 时 Add 会 panic）；请用 New / FromJSON 构造，
// 或直接 Set{}。Has / Any / IDs / Len 在 nil 上也安全。
type Set map[int64]struct{}

// New 从若干个 id 切片构造集合（自动去重、忽略 <=0 的值）。
func New(ids ...[]int64) Set {
	s := make(Set)
	for _, group := range ids {
		for _, id := range group {
			if id > 0 {
				s[id] = struct{}{}
			}
		}
	}
	return s
}

// FromJSON 解析 PG 里 array_to_json 出来的文本（形如 "[1,2,3]"）。
//
// 空串、空白、裸 "null"、JSON null、非法 JSON、元素类型不对——一律返回空集合且不报错：
// 调用方（planner 热路径）不该因为一行脏数据就失败。
// 也容忍纯数字组成的字符串元素（PG 的 bigint[] 经 array_to_json 是数字，
// 但历史数据/别的写入点可能塞成 "1"），以及带空格的数组。
func FromJSON(s string) Set {
	out := make(Set)
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return out
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return out
	}
	for _, m := range raw {
		if id, ok := toID(m); ok {
			out[id] = struct{}{}
		}
	}
	return out
}

// toID 把一个 JSON 元素转成正 id（<=0 / 非数字都算无效）。
func toID(m json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(m))
	if text == "" || text == "null" {
		return 0, false
	}
	if text[0] == '"' { // 字符串数字，如 "42"
		var str string
		if err := json.Unmarshal(m, &str); err != nil {
			return 0, false
		}
		text = strings.TrimSpace(str)
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// Add 加入（返回是否新增）。
//
// nil Set 视为只读空集：Add 返回 false（值接收者无法把新建的 map 交回调用方，
// 所以这里选择静默 no-op 而不是 panic）。请始终用 New / FromJSON 构造。
func (s Set) Add(id int64) bool {
	if id <= 0 || s == nil {
		return false
	}
	if _, ok := s[id]; ok {
		return false
	}
	s[id] = struct{}{}
	return true
}

// Has 判断是否命中（<=0 恒 false）。
func (s Set) Has(id int64) bool {
	if id <= 0 {
		return false
	}
	_, ok := s[id]
	return ok
}

// Any 判断 ids 中是否有任一命中；返回命中的那个 id（按 ids 顺序的第一个）与是否命中。
func (s Set) Any(ids []int64) (int64, bool) {
	for _, id := range ids {
		if s.Has(id) {
			return id, true
		}
	}
	return 0, false
}

// IDs 返回排序后的 id 切片（便于日志/断言）。空集合返回 nil。
func (s Set) IDs() []int64 {
	if len(s) == 0 {
		return nil
	}
	out := make([]int64, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Len 返回集合大小。
func (s Set) Len() int { return len(s) }

// Merge 合并另一个集合（原地，返回 s）。o 中的非正数被忽略，nil o 是 no-op。
//
// s 本身是 nil 时无法就地初始化（值接收者），直接返回 nil——调用方若需要结果请
// 用 `s = s.Merge(o)` 并确保 s 来自 New / FromJSON。
func (s Set) Merge(o Set) Set {
	if s == nil {
		return s
	}
	for id := range o {
		if id <= 0 {
			continue
		}
		s[id] = struct{}{}
	}
	return s
}
