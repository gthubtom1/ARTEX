package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/creds"
	"github.com/Autumn-27/artex/internal/dataloc"
	"github.com/Autumn-27/artex/internal/harvest"
	actool "github.com/Autumn-27/norma/tool"
)

// M15 数据定位 + M12 凭据收割：把数据库探测输出变成 D1–D4 证明，
// 把配置/历史/元数据里的凭据直接收割进加密保险库。
// 纪律：数据只记录不导出；凭据明文只进库，工具返回一律打码。

const (
	harvestMaxSignals   = 200
	harvestMaxTextBytes = 64 * 1024
	staleCredDays       = 7
)

// ───────────────────────── M15 数据定位 ─────────────────────────

func (t *ToolSet) dataLocate() actool.CoreTool {
	return readTool("data_locate",
		"【数据定位/M15】把数据库客户端输出（information_schema / SHOW TABLES / SELECT COUNT(*) / psql 表格）解析成"+
			"表名、行数、敏感列，并给出 D1–D4 可达性证明级别（D1 仅表名/列名、D2 有行数、D3 有敏感列、D4 出现导出能力）。"+
			"**只记录不导出**：本工具不执行任何 SQL、不导出数据；导出类语句只标注不执行。",
		obj(map[string]any{
			"engine":   str("数据库类型：mysql|postgres|mssql"),
			"output":   str("数据库客户端的原始输出（可含表头/边框/psql 表格）"),
			"asset_id": intp("关联资产 id（可选，仅记录）"),
		}, "engine", "output"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Engine  string `json:"engine"`
				Output  string `json:"output"`
				AssetID int64  `json:"asset_id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if strings.TrimSpace(a.Output) == "" {
				return actool.Errorf("output 不能为空：请粘贴数据库客户端的原始输出"), nil
			}
			res := dataloc.Parse(a.Engine, a.Output)
			return jsonResult(map[string]any{
				"asset_id": a.AssetID,
				"engine":   a.Engine,
				"result":   res,
				"summary":  dataLocateSummary(a.Engine, res),
				"notice":   "只记录不导出：未执行任何 SQL/导出命令；导出类证据仅标注不执行。",
				"probes":   dataloc.PlanProbe(a.Engine),
			})
		})
}

func dataLocateSummary(engine string, r dataloc.Result) string {
	eng := strings.TrimSpace(engine)
	if eng == "" {
		eng = "unknown"
	}
	if len(r.Tables) == 0 {
		return fmt.Sprintf("%s：未解析到表，证明级别 %s（补 information_schema.tables 输出可升到 D1）", eng, r.ProofLevel)
	}
	known, sensitive := 0, 0
	var rows int64
	for _, tb := range r.Tables {
		if tb.Rows >= 0 {
			known++
			rows += tb.Rows
		}
		sensitive += len(tb.SensitiveColumns)
	}
	return fmt.Sprintf("%s：%d 张表，%d 张有行数（合计 %d 行），%d 个敏感列，证明级别 %s；只记录不导出",
		eng, len(r.Tables), known, rows, sensitive, r.ProofLevel)
}

// ───────────────────────── M12 凭据收割 ─────────────────────────

func (t *ToolSet) credHarvest() actool.CoreTool {
	return writeTool("cred_harvest",
		"【凭据收割/M12】从配置文本（.env/yaml/properties/php define/连接串/bash history）或云元数据 JSON"+
			"（AWS/GCP/阿里云 IMDS）里自动识别凭据，逐条写入加密凭据库（AES-GCM，去重）。"+
			"只返回 cred_id/类型/用户名/置信度和打码提示，**绝不回显明文**；拿到 cred_id 后用 cred_reuse_plan 试服务。",
		obj(map[string]any{
			"text":          str("原始文本（配置/历史/连接串），与 metadata_json 至少提供一个"),
			"metadata_json": str("云元数据 JSON（AccessKeyId/SecretAccessKey/Token/private_key 等）"),
			"source":        str("来源标签：config|history|browser|cloud|manual（默认 text）"),
			"asset_id":      intp("关联资产 id（可选）"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.p2 == nil || t.p2.Creds == nil {
				return actool.Errorf("凭据库不可用：P2 装配包未注入"), nil
			}
			var a struct {
				Text         string `json:"text"`
				MetadataJSON string `json:"metadata_json"`
				Source       string `json:"source"`
				AssetID      int64  `json:"asset_id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if strings.TrimSpace(a.Text) == "" && strings.TrimSpace(a.MetadataJSON) == "" {
				return actool.Errorf("text 与 metadata_json 至少提供一个"), nil
			}
			if len(a.Text) > harvestMaxTextBytes || len(a.MetadataJSON) > harvestMaxTextBytes {
				return actool.Errorf("输入过长（>64KB）：请分段收割"), nil
			}
			source := strings.TrimSpace(a.Source)
			if source == "" {
				source = "text"
			}

			var signals []harvest.Signal
			if strings.TrimSpace(a.Text) != "" {
				signals = append(signals, harvest.FromText(source, a.Text)...)
			}
			if strings.TrimSpace(a.MetadataJSON) != "" {
				signals = append(signals, harvest.FromMetadata(a.MetadataJSON)...)
			}
			if len(signals) > harvestMaxSignals {
				signals = signals[:harvestMaxSignals]
			}

			out := make([]map[string]any, 0, len(signals))
			added, existing, failed := 0, 0, 0
			for _, s := range signals {
				if strings.TrimSpace(s.Secret) == "" {
					continue
				}
				src := s.Source
				if src == "" {
					src = source
				} else if !strings.HasPrefix(src, source) {
					src = source + ":" + src
				}
				id, wasExisting, err := t.p2.Creds.Add(ctx, creds.Entry{
					TaskID: t.taskID, AssetID: a.AssetID, Kind: s.Kind, Username: s.Username, Source: src,
				}, s.Secret)
				if err != nil {
					failed++
					continue
				}
				if wasExisting {
					existing++
				} else {
					added++
				}
				// 只回打码提示，明文绝不回显。
				out = append(out, map[string]any{
					"cred_id":     id,
					"kind":        s.Kind,
					"username":    s.Username,
					"confidence":  s.Confidence,
					"secret_hint": harvest.Redact(s.Secret),
					"asset_hint":  s.AssetHint,
					"existing":    wasExisting,
				})
			}
			return jsonResult(map[string]any{
				"found":       len(signals),
				"added":       added,
				"existing":    existing,
				"failed":      failed,
				"credentials": out,
				"notice":      "明文已写入加密凭据库，本工具不回显明文；用 cred_reuse_plan 计算可试服务。",
			})
		})
}

func (t *ToolSet) credStaleScan() actool.CoreTool {
	return readTool("cred_stale_scan",
		"【凭据卫生】列出需要复验/轮换的凭据：status=invalid，或创建超过 7 天且从未使用（use_count=0）。"+
			"只给元数据（不含明文）；拿到 cred_id 后用 cred_verify 复验，失败则重新收割。",
		obj(map[string]any{
			"limit": intp("最多扫描条数（默认 200）"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.p2 == nil || t.p2.Creds == nil {
				return actool.Errorf("凭据库不可用：P2 装配包未注入"), nil
			}
			var a struct {
				Limit int `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 200
			}
			list, err := t.p2.Creds.List(ctx, t.taskID, a.Limit)
			if err != nil {
				return actool.Errorf("读取失败：" + err.Error()), nil
			}
			now := time.Now()
			stale := make([]map[string]any, 0, len(list))
			for _, e := range list {
				reasons := make([]string, 0, 2)
				if e.Status == "invalid" {
					reasons = append(reasons, "已验证失败（status=invalid）")
				}
				ageDays := 0
				if !e.CreatedAt.IsZero() {
					ageDays = int(now.Sub(e.CreatedAt).Hours() / 24)
					if e.UseCount == 0 && ageDays > staleCredDays {
						reasons = append(reasons, fmt.Sprintf("创建 %d 天且从未使用（use_count=0）", ageDays))
					}
				}
				if len(reasons) == 0 {
					continue
				}
				stale = append(stale, map[string]any{
					"cred_id":    e.ID,
					"kind":       e.Kind,
					"username":   e.Username,
					"status":     e.Status,
					"use_count":  e.UseCount,
					"verified":   e.Verified,
					"asset_id":   e.AssetID,
					"age_days":   ageDays,
					"created_at": e.CreatedAt,
					"reasons":    reasons,
					"action":     "复验（cred_verify）或轮换后重新收割",
				})
			}
			return jsonResult(map[string]any{
				"stale":   stale,
				"count":   len(stale),
				"scanned": len(list),
				"hint":    "这些凭据需要复验/轮换：先 cred_verify 复验，失败则标记 invalid 并重新收割。",
			})
		})
}

// dataMask 把已取到的样本值打码（D3 只允许 ≤1 行且必须打码）。
func (t *ToolSet) dataMask() actool.CoreTool {
	return readTool("data_mask",
		"【样本打码】对已取到的 1 行样本值做打码：手机 3+2、邮箱首字母+域名、身份证 4+2、其余首尾各 2。"+
			"写报告/能力卡之前先把样本过一遍它。",
		obj(map[string]any{
			"values": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "原始样本值（≤1 行）"},
		}, "values"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Values []string `json:"values"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if len(a.Values) == 0 {
				return actool.Errorf("values 不能为空"), nil
			}
			out := make([]string, 0, len(a.Values))
			for _, v := range a.Values {
				out = append(out, dataloc.MaskValue(v))
			}
			return jsonResult(map[string]any{"masked": out})
		})
}
