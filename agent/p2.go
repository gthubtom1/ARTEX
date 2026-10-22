package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/access"
	"github.com/Autumn-27/artex/internal/adviser"
	"github.com/Autumn-27/artex/internal/capability"
	"github.com/Autumn-27/artex/internal/creds"
	"github.com/Autumn-27/artex/internal/dbiface"
	"github.com/Autumn-27/artex/internal/decision"
	"github.com/Autumn-27/artex/internal/gatechain"
	"github.com/Autumn-27/artex/internal/lateral"
	"github.com/Autumn-27/artex/internal/ledger"
	"github.com/Autumn-27/artex/internal/privesc"
	"github.com/Autumn-27/artex/internal/stall"
	"github.com/Autumn-27/artex/internal/verify"
	actool "github.com/Autumn-27/norma/tool"
)

// P2 是 P2~P5 的装配包：GateChain / 停顿检测+顾问 / 能力卡 / 三门校验 /
// 凭据库 / 立足点池 / 持有台账。所有 store 都是全局的（按 task_id / owned_id 作用域），
// 由 server 在启动时构造一次并注入每个 ToolSet。
type P2 struct {
	DB     dbiface.DB
	Creds  *creds.Store
	Pool   *access.Pool
	Ledger *ledger.Store
	Tax    *capability.Taxonomy
}

// NewP2 构造装配包；db 为 nil 时各 store 走内存实现（无库可用但不崩）。
func NewP2(dbConn dbiface.DB, credKey string) (*P2, error) {
	v, err := creds.NewVault(credKey)
	if err != nil {
		v = creds.NewEphemeralVault()
	}
	return &P2{
		DB:     dbConn,
		Creds:  creds.NewStore(dbConn, v),
		Pool:   access.NewPool(0),
		Ledger: ledger.NewStore(dbConn, 24, 7),
		Tax:    capability.Default(),
	}, nil
}

// SetP2 注入装配包（nil = P2 工具不注册）。
func (t *ToolSet) SetP2(p *P2) { t.p2 = p }

// P2PlannerTools 是规划者可见的 P2 工具（只读为主 + 诊断）。
func (t *ToolSet) P2PlannerTools() []actool.CoreTool {
	if t.p2 == nil {
		return nil
	}
	return []actool.CoreTool{
		t.rankIntents(), t.diagnoseProgress(), t.listCapabilities(), t.verifyClaimTool(),
		t.listCreds(), t.listSessions(), t.listOwned(), t.dueOwned(), t.exportOwned(),
		t.dataLocate(), t.dataMask(), t.credHarvest(), t.credStaleScan(),
	}
}

// P2MainTools 是人机界面（主 agent）可见的 P2 工具：看菜单 + 做裁决。
func (t *ToolSet) P2MainTools() []actool.CoreTool {
	if t.p2 == nil {
		return nil
	}
	out := []actool.CoreTool{
		t.listCapabilities(), t.decideCapability(), t.diagnoseProgress(),
		t.listOwned(), t.dueOwned(), t.exportOwned(), t.listCreds(), t.listSessions(),
	}
	out = append(out, t.P2OpsTools()...)
	out = append(out, t.sessionExec(), t.credReuseRun(), t.routeResolve())
	return out
}

// P2WorkerTools 是执行者可见的 P2 工具（含写回）。
func (t *ToolSet) P2WorkerTools() []actool.CoreTool {
	if t.p2 == nil {
		return nil
	}
	out := []actool.CoreTool{
		t.recordCapability(), t.listCapabilities(), t.verifyClaimTool(),
		t.addCred(), t.listCreds(), t.credReusePlan(), t.verifyCred(),
		t.registerSession(), t.leaseSession(), t.listSessions(), t.releaseSession(),
		t.registerOwned(), t.listOwned(), t.checkOwned(), t.statusOwned(), t.cleanupOwned(),
		t.privescPlan(), t.lateralPlan(), t.routePlan(),
		t.dataLocate(), t.dataMask(), t.credHarvest(), t.credStaleScan(),
	}
	// L3/L4 执行面：提权 / 横移 / 战后清理（在会话上执行）+ 会话命令 + 凭据复用执行。
	out = append(out, t.P2OpsTools()...)
	out = append(out, t.sessionExec(), t.credReuseRun(), t.routeResolve())
	return out
}

// ───────────────────────── GateChain ─────────────────────────

func (t *ToolSet) rankIntents() actool.CoreTool {
	return readTool("rank_intents",
		"【意图闸门】把准备下发的多条探索方向做去重+优先级排序（不拒绝任何方向：重复的合并到代表意图）。"+
			"在 add_intent 之前调用一次，可避免同一入口同一手法重复下发；返回顺序就是建议下发顺序。",
		obj(map[string]any{
			"intents": map[string]any{"type": "array", "description": "候选方向数组", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":        map[string]any{"type": "integer"},
					"summary":   str("一句话描述"),
					"asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
					"stage":     str("recon|access|foothold|privesc|lateral|data"),
					"attempts":  map[string]any{"type": "integer", "description": "同方向已尝试次数"},
					"yields":    map[string]any{"type": "integer", "description": "同方向已产出 fact 数"},
				},
			}},
			"layer_gap":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"untested_assets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"hint_keywords":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "intents"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Intents        []gatechain.Intent `json:"intents"`
				LayerGap       []string           `json:"layer_gap"`
				UntestedAssets []string           `json:"untested_assets"`
				HintKeywords   []string           `json:"hint_keywords"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			ranked := gatechain.Rank(a.Intents, gatechain.Options{
				LayerGap: a.LayerGap, UntestedAssets: a.UntestedAssets, HintKeywords: a.HintKeywords,
			})
			return jsonResult(map[string]any{"ranked": ranked, "dropped_as_duplicate": len(a.Intents) - len(ranked)})
		})
}

// ───────────────────────── 停顿检测 + 顾问 ─────────────────────────

func (t *ToolSet) diagnoseProgress() actool.CoreTool {
	return t.readExpTool("diagnose_progress",
		"【自检/纠偏】用确定性检测器算一遍当前进度（连续无产出轮数/重复度/层级缺口/预算燃烧），"+
			"并由顾问给出可执行的换层建议。apply=true 时把建议作为 hint 写进图（会唤醒规划者）。"+
			"跑了几轮感觉在原地打转时先调它。",
		obj(map[string]any{
			"apply": map[string]any{"type": "boolean", "description": "true=把建议写入 hints（默认 false 只看）"},
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Apply bool `json:"apply"`
			}
			_ = json.Unmarshal(in, &a)
			rep, led, neg, err := t.buildStall(ctx)
			if err != nil {
				return actool.Errorf("诊断失败：" + err.Error()), nil
			}
			d := adviser.Diagnose(rep, led, neg)
			out := map[string]any{"stall": rep, "diagnosis": d}
			if a.Apply && len(d.Hints) > 0 {
				n := 0
				for _, h := range d.Hints {
					_, err := t.ts.AddNode(db.KindHint, map[string]any{"text": h}, 0, "active", "adviser", nil)
					if err == nil {
						n++
					}
				}
				out["hints_written"] = n
				if t.notify != nil && n > 0 {
					t.notify()
				}
			}
			return jsonResult(out)
		})
}

// StallBundle 是诊断输入的导出入口（server 的 LLM 顾问复用同一份口径）。
func (t *ToolSet) StallBundle(ctx context.Context) (stall.Report, adviser.Ledger, adviser.NegativeSpace, error) {
	return t.buildStall(ctx)
}

// buildStall 从探索图/资产图聚合检测器输入（近似口径见各字段注释）。
func (t *ToolSet) buildStall(ctx context.Context) (stall.Report, adviser.Ledger, adviser.NegativeSpace, error) {
	var rep stall.Report
	var led adviser.Ledger
	var neg adviser.NegativeSpace
	if t.ts == nil {
		return rep, led, neg, fmt.Errorf("需要任务上下文")
	}
	intents, err := t.ts.ListByKind(db.KindIntent, 200)
	if err != nil {
		return rep, led, neg, err
	}
	facts, _ := t.ts.ListByKind(db.KindFact, 500)
	findings, _ := t.ts.ListByKind(db.KindFinding, 200)

	// 按时间升序（ListByKind 返回新→旧）。
	reverseNodes(intents)
	reverseNodes(facts)

	layerFacts := map[string]int{"host": 0, "service": 0, "endpoint": 0, "data": 0, "auth": 0}
	for _, f := range facts {
		layerFacts[factLayer(f)]++
	}

	var refs []stall.IntentRef
	yieldByAnchor := map[string]int{}
	attemptByAnchor := map[string]int{}
	for _, it := range intents {
		var p map[string]any
		_ = json.Unmarshal(it.Payload, &p)
		summary, _ := p["summary"].(string)
		anchors := it.Anchors
		ys, _ := t.ts.FactsYielded(it.ID)
		key := anchorKey(anchors)
		attemptByAnchor[key]++
		yieldByAnchor[key] += len(ys)
		ref := stall.IntentRef{ID: it.ID, Summary: summary, AssetIDs: anchors, Stage: stageOf(summary),
			Yields: len(ys), Attempts: attemptByAnchor[key], State: it.State}
		ref.AssetTypes = t.assetTypes(anchors)
		refs = append(refs, ref)
	}

	// rounds：用每条意图的产出近似一轮（每个意图≈一次规划派发）。
	var rounds []stall.RoundStat
	for _, r := range refs {
		rounds = append(rounds, stall.RoundStat{NewFacts: r.Yields})
	}

	in := stall.Input{
		Rounds: rounds, Intents: refs, LayerFacts: layerFacts,
		AssetsInScope: 0, AssetsTested: 0,
	}
	if t.as != nil && t.taskID > 0 {
		if counts, err := t.as.CountsByTypeForTask(t.taskID); err == nil {
			for _, n := range counts {
				in.AssetsInScope += n
			}
		}
	}
	tested := map[int64]bool{}
	for _, it := range intents {
		for _, id := range it.Anchors {
			tested[id] = true
		}
	}
	in.AssetsTested = len(tested)

	rep = stall.Detect(in, stall.Thresholds{})

	// 顾问账本
	led = adviser.Ledger{Goal: t.goal, Rounds: len(refs), IntentCount: len(refs), NewFacts: len(facts), Findings: len(findings)}
	for _, r := range refs {
		led.Directions = append(led.Directions, adviser.Direction{IntentID: r.ID, Summary: r.Summary,
			Facts: r.Yields, Attempts: r.Attempts})
	}

	// 负空间：范围内、但从未被任何意图锚定的资产。
	if t.as != nil && t.taskID > 0 {
		assets, _ := t.as.QueryByTask(t.taskID, "", 200, 0)
		for _, a := range assets {
			if a == nil || tested[a.ID] {
				continue
			}
			host := firstNonEmpty(a.Domain, a.IP, a.URL)
			if host == "" {
				continue
			}
			if a.Port != nil && *a.Port > 0 {
				neg.UntestedHosts = append(neg.UntestedHosts, fmt.Sprintf("%s:%d", host, *a.Port))
			} else if a.URL != "" {
				neg.UntestedURLs = append(neg.UntestedURLs, a.URL)
			}
		}
		sort.Strings(neg.UntestedHosts)
		if len(neg.UntestedHosts) > 8 {
			neg.UntestedHosts = neg.UntestedHosts[:8]
		}
		if len(neg.UntestedURLs) > 8 {
			neg.UntestedURLs = neg.UntestedURLs[:8]
		}
	}
	return rep, led, neg, nil
}

func reverseNodes(ns []*db.Node) {
	for i, j := 0, len(ns)-1; i < j; i, j = i+1, j-1 {
		ns[i], ns[j] = ns[j], ns[i]
	}
}

func anchorKey(ids []int64) string {
	cp := append([]int64{}, ids...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	var b strings.Builder
	for _, id := range cp {
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteByte(',')
	}
	return b.String()
}

// stageOf 从摘要里猜阶段（只用于排序权重，不影响动作）。
func stageOf(summary string) string {
	l := strings.ToLower(summary)
	switch {
	case strings.Contains(l, "表结构") || strings.Contains(l, "导出") || strings.Contains(l, "count(") || strings.Contains(l, "数据"):
		return "data"
	case strings.Contains(l, "横向") || strings.Contains(l, "内网") || strings.Contains(l, "pivot"):
		return "lateral"
	case strings.Contains(l, "提权") || strings.Contains(l, "sudo") || strings.Contains(l, "suid"):
		return "privesc"
	case strings.Contains(l, "shell") || strings.Contains(l, "rce") || strings.Contains(l, "命令执行"):
		return "foothold"
	case strings.Contains(l, "登录") || strings.Contains(l, "后台") || strings.Contains(l, "凭据") || strings.Contains(l, "口令"):
		return "access"
	}
	return "endpoint"
}

// factLayer 按内容把 fact 归到五层之一（用于层级缺口检测）。
func factLayer(n *db.Node) string {
	if n == nil {
		return "endpoint"
	}
	var p map[string]any
	_ = json.Unmarshal(n.Payload, &p)
	txt := ""
	for _, k := range []string{"text", "summary", "fact", "content", "detail"} {
		if s, ok := p[k].(string); ok {
			txt += " " + s
		}
	}
	txt = strings.ToLower(txt)
	switch {
	case strings.Contains(txt, "information_schema") || strings.Contains(txt, "count(") ||
		strings.Contains(txt, "表结构") || strings.Contains(txt, "行数") || strings.Contains(txt, "数据库"):
		return "data"
	case strings.Contains(txt, "登录成功") || strings.Contains(txt, "token") || strings.Contains(txt, "cookie") ||
		strings.Contains(txt, "口令") || strings.Contains(txt, "凭据"):
		return "auth"
	case strings.Contains(txt, "sudo") || strings.Contains(txt, "suid") || strings.Contains(txt, "shell") ||
		strings.Contains(txt, "提权") || strings.Contains(txt, "uid="):
		return "host"
	case strings.Contains(txt, "端口") || strings.Contains(txt, "banner") || strings.Contains(txt, "服务版本") ||
		strings.Contains(txt, ":22") || strings.Contains(txt, ":3306") || strings.Contains(txt, ":6379"):
		return "service"
	}
	return "endpoint"
}

func (t *ToolSet) assetTypes(ids []int64) []string {
	if t.as == nil || len(ids) == 0 {
		return nil
	}
	assets, err := t.as.GetByIDs(ids)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range assets {
		if a == nil || seen[a.Type] {
			continue
		}
		seen[a.Type] = true
		out = append(out, a.Type)
	}
	return out
}

// ───────────────────────── 能力卡 ─────────────────────────

func (t *ToolSet) recordCapability() actool.CoreTool {
	return t.writeExpTool("record_capability",
		"【能力卡】把已经拿到的能力登记成用户能看懂的卡片（派生自证据，不另造真相）。"+
			"会自动过三门校验（证据/对照组/独立复现）：证据不足的卡标 inferred、猜测性证据直接 rejected 降为 P1。"+
			"item 用 kb/capabilities/taxonomy.md 的编号（如 1.2 越权 / 4.1 命令执行 / 2.1 明文口令）；留空则按 text 自动归类。",
		obj(map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"item":           str("清单编号，如 1.2"),
				"text":           str("能力描述（可自动归类）"),
				"surface":        str("入口：接口/URL/端口"),
				"evidence":       str("证据：请求行+响应片段 / 命令输出"),
				"proof_level":    str("P1..P4 / D1..D4"),
				"impact":         str("影响"),
				"risk":           str("代价/风险"),
				"next":           str("要继续的话做什么"),
				"node_ids":       map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
				"control_tested": map[string]any{"type": "boolean"},
				"independent":    map[string]any{"type": "boolean"},
				"retest_ok":      map[string]any{"type": "boolean"},
				"tool_verified":  map[string]any{"type": "boolean"},
			}}},
		}, "items"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Items []struct {
					Item          string  `json:"item"`
					Text          string  `json:"text"`
					Surface       string  `json:"surface"`
					Evidence      string  `json:"evidence"`
					ProofLevel    string  `json:"proof_level"`
					Impact        string  `json:"impact"`
					Risk          string  `json:"risk"`
					Next          string  `json:"next"`
					NodeIDs       []int64 `json:"node_ids"`
					ControlTested bool    `json:"control_tested"`
					Independent   bool    `json:"independent"`
					RetestOK      bool    `json:"retest_ok"`
					ToolVerified  bool    `json:"tool_verified"`
				} `json:"items"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if len(a.Items) == 0 {
				return actool.Errorf("items 不能为空"), nil
			}
			var cards []capability.Card
			var verdicts []verify.Verdict
			for _, it := range a.Items {
				v := verify.Check(verify.Claim{Title: firstNonEmpty(it.Item, it.Text), Surface: it.Surface,
					Evidence: it.Evidence, ProofLevel: it.ProofLevel}, verify.Options{
					ControlTested: it.ControlTested, RetestOK: it.RetestOK, Independent: it.Independent, ToolVerified: it.ToolVerified,
				})
				verdicts = append(verdicts, v)
				conf := "observed"
				if v.Level != "verified" {
					conf = "inferred"
				}
				cards = append(cards, capability.Card{
					Group: groupOf(it.Item), Item: it.Item, Surface: it.Surface, Evidence: it.Evidence,
					ProofLevel: v.ProofLevel, Confidence: conf, Impact: it.Impact, Risk: it.Risk,
					NextIfApproved: it.Next, SourceNodeIDs: it.NodeIDs, Status: "open",
				})
			}
			derived := capability.DeriveWithTaxonomy(t.p2.Tax, signalsFrom(cards))
			n, err := t.saveCapabilities(ctx, derived)
			if err != nil {
				return actool.Errorf("能力卡入库失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"saved": n, "cards": derived, "verdicts": verdicts,
				"plain": capability.RenderPlain(derived, nil)})
		})
}

func signalsFrom(cards []capability.Card) []capability.Signal {
	out := make([]capability.Signal, 0, len(cards))
	for _, c := range cards {
		out = append(out, capability.Signal{Item: c.Item, Text: c.Can, Surface: c.Surface, Evidence: c.Evidence,
			ProofLevel: c.ProofLevel, Confidence: c.Confidence, Impact: c.Impact, Risk: c.Risk,
			Next: c.NextIfApproved, NodeIDs: c.SourceNodeIDs})
	}
	return out
}

func groupOf(item string) int {
	if i := strings.Index(item, "."); i > 0 {
		if n, err := strconv.Atoi(item[:i]); err == nil {
			return n
		}
	}
	return 0
}

func (t *ToolSet) saveCapabilities(ctx context.Context, cards []capability.Card) (int, error) {
	if t.p2 == nil || t.p2.DB == nil {
		return 0, fmt.Errorf("需要 PostgreSQL（capabilities 表）")
	}
	n := 0
	for _, c := range cards {
		if c.Group == 0 || strings.TrimSpace(c.Item) == "" {
			continue
		}
		_, err := t.p2.DB.ExecContext(ctx,
			`INSERT INTO capabilities (task_id, grp, item, can, surface, evidence, proof_level, confidence,
			 impact, risk, next_if_approved, source_node_ids, status)
			 VALUES (NULLIF($1,0),$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::bigint[],'open')
			 ON CONFLICT (task_id, grp, item) DO UPDATE SET
			   can = EXCLUDED.can,
			   surface = CASE WHEN capabilities.surface = '' THEN EXCLUDED.surface
			                WHEN EXCLUDED.surface = '' OR capabilities.surface = EXCLUDED.surface THEN capabilities.surface
			                ELSE capabilities.surface || ' | ' || EXCLUDED.surface END,
			   evidence = CASE WHEN capabilities.evidence = '' THEN EXCLUDED.evidence
			                WHEN EXCLUDED.evidence = '' OR capabilities.evidence LIKE '%' || EXCLUDED.evidence || '%' THEN capabilities.evidence
			                ELSE capabilities.evidence || '；' || EXCLUDED.evidence END,
			   proof_level = CASE WHEN length(EXCLUDED.proof_level) > 0 AND EXCLUDED.proof_level > capabilities.proof_level
			                THEN EXCLUDED.proof_level ELSE capabilities.proof_level END,
			   confidence = CASE WHEN EXCLUDED.confidence = 'observed' THEN 'observed' ELSE capabilities.confidence END,
			   impact = COALESCE(NULLIF(EXCLUDED.impact,''), capabilities.impact),
			   risk = COALESCE(NULLIF(EXCLUDED.risk,''), capabilities.risk),
			   next_if_approved = COALESCE(NULLIF(EXCLUDED.next_if_approved,''), capabilities.next_if_approved),
			   source_node_ids = capabilities.source_node_ids || EXCLUDED.source_node_ids,
			   updated_at = now()`,
			t.taskID, c.Group, c.Item, c.Can, c.Surface, c.Evidence, c.ProofLevel, c.Confidence,
			c.Impact, c.Risk, c.NextIfApproved, int64ArrayLiteral(c.SourceNodeIDs))
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (t *ToolSet) listCapabilities() actool.CoreTool {
	return readTool("list_capabilities",
		"【能力卡】读本任务已登记的能力卡，并渲染成给零基础用户看的白话菜单（含 13 组覆盖度自检）。"+
			"给用户汇报\"这个站能做什么\"时用它，不要自己编。",
		obj(map[string]any{
			"plain": map[string]any{"type": "boolean", "description": "true=同时返回白话文本（默认 true）"},
			"negatives": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "负结论（做不到什么），一并渲染"},
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Plain     *bool    `json:"plain"`
				Negatives []string `json:"negatives"`
			}
			_ = json.Unmarshal(in, &a)
			cards, err := t.loadCapabilities(ctx)
			if err != nil {
				return actool.Errorf("读能力卡失败：" + err.Error()), nil
			}
			out := map[string]any{"cards": cards, "coverage": capability.Coverage(cards)}
			if a.Plain == nil || *a.Plain {
				out["plain"] = capability.RenderPlain(cards, a.Negatives)
			}
			return jsonResult(out)
		})
}

func (t *ToolSet) loadCapabilities(ctx context.Context) ([]capability.Card, error) {
	if t.p2 == nil || t.p2.DB == nil {
		return nil, fmt.Errorf("需要 PostgreSQL")
	}
	rows, err := t.p2.DB.QueryContext(ctx,
		`SELECT id, COALESCE(task_id,0), grp, item, can, surface, evidence, proof_level, confidence,
		 impact, risk, next_if_approved, COALESCE(array_to_json(source_node_ids)::text,'[]'), status
		 FROM capabilities WHERE ($1 = 0 OR task_id = $1) ORDER BY grp, item`, t.taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []capability.Card
	for rows.Next() {
		var c capability.Card
		var nodeIDsJSON string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Group, &c.Item, &c.Can, &c.Surface, &c.Evidence,
			&c.ProofLevel, &c.Confidence, &c.Impact, &c.Risk, &c.NextIfApproved, &nodeIDsJSON, &c.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(nodeIDsJSON), &c.SourceNodeIDs)
		if tx, ok := t.p2.Tax.Get(c.Item); ok {
			c.Plain, c.Value = tx.Plain, tx.Value
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *ToolSet) verifyClaimTool() actool.CoreTool {
	return readTool("verify_claim",
		"【三门校验】一条结论进报告前先过三道关：证据在不在（grounding）、有没有对照组（negation）、"+
			"能不能独立复现（interrogation）。返回 verified / inferred / rejected 与建议的证明级别。"+
			"报 finding 前先调它，避免把猜测当战果。",
		obj(map[string]any{
			"title":          str("结论一句话"),
			"surface":        str("入口：接口/URL/端口"),
			"evidence":       str("证据文本：请求行+响应片段 / 命令输出"),
			"proof_level":    str("P1..P4 / D1..D4（默认 P2）"),
			"control_tested": map[string]any{"type": "boolean", "description": "是否做过对照组"},
			"independent":    map[string]any{"type": "boolean", "description": "是否用独立路径复现"},
			"retest_ok":      map[string]any{"type": "boolean", "description": "复现门是否通过"},
			"tool_verified":  map[string]any{"type": "boolean", "description": "是否有工具级证据"},
		}, "title", "evidence"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Title         string `json:"title"`
				Surface       string `json:"surface"`
				Evidence      string `json:"evidence"`
				ProofLevel    string `json:"proof_level"`
				ControlTested bool   `json:"control_tested"`
				Independent   bool   `json:"independent"`
				RetestOK      bool   `json:"retest_ok"`
				ToolVerified  bool   `json:"tool_verified"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			v := verify.Check(verify.Claim{Title: a.Title, Surface: a.Surface, Evidence: a.Evidence, ProofLevel: a.ProofLevel},
				verify.Options{ControlTested: a.ControlTested, Independent: a.Independent, RetestOK: a.RetestOK, ToolVerified: a.ToolVerified})
			return jsonResult(map[string]any{"verdict": v, "summary": v.Describe(a.Title)})
		})
}

// ───────────────────────── 凭据 ─────────────────────────

func (t *ToolSet) addCred() actool.CoreTool {
	return writeTool("cred_add",
		"【凭据库】把收到的凭据存进加密保险库（AES-GCM，报告/日志永不出现明文）。同秘密不重复入库。"+
			"返回 cred_id；后续用 cred_reuse_plan 算能试哪些服务、用 session_register 挂钩立足点。",
		obj(map[string]any{
			"username": str("用户名（可为空）"),
			"kind":     str("password|hash|token|key|ssh_key|conn_str"),
			"secret":   str("秘密本体（只进加密库）"),
			"source":   str("来源：config|history|browser|cloud|manual"),
			"asset_id": map[string]any{"type": "integer", "description": "关联资产 id（可选）"},
		}, "secret"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Username string `json:"username"`
				Kind     string `json:"kind"`
				Secret   string `json:"secret"`
				Source   string `json:"source"`
				AssetID  int64  `json:"asset_id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			id, existing, err := t.p2.Creds.Add(ctx, creds.Entry{TaskID: t.taskID, AssetID: a.AssetID,
				Kind: a.Kind, Username: a.Username, Source: a.Source}, a.Secret)
			if err != nil {
				return actool.Errorf("入库失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"cred_id": id, "existing": existing})
		})
}

func (t *ToolSet) listCreds() actool.CoreTool {
	return readTool("cred_list",
		"【凭据库】列出本任务的凭据元数据（**不含明文**）。看有哪些可用凭据、验证状态、用在哪。",
		obj(map[string]any{"limit": intp("最多返回条数（默认 50）")}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Limit int `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 50
			}
			list, err := t.p2.Creds.List(ctx, t.taskID, a.Limit)
			if err != nil {
				return actool.Errorf("读取失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"creds": list})
		})
}

func (t *ToolSet) credReusePlan() actool.CoreTool {
	return readTool("cred_reuse_plan",
		"【凭据复用】给定一条凭据与目标主机（可带已扫描开放端口），算出能试哪些服务（ssh/mysql/smb/winrm/redis…）"+
			"与可直接执行的命令模板（占位 ${USER}/${SECRET}，不含明文）。",
		obj(map[string]any{
			"cred_id": idp("凭据 id"),
			"host":    str("目标 host/ip"),
			"ports":   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "开放端口（空=全量清单）"},
		}, "cred_id", "host"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				CredID int64  `json:"cred_id"`
				Host   string `json:"host"`
				Ports  []int  `json:"ports"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			e, _, err := t.p2.Creds.Get(ctx, a.CredID)
			if err != nil {
				return actool.Errorf("凭据不存在：" + err.Error()), nil
			}
			plan := creds.PlanReuse(creds.Target{Host: a.Host}, e, a.Ports)
			return jsonResult(map[string]any{"attempts": plan, "cred": map[string]any{"id": e.ID, "kind": e.Kind, "username": e.Username}})
		})
}

func (t *ToolSet) verifyCred() actool.CoreTool {
	return writeTool("cred_verify",
		"【凭据库】更新凭据验证状态（ok=true → verified；false → invalid）。验证成功后记得 cred_reuse_plan。",
		obj(map[string]any{"cred_id": idp("凭据 id"), "ok": map[string]any{"type": "boolean"}}, "cred_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				CredID int64 `json:"cred_id"`
				OK     bool  `json:"ok"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if err := t.p2.Creds.SetVerified(ctx, a.CredID, a.OK); err != nil {
				return actool.Errorf("更新失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"ok": true})
		})
}

// ───────────────────────── 立足点池 ─────────────────────────

func (t *ToolSet) registerSession() actool.CoreTool {
	return writeTool("session_register",
		"【立足点池】登记一个可复用的会话（shell/webshell/ssh/admin/db），写入后**所有 worker 都能租用**。"+
			"打下 shell 后立刻登记，别让别人重打一遍。",
		obj(map[string]any{
			"owned_id":  idp("台账 id（可先用 owned_register 登记）"),
			"proto":     str("shell|webshell|ssh|vpn|jump|admin|db"),
			"endpoint":  str("host:port 或 URL"),
			"cred_ref":  idp("关联凭据 id（可选）"),
			"host":      str("主机（可省略，从 endpoint 推断）"),
			"port":      intp("端口"),
			"user":      str("登录用户（ssh/db 用；可省略，取凭据用户名）"),
			"cmd_param": str("webshell 参数名（默认 cmd）"),
			"notes":     str("备注（怎么用、有什么限制）"),
		}, "proto", "endpoint"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID  int64  `json:"owned_id"`
				Proto    string `json:"proto"`
				Endpoint string `json:"endpoint"`
				CredRef  int64  `json:"cred_ref"`
				Host     string `json:"host"`
				Port     int    `json:"port"`
				User     string `json:"user"`
				CmdParam string `json:"cmd_param"`
				Notes    string `json:"notes"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			ses := access.Session{OwnedID: a.OwnedID, Proto: a.Proto, Endpoint: a.Endpoint,
				Host: a.Host, Port: a.Port, User: a.User, CmdParam: a.CmdParam,
				CredRef: a.CredRef, Notes: a.Notes}
			id, err := t.p2.Pool.Register(ses)
			if err != nil {
				return actool.Errorf("登记失败：" + err.Error()), nil
			}
			ses.ID = id
			perr := t.p2.PersistSession(ctx, ses)
			out := map[string]any{"session_id": id}
			if perr != nil {
				out["persist_error"] = perr.Error()
			}
			return jsonResult(out)
		})
}

func (t *ToolSet) leaseSession() actool.CoreTool {
	return writeTool("session_lease",
		"【立足点池】租用一个已登记会话（默认挑当前 worker 能用的、用得最少的）。"+
			"租到后按 endpoint 直接连接；用不上了用 session_release 还回去。",
		obj(map[string]any{
			"owned_id": idp("只租某台主机的会话（0/省略=任意）"),
			"worker":   str("租用人标识（默认当前 worker）"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID int64  `json:"owned_id"`
				Worker  string `json:"worker"`
			}
			_ = json.Unmarshal(in, &a)
			w := firstNonEmpty(a.Worker, t.worker, "worker")
			s, err := t.p2.Pool.Lease(w, a.OwnedID)
			if err != nil {
				return actool.Errorf("没有可用会话：" + err.Error()), nil
			}
			var credHint any
			if s.CredRef > 0 {
				credHint = map[string]any{"cred_ref": s.CredRef, "note": "用 cred_reuse_plan 取命令模板"}
			}
			return jsonResult(map[string]any{"session": s, "cred": credHint})
		})
}

func (t *ToolSet) listSessions() actool.CoreTool {
	return readTool("session_list", "【立足点池】列出已登记的会话与租用状态（跨 worker 共享）。",
		obj(map[string]any{"owned_id": idp("只看某台主机（0/省略=全部）")}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID int64 `json:"owned_id"`
			}
			_ = json.Unmarshal(in, &a)
			return jsonResult(map[string]any{"sessions": t.p2.Pool.List(a.OwnedID)})
		})
}

func (t *ToolSet) releaseSession() actool.CoreTool {
	return writeTool("session_release", "【立足点池】释放会话租约（会话保持可用，别人可接手）。",
		obj(map[string]any{"session_id": idp("会话 id")}, "session_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64 `json:"session_id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			t.p2.Pool.Release(a.SessionID)
			return jsonResult(map[string]any{"ok": true})
		})
}

// ───────────────────────── 持有台账 ─────────────────────────

func (t *ToolSet) registerOwned() actool.CoreTool {
	return writeTool("owned_register",
		"【持有台账】把打下来的资产登记进全局台账（跨任务同一条）。登记后系统会按周期复查，"+
			"改密/关站会自动标 stale，长期持有不丢。",
		obj(map[string]any{
			"host":        str("域名/host（优先）"),
			"ip":          str("IP"),
			"port":        intp("端口"),
			"access_type": str("shell|webshell|ssh|vpn|jump|admin|db"),
			"privilege":   str("user|root|admin|system"),
			"via_owned":   idp("经哪台跳板拿下（台账 id，可选）"),
			"depth":       intp("跳数（可选）"),
			"asset_id":    idp("关联 assets.id（可选）"),
			"evidence":    str("一句话证据"),
		}, "access_type"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Host       string `json:"host"`
				IP         string `json:"ip"`
				Port       int    `json:"port"`
				AccessType string `json:"access_type"`
				Privilege  string `json:"privilege"`
				ViaOwned   int64  `json:"via_owned"`
				Depth      int    `json:"depth"`
				AssetID    int64  `json:"asset_id"`
				Evidence   string `json:"evidence"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if a.Privilege == "" {
				a.Privilege = "user"
			}
			id, created, err := t.p2.Ledger.Own(ctx, ledger.Owned{Host: a.Host, IP: a.IP, Port: a.Port,
				AccessType: a.AccessType, Privilege: a.Privilege, ViaAsset: a.ViaOwned, Depth: a.Depth,
				AssetID: a.AssetID, Evidence: a.Evidence, FirstTaskID: t.taskID})
			if err != nil {
				return actool.Errorf("登记失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"owned_id": id, "created": created})
		})
}

func (t *ToolSet) listOwned() actool.CoreTool {
	return readTool("owned_list", "【持有台账】列出打下来的资产（按状态过滤：owned/verified/stale/lost/cleaned）。",
		obj(map[string]any{"status": str("状态过滤（空=全部）"), "limit": intp("最多条数")}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Status string `json:"status"`
				Limit  int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			list, err := t.p2.Ledger.List(ctx, a.Status, a.Limit)
			if err != nil {
				return actool.Errorf("读取失败：" + err.Error()), nil
			}
			stats, _ := t.p2.Ledger.Stats(ctx)
			return jsonResult(map[string]any{"assets": list, "stats": stats})
		})
}

func (t *ToolSet) dueOwned() actool.CoreTool {
	return readTool("owned_due",
		"【持有台账】列出到期该复查的资产，并给出每条该跑的复查命令（nc/curl，只读低成本）。",
		obj(map[string]any{"limit": intp("最多条数")}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Limit int `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 20
			}
			due, err := t.p2.Ledger.Due(ctx, nowUTC(), a.Limit)
			if err != nil {
				return actool.Errorf("读取失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"due": due, "checks": ledger.PlanChecks(due)})
		})
}

func (t *ToolSet) checkOwned() actool.CoreTool {
	return writeTool("owned_check", "【持有台账】写回一次复查结果：ok → verified；失败 → stale（改密/关站会被识别出来）。",
		obj(map[string]any{
			"owned_id":   idp("台账 id"),
			"kind":       str("http|ssh|port|login|webshell"),
			"ok":         map[string]any{"type": "boolean"},
			"latency_ms": intp("延迟毫秒（可选）"),
			"changed":    map[string]any{"type": "boolean", "description": "行为/内容是否变了"},
			"detail":     str("细节（状态码/报错）"),
		}, "owned_id", "kind"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID   int64  `json:"owned_id"`
				Kind      string `json:"kind"`
				OK        bool   `json:"ok"`
				LatencyMS int    `json:"latency_ms"`
				Changed   bool   `json:"changed"`
				Detail    string `json:"detail"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if err := t.p2.Ledger.RecordCheck(ctx, ledger.Check{OwnedID: a.OwnedID, Kind: a.Kind,
				OK: a.OK, LatencyMS: a.LatencyMS, Changed: a.Changed, Detail: a.Detail}); err != nil {
				return actool.Errorf("写回失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"ok": true})
		})
}

func (t *ToolSet) statusOwned() actool.CoreTool {
	return writeTool("owned_status", "【持有台账】直接改持有状态（lost=丢了；verified=已确认；owned=刚拿下）。",
		obj(map[string]any{"owned_id": idp("台账 id"), "status": str("owned|verified|stale|lost|cleaned"), "notes": str("备注")},
			"owned_id", "status"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID int64  `json:"owned_id"`
				Status  string `json:"status"`
				Notes   string `json:"notes"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if err := t.p2.Ledger.SetStatus(ctx, a.OwnedID, a.Status, a.Notes); err != nil {
				return actool.Errorf("更新失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"ok": true})
		})
}

func (t *ToolSet) cleanupOwned() actool.CoreTool {
	return writeTool("owned_cleanup",
		"【战后清理】把一条持有记录标 cleaned，并记下清理项（删了哪个 webshell/临时文件/账号）。"+
			"清理完 session 用 session_release 释放；台账里会永久留痕。",
		obj(map[string]any{"owned_id": idp("台账 id"), "detail": str("清理项：删了哪些 webshell/文件/账号")},
			"owned_id", "detail"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OwnedID int64  `json:"owned_id"`
				Detail  string `json:"detail"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if err := t.p2.Ledger.Cleanup(ctx, a.OwnedID, a.Detail); err != nil {
				return actool.Errorf("清理失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"ok": true})
		})
}

func (t *ToolSet) exportOwned() actool.CoreTool {
	return readTool("owned_export", "【持有台账】导出整本台账 JSON（报告/前端用）。",
		obj(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (actool.Result, error) {
			b, err := t.p2.Ledger.ExportJSON(ctx)
			if err != nil {
				return actool.Errorf("导出失败：" + err.Error()), nil
			}
			return actool.Text(string(b)), nil
		})
}

// ───────────────────────── 提权 / 横移 / 路由 ─────────────────────────

func (t *ToolSet) privescPlan() actool.CoreTool {
	return readTool("privesc_plan",
		"【提权决策】把枚举输出（LinPEAS/WinPEAS/whoami /priv 等）解析成 3 条可执行提权路径，"+
			"按成功率排序（内核漏洞永远排最后）。",
		obj(map[string]any{
			"os":     str("linux|windows"),
			"output": str("枚举工具输出原文（可截断）"),
			"limit":  intp("最多路径数（默认 3）"),
		}, "output"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				OS     string `json:"os"`
				Output string `json:"output"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			var fs []privesc.Finding
			if strings.Contains(strings.ToLower(a.OS), "win") {
				fs = privesc.FromWindows(a.Output)
			} else {
				fs = privesc.FromLinPEAS(a.Output)
			}
			return jsonResult(map[string]any{"findings": fs, "paths": privesc.Plan(fs, a.OS, a.Limit)})
		})
}

func (t *ToolSet) lateralPlan() actool.CoreTool {
	return readTool("lateral_plan",
		"【横移】给定目标与开放端口，给出可执行的横移方案（ssh/smb/winrm/mssql/mysql/redis/rdp，按可用性排序）。",
		obj(map[string]any{
			"addr":  str("目标 ip/host"),
			"ports": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			"os":    str("windows|linux（可选）"),
		}, "addr"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Addr  string `json:"addr"`
				Ports []int  `json:"ports"`
				OS    string `json:"os"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"plans": lateral.Plans(lateral.Host{Addr: a.Addr, Ports: a.Ports, OS: a.OS})})
		})
}

func (t *ToolSet) routePlan() actool.CoreTool {
	return readTool("route_plan",
		"【路由】为内网网段选择跳板（跳数最少优先），返回 routes 行（cidr/via/proxy/hops）。",
		obj(map[string]any{
			"targets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "目标网段/IP"},
			"pivots": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"host":  str("跳板 host"),
				"cidrs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"proxy": str("socks5://…（可选）"),
			}}},
		}, "targets"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Targets []string        `json:"targets"`
				Pivots  []lateral.Pivot `json:"pivots"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			return jsonResult(map[string]any{"routes": lateral.PlanRoutes(a.Targets, a.Pivots)})
		})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func nowUTC() time.Time { return time.Now().UTC() }

// ───────────────────────── 引擎/服务侧入口 ─────────────────────────

// DiagnoseProgress 是 diagnose_progress 工具的编程入口（引擎每轮规划后调用）。
// apply=true 时把顾问建议写进 hints 并唤醒规划者。
func (t *ToolSet) DiagnoseProgress(ctx context.Context, apply bool) (stall.Report, adviser.Diagnosis, int, error) {
	rep, led, neg, err := t.buildStall(ctx)
	if err != nil {
		return rep, adviser.Diagnosis{}, 0, err
	}
	d := adviser.Diagnose(rep, led, neg)
	written := 0
	if apply && len(d.Hints) > 0 && t.ts != nil {
		// 去重：同一段建议不重复写（顾问每轮都算，文本相同就不刷屏）。
		seen := map[string]bool{}
		if hints, err := t.ts.ListByKind(db.KindHint, 50); err == nil {
			for _, h := range hints {
				var p map[string]any
				if json.Unmarshal(h.Payload, &p) == nil {
					if s, ok := p["text"].(string); ok {
						seen[s] = true
					}
				}
			}
		}
		for _, h := range d.Hints {
			if seen[h] {
				continue
			}
			if _, err := t.ts.AddNode(db.KindHint, map[string]any{"text": h}, 0, "active", "adviser", nil); err == nil {
				written++
				seen[h] = true
			}
		}
	}
	return rep, d, written, nil
}

// SyncCapabilitiesFromFindings 把本任务已登记的 finding 自动派生为能力卡
// （幂等：同一 item 已存在时走合并，不会重复插入）。
// 这是"能力卡派生自 finding、不独立维护真相"（ADR A8）的落地。
func (t *ToolSet) SyncCapabilities(ctx context.Context) (int, error) {
	if t.p2 == nil || t.ts == nil {
		return 0, fmt.Errorf("P2 未装配")
	}
	findings, err := t.ts.ListByKind(db.KindFinding, 100)
	if err != nil {
		return 0, err
	}
	if len(findings) == 0 {
		return 0, nil
	}
	// 已有哪些 item（避免每轮重复计算/重复写）。
	existing := map[string]bool{}
	if t.p2.DB != nil && t.taskID > 0 {
		rows, err := t.p2.DB.QueryContext(ctx, `SELECT item FROM capabilities WHERE task_id = $1`, t.taskID)
		if err == nil {
			for rows.Next() {
				var item string
				if rows.Scan(&item) == nil {
					existing[item] = true
				}
			}
			rows.Close()
		}
	}
	var sigs []capability.Signal
	for _, f := range findings {
		var p map[string]any
		_ = json.Unmarshal(f.Payload, &p)
		text := strings.TrimSpace(strOf(p["name"]) + " " + strOf(p["summary"]) + " " + strOf(p["vulnclass"]))
		item, ok := capability.Classify(text)
		if !ok || existing[item] {
			continue
		}
		evidence := strOf(p["evidence"])
		proof := "P1"
		if hasResponseMarker(evidence) {
			proof = "P2"
		}
		sigs = append(sigs, capability.Signal{
			Item: item, Text: text, Evidence: evidence, ProofLevel: proof, Confidence: "inferred",
			Surface: strOf(p["surface"]), NodeIDs: []int64{f.ID},
			Risk: "证据尚未独立复现（三门校验 interrogation 未过），先按推断登记",
			Next: "用另一条独立路径复现一次，再用 record_capability 升级证明级别",
		})
	}
	if len(sigs) == 0 {
		return 0, nil
	}
	cards := capability.DeriveWithTaxonomy(t.p2.Tax, sigs)
	return t.saveCapabilities(ctx, cards)
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func hasResponseMarker(evidence string) bool {
	l := strings.ToLower(evidence)
	for _, m := range []string{"200", "403", "301", "302", "root:", "uid=", "http/1.", "响应", "返回"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// DefaultP2 是服务启动时装配的全局 P2 包；NewToolSet 自动带上它。
// 与 prompt override 的全局钩子同一套风格：进程内只有一份，测试可置 nil。
var DefaultP2 *P2

// SetDefaultP2 由 server 在启动时调用一次。
func SetDefaultP2(p *P2) { DefaultP2 = p }

// AddHintOnce 写一条 hint，若同样的文本已在最近 50 条 hints 里则跳过。
func (t *ToolSet) AddHintOnce(ctx context.Context, text string) (bool, error) {
	if t.ts == nil || strings.TrimSpace(text) == "" {
		return false, nil
	}
	hints, err := t.ts.ListByKind(db.KindHint, 50)
	if err == nil {
		for _, h := range hints {
			var p map[string]any
			if json.Unmarshal(h.Payload, &p) == nil && strOf(p["text"]) == text {
				return false, nil
			}
		}
	}
	_, err = t.ts.AddNode(db.KindHint, map[string]any{"text": text}, 0, "active", "adviser", nil)
	return err == nil, err
}

// ───────────────────────── 决策队列 ─────────────────────────

// DecideCapability 执行一次三档裁决：更新能力卡状态 + 写一条可执行 hint。
// 这是"用户只做商业决策，不被打断"的落地：裁决结果变成规划者下一轮能读到的提示。
func (p *P2) DecideCapability(ctx context.Context, ts *db.ExplorationStore, capID int64, actionRaw, note, by string) (decision.Verdict, error) {
	if p == nil || p.DB == nil {
		return decision.Verdict{}, fmt.Errorf("决策队列需要 PostgreSQL（capabilities 表）")
	}
	action, err := decision.Parse(actionRaw)
	if err != nil {
		return decision.Verdict{}, err
	}
	var it decision.Item
	var nodeIDs []int64
	err = p.DB.QueryRowContext(ctx,
		`SELECT id, COALESCE(task_id,0), grp, item, can, surface, status, source_node_ids
		 FROM capabilities WHERE id = $1`, capID).
		Scan(&it.CapID, &it.TaskID, &it.Group, &it.Item, &it.Can, &it.Surface, &it.Status, &nodeIDs)
	if err != nil {
		return decision.Verdict{}, fmt.Errorf("能力卡 %d 不存在：%w", capID, err)
	}
	it.AssetIDs = nodeIDs
	v, err := decision.Decide(it, action, note)
	if err != nil {
		return decision.Verdict{}, err
	}
	if by == "" {
		by = "human"
	}
	budgetMinutes, budgetRounds := 0, 0
	if action == decision.ActionContinue {
		budgetMinutes, budgetRounds = v.BudgetMinutes, v.BudgetRounds
	}
	if _, err := p.DB.ExecContext(ctx,
		`UPDATE capabilities SET status = $2, decided_at = now(), decided_by = $3,
		 decision_action = $4, decision_note = $5,
		 budget_started_at = CASE WHEN $6 > 0 THEN now() ELSE budget_started_at END,
		 budget_minutes = $6, budget_rounds = $7, updated_at = now() WHERE id = $1`,
		capID, v.Status, by, string(action), note, budgetMinutes, budgetRounds); err != nil {
		return decision.Verdict{}, fmt.Errorf("裁决落库失败：%w", err)
	}
	// 裁决必须变成规划者能读到的提示，否则"点了按钮"等于没点。
	if ts != nil {
		_, _ = ts.AddNode(db.KindHint, map[string]any{"text": v.Hint}, 0, "active", by, nil)
	}
	return v, nil
}

// decideCapability 给主 agent 的工具入口。
func (t *ToolSet) decideCapability() actool.CoreTool {
	return writeTool("decide_capability",
		"【决策队列】对一张能力卡做三档裁决：evidence=只拿证据（再取 1 条样本就停，默认）、"+
			"continue=继续深挖（15 分钟 / 20 轮预算，可升 P3）、switch=换目标（冻结该资产，转去未测资产）。"+
			"裁决结果会写成规划者能读到的 hint，任务不会被打断。",
		obj(map[string]any{
			"cap_id": idp("能力卡 id（list_capabilities 返回）"),
			"action": str("evidence|continue|switch"),
			"note":   str("可选备注（会附在 hint 里）"),
		}, "cap_id", "action"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				CapID  int64  `json:"cap_id"`
				Action string `json:"action"`
				Note   string `json:"note"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			by := "human"
			if t.worker != "" && t.worker != "planner" && t.worker != "adviser" {
				by = t.worker
			}
			v, err := t.p2.DecideCapability(ctx, t.ts, a.CapID, a.Action, a.Note, by)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if t.notify != nil {
				t.notify()
			}
			return jsonResult(map[string]any{"verdict": v})
		})
}

// int64ArrayLiteral 把 []int64 编码成 PostgreSQL 数组字面量（"{1,2}"），
// 避免 database/sql 驱动对 []int64 参数不支持的问题。
func int64ArrayLiteral(ids []int64) string {
	if len(ids) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
