package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/llmrec"
)

// 战况卡 P0-c：「每个漏洞能换到什么」接 AI（按证据 hash 缓存，失败回落规则版）。
//
// 纪律与 loot_ai.go 一致：
//   - 状态/等级/已拿到 全部由规则算，AI 只写**一句解释**；
//   - 缓存键 = sha256(finding 的证据文本)，证据一变就重新写，不拿旧解释冒充新结论；
//   - 调用失败什么都不写，brief 继续用规则模板（abilityText），前端不需要重试逻辑。
//
// 单次调用覆盖多条：一条漏洞一次请求太贵（6 条漏洞 6 次往返），
// 所以合成一次 JSON 批量，缓存仍按 finding 逐条落（下次只有新漏洞才再问）。

const briefAbilitySystem = `你是给非技术委托人解释渗透成果的助手。只根据给定的一条已确认漏洞，说明它现在能换来什么现实后果。要求：一句话，最多 40 个字；全中文；不出现英文技术术语、产品名、命令、URL；不写具体数量；不编造材料里没有的表名、字段或对象；不要提“证据”“输入”“漏洞”这些词。`

type briefAbilityAI struct {
	FindingID int64  `json:"finding_id"`
	Grants    string `json:"grants"`
}

// briefAbilityHash 一条 finding 的缓存键：标题 + 摘要 + 证据原文。
// 只用 hash，不存原文；证据变了 hash 就变，旧解释自然失效。
func briefAbilityHash(f lootFinding) string {
	h := sha256.New()
	h.Write([]byte(f.title))
	h.Write([]byte{0})
	h.Write([]byte(f.summary))
	h.Write([]byte{0})
	h.Write([]byte(f.evidence))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// briefAbilityCache 一次取全任务的 AI 解释（finding_id → (hash, text)）。
// 读不到就返回空表：AI 是增强项，读不到不该让战况卡失败。
func (s *Server) briefAbilityCache(ctx context.Context, taskID int64) map[int64]struct {
	Hash string
	Text string
} {
	out := map[int64]struct {
		Hash string
		Text string
	}{}
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx,
		`SELECT finding_id, evidence_hash, text FROM finding_abilities WHERE task_id=$1 AND status='done'`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var hash, text string
		if rows.Scan(&id, &hash, &text) == nil {
			out[id] = struct {
				Hash string
				Text string
			}{Hash: hash, Text: text}
		}
	}
	return out
}

func (s *Server) saveBriefAbility(taskID, findingID int64, hash, status, text, model string) {
	if s.m == nil || s.m.pg == nil {
		return
	}
	if _, err := s.m.pg.Exec(`INSERT INTO finding_abilities(task_id,finding_id,evidence_hash,status,text,model,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (task_id,finding_id) DO UPDATE SET
			evidence_hash=EXCLUDED.evidence_hash, status=EXCLUDED.status,
			text=EXCLUDED.text, model=EXCLUDED.model, updated_at=now()`,
		taskID, findingID, hash, status, text, model); err != nil {
		log.Printf("[brief] save ability failed (task=%d finding=%d): %v", taskID, findingID, err)
	}
}

// taskBriefAI：把「能换到什么」交给 AI 写一遍，落缓存，返回新的 can 列表。
// 请求体可选 {"finding_ids":[1,2],"force":true}；不带就取前 8 条**还没有 AI 解释**的。
func (s *Server) taskBriefAI(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m == nil || s.m.pg == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	var body struct {
		FindingIDs []int64 `json:"finding_ids"`
		Force      bool    `json:"force"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)

	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	fs := s.lootFindings(t)
	if len(fs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"can": []briefAbility{}, "asked": 0, "filled": 0, "model": "rule"})
		return
	}
	byID := map[int64]lootFinding{}
	for _, f := range fs {
		byID[f.id] = f
	}
	cache := s.briefAbilityCache(r.Context(), taskID)

	// 选出这次要问的：显式指定优先，否则按 id 升序取前 8 条未缓存的。
	var want []lootFinding
	if len(body.FindingIDs) > 0 {
		for _, id := range body.FindingIDs {
			f, ok := byID[id]
			if !ok {
				continue
			}
			if !body.Force {
				if c, hit := cache[id]; hit && c.Hash == briefAbilityHash(f) && strings.TrimSpace(c.Text) != "" {
					continue
				}
			}
			want = append(want, f)
		}
	} else {
		for _, f := range fs {
			if c, hit := cache[f.id]; hit && c.Hash == briefAbilityHash(f) && strings.TrimSpace(c.Text) != "" {
				continue
			}
			want = append(want, f)
			if len(want) >= 8 {
				break
			}
		}
	}

	if len(want) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"can": s.briefCan(r.Context(), t, taskID, s.lootRowsForTask(r.Context(), t, fs), s.impactStats(r.Context(), t, taskID)), "asked": 0, "filled": 0, "model": "cache",
		})
		return
	}

	var b strings.Builder
	b.WriteString("已确认的漏洞（逐条给一句话）：\n")
	for _, f := range want {
		fmt.Fprintf(&b, "#%d %s\n", f.id, f.title)
		if sum := strings.TrimSpace(f.summary); sum != "" {
			fmt.Fprintf(&b, "  摘要：%s\n", truncateRunes(sum, 200))
		}
		if ev := strings.TrimSpace(f.evidence); ev != "" {
			fmt.Fprintf(&b, "  材料：%s\n", truncateRunes(ev, 300))
		}
	}
	b.WriteString("\n只输出一个 JSON 数组，不要 Markdown：")
	b.WriteString(`[{"finding_id":123,"grants":"这句话"}]`)
	b.WriteString("\n每条都必须用上面真实存在的 finding_id；拿不准就写“需要打开证据看具体能读到什么”。")

	ctx, cancel := context.WithTimeout(llmrec.WithWorker(r.Context(), "brief"), 60*time.Second)
	defer cancel()
	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "adviser"}
	raw, lerr := collectText(ctx, rt, true, briefAbilitySystem, b.String(), 1500)
	if lerr != nil {
		log.Printf("[brief] ai ability failed (task=%d): %v", taskID, lerr)
		writeJSON(w, http.StatusOK, map[string]any{
			"can": s.briefCan(r.Context(), t, taskID, s.lootRowsForTask(r.Context(), t, fs), s.impactStats(r.Context(), t, taskID)), "asked": len(want), "filled": 0, "model": "rule",
		})
		return
	}

	got := parseBriefAbilities(raw)
	filled := 0
	for _, item := range got {
		f, ok := byID[item.FindingID]
		if !ok {
			continue // 模型编出来的 id 一律丢弃：不允许把不存在的漏洞写进缓存
		}
		text := strings.TrimSpace(item.Grants)
		if text == "" || len([]rune(text)) > 80 {
			continue
		}
		s.saveBriefAbility(taskID, f.id, briefAbilityHash(f), "done", text, "adviser")
		filled++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"can": s.briefCan(r.Context(), t, taskID, s.lootRowsForTask(r.Context(), t, fs), s.impactStats(r.Context(), t, taskID)), "asked": len(want), "filled": filled, "model": "adviser",
	})
}

// parseBriefAbilities 从模型输出里抠出 JSON 数组（有的模型会裹 Markdown 或前后废话）。
func parseBriefAbilities(raw string) []briefAbilityAI {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	var out []briefAbilityAI
	if json.Unmarshal([]byte(raw[start:end+1]), &out) != nil {
		return nil
	}
	return out
}
