package server

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)

// 进展清单（GET /api/roadmap）：把"这个产品做到哪一步、还剩什么没做"直接摆到页面上。
//
// 为什么要有（2026-10-25 用户原话）：「你这个有个清单我都不知道到底有哪些没有完成，
// 你为什么不能在我的页面上面显示出来呢」—— 未完成项只写在 BACKLOG/docs 里，
// 用户（零基础、不看仓库）根本看不见，只能每次问。
//
// 数据源就是 server/roadmap.json（跟二进制一起发出去，不会"文档在但页面没有"）。
// 纪律：状态只有 done / todo / blocked / doing 四种；todo 与 blocked **必须**写 why_not，
// 否则前端会显示"没写原因"——逼着维护者说清楚，而不是堆一个空清单。

//go:embed roadmap.json
var roadmapJSON []byte

type roadmapItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`   // done | doing | todo | blocked
	Why    string `json:"why_not"`  // 没做的原因（done 可空）
	Accept string `json:"accept"`   // 怎么算做完（可执行判据）
	Proof  string `json:"evidence"` // 已完成项的判据（真机/测试）
}

type roadmapGroup struct {
	ID    string        `json:"id"`
	Title string        `json:"title"`
	Why   string        `json:"why"`
	Items []roadmapItem `json:"items"`
}

type roadmapDoc struct {
	Version string         `json:"version"`
	Updated string         `json:"updated"`
	Intro   string         `json:"intro"`
	Groups  []roadmapGroup `json:"groups"`
}

type roadmapResp struct {
	roadmapDoc
	// 统计（前端直接显示"X 件已完成 / Y 件没做"，不需要自己数）
	Done    int `json:"done"`
	Todo    int `json:"todo"`
	Blocked int `json:"blocked"`
	Total   int `json:"total"`
	// 一句话总结（给零基础看的那句人话）
	Headline string `json:"headline"`
}

func (s *Server) registerRoadmapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/roadmap", s.apiRoadmap)
}

func (s *Server) apiRoadmap(w http.ResponseWriter, _ *http.Request) {
	out, err := buildRoadmap(roadmapJSON)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "进展清单读取失败")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// buildRoadmap 是纯函数（可单测）：解析 + 统计 + 补全缺失字段。
func buildRoadmap(raw []byte) (roadmapResp, error) {
	var doc roadmapDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return roadmapResp{}, err
	}
	out := roadmapResp{roadmapDoc: doc}
	for gi := range out.Groups {
		for ii := range out.Groups[gi].Items {
			it := &out.Groups[gi].Items[ii]
			out.Total++
			switch it.Status {
			case "done":
				out.Done++
			case "blocked":
				out.Blocked++
				out.Todo++
			default:
				out.Todo++
			}
			// 没做又没写原因 → 明确标出来（前端会把它涂红），不许空着糊过去。
			if it.Status != "done" && strings.TrimSpace(it.Why) == "" {
				it.Why = "（没写原因：这条清单不合格，维护者要补）"
			}
			if it.Status == "done" && strings.TrimSpace(it.Proof) == "" {
				it.Proof = "（没写判据：这条完成没有得到验证）"
			}
		}
	}
	out.Headline = roadmapHeadline(out.Done, out.Todo, out.Blocked)
	return out, nil
}

// roadmapHeadline 生成一句话结论，避免用户自己算百分比。
func roadmapHeadline(done, todo, blocked int) string {
	switch {
	case todo == 0:
		return "全部做完了。"
	case done == 0:
		return "还没开始。"
	case blocked > 0:
		return "核心链路已经能跑，还剩 " + itoa(todo) + " 件（其中 " + itoa(blocked) + " 件要你点头或换环境）。"
	default:
		return "核心链路已经能跑，还剩 " + itoa(todo) + " 件没做。"
	}
}

// itoa 避免为一行拼串引入 strconv 依赖噪音。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
