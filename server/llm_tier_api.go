package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/llmmodel"
)

// P8-9：模型分层引导。
//
// per-agent 绑定其实早就支持了（agentKey = goals/planner/worker/mainagent/adviser/
// verifier/retester），但建任务时没有任何引导，用户默认全用同一个模型 ——
// planner 用便宜模型 → 规划质量差 → 看起来"平台不行"。
//
// 这里只做两件事：
//   1. 三档预设（settings: llm.tier.<agentKey> = profile id）；
//   2. 给出建议（按模型名猜档位），让用户一键采用。
//
// 优先级（不可颠倒）：任务显式 LLM 配置链 > agent 绑定 > 档位映射 > 全局激活配置。

// tierAgents 是允许配档位的角色（顺序即展示顺序）。
var tierAgents = []string{"planner", "worker", "verifier", "adviser"}

// tierProfileFor 返回某角色配置的档位 profile id（未配置/非法 → 0）。
func (s *Server) tierProfileFor(agentKey string) int64 {
	if s.settings == nil || agentKey == "" {
		return 0
	}
	raw := strings.TrimSpace(s.settings.String("llm.tier."+agentKey, ""))
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// tierMap 读出当前全部档位映射。
func (s *Server) tierMap() map[string]int64 {
	out := map[string]int64{}
	for _, k := range tierAgents {
		if id := s.tierProfileFor(k); id > 0 {
			out[k] = id
		}
	}
	return out
}

// loadLLMProfiles 把 db 的 profile 列表映射成 llmmodel.Profile（不含密钥）。
func (s *Server) loadLLMProfiles() ([]llmmodel.Profile, int64) {
	var out []llmmodel.Profile
	var activeID int64
	if s.m == nil || s.m.pg == nil {
		return out, 0
	}
	list, err := s.m.pg.ListProfiles()
	if err != nil {
		return out, 0
	}
	for _, p := range list {
		if p == nil {
			continue
		}
		out = append(out, llmmodel.Profile{ID: p.ID, Name: p.Name, Model: p.Model})
	}
	if a, err := s.m.pg.ActiveProfile(); err == nil && a != nil {
		activeID = a.ID
	}
	return out, activeID
}

func (s *Server) registerTierRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/llm/tier-suggest", s.getTierSuggest)
	mux.HandleFunc("PUT /api/llm/tier", s.putTier)
}

// tierProfileView 是给前端的 profile 摘要（永远不含 api_key）。
type tierProfileView struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
	Tier  string `json:"tier"`
	Note  string `json:"note"`
}

func (s *Server) getTierSuggest(w http.ResponseWriter, r *http.Request) {
	profiles, activeID := s.loadLLMProfiles()
	views := make([]tierProfileView, 0, len(profiles))
	for _, p := range profiles {
		t := llmmodel.Classify(p.Model)
		views = append(views, tierProfileView{
			ID: p.ID, Name: p.Name, Model: p.Model,
			Tier: string(t), Note: t.Describe(),
		})
	}
	sug := llmmodel.Suggest(tierAgents, s.tierMap(), activeID, profiles)
	writeJSON(w, 200, map[string]any{
		"suggest":     sug,
		"reason":      llmmodel.Reason(sug, profiles),
		"profiles":    views,
		"current":     s.tierMap(),
		"active_id":   activeID,
		"agent_order": tierAgents,
	})
}

func (s *Server) putTier(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// nil = 不改；0 = 清空该角色（回落全局激活配置）
		Planner  *int64 `json:"planner"`
		Worker   *int64 `json:"worker"`
		Verifier *int64 `json:"verifier"`
		Adviser  *int64 `json:"adviser"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	pairs := map[string]*int64{
		"planner": req.Planner, "worker": req.Worker,
		"verifier": req.Verifier, "adviser": req.Adviser,
	}
	// 先校验全部 id 都存在，避免写一半。
	profiles, _ := s.loadLLMProfiles()
	exists := map[int64]bool{}
	for _, p := range profiles {
		exists[p.ID] = true
	}
	for k, v := range pairs {
		if v == nil {
			continue
		}
		if *v < 0 || (*v > 0 && !exists[*v]) {
			writeErr(w, 400, "角色 "+k+" 指定的模型配置不存在："+strconv.FormatInt(*v, 10))
			return
		}
	}
	for k, v := range pairs {
		if v == nil {
			continue
		}
		val := ""
		if *v > 0 {
			val = strconv.FormatInt(*v, 10)
		}
		if err := s.m.pg.SetSetting("llm.tier."+k, val); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if s.settings != nil {
			_ = s.settings.Set(r.Context(), "llm.tier."+k, val)
		}
	}
	writeJSON(w, 200, map[string]any{"current": s.tierMap()})
}
