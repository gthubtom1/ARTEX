package agent

import (
	"context"
	"encoding/json"
	"strings"

	actool "github.com/Autumn-27/norma/tool"

	"github.com/Autumn-27/artex/internal/oast"
)

// NewOASTTools 返回带外通道的两个工具（未启用通道时返回 nil，不占 token）。
//
// 为什么需要它：盲 SSRF / 盲命令注入 / 盲 XXE / Log4j 这类漏洞，目标**不会回显**
// 任何东西。唯一的判据是"目标有没有真的去访问你给的地址"。
//
// 用法（worker 侧）：
//  1. 调 oast_new_url 拿一个专属地址（每次探测用新的，便于归因）；
//  2. 把它塞进注入点（url 参数、命令拼接、XML 实体、JNDI 等）；
//  3. 等几秒，调 oast_hits（带 step 1 的 id）看有没有回连；
//  4. 有回连 = 漏洞成立，把 Source（源 IP）与 At（时间）写进 report_finding 的 evidence。
func NewOASTTools(srv *oast.Server) []actool.CoreTool {
	if srv == nil {
		return nil
	}
	return []actool.CoreTool{
		writeTool(
			"oast_new_url",
			"申请一个带外回调地址，用于验证**看不见的漏洞**（盲 SSRF / 盲命令注入 / 盲 XXE / JNDI 注入等）。\n"+
				"返回 {id, http_url, dns_host}：把 http_url（或 dns_host）塞进注入点，然后用 oast_hits 查回连。\n"+
				"收到回连 = 目标真的执行了，漏洞成立且带源 IP 与时间作为证据；没收到 = 不能算成立。\n"+
				"每次探测请用新的 id（便于把回连归因到具体哪一次注入）。",
			obj(map[string]any{}),
			func(ctx context.Context, _ json.RawMessage) (actool.Result, error) {
				return jsonResult(srv.NewToken())
			},
		),
		writeTool(
			"oast_hits",
			"查询带外回调记录。参数 id 传 oast_new_url 返回的 id；留空则返回全部。\n"+
				"返回 {hits:[{token_id, proto, source, detail, at}]}：source 是回连的来源 IP，"+
				"proto 是 http/dns，detail 是请求摘要（路径/UA 等）。\n"+
				"有记录就是硬证据：把它写进 report_finding 的 evidence（例：\"目标于 <at> 从 <source> 回连 <url>，证明命令执行\")。",
			obj(map[string]any{
				"id": str("oast_new_url 返回的 token id（留空=全部）"),
			}),
			func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
				var args struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(in, &args)
				id := strings.TrimSpace(args.ID)
				if id == "" {
					return jsonResult(map[string]any{"hits": srv.AllHits()})
				}
				return jsonResult(map[string]any{"hits": srv.Hits(id)})
			},
		),
	}
}
