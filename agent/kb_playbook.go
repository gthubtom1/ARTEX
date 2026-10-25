package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	artexkb "github.com/Autumn-27/artex/internal/kb"
	actool "github.com/Autumn-27/norma/tool"
)

// kb_playbook（P0-02，docs/PLANS/2026-10-25-artex-找洞能力审查.md）：
// 拿到一个新入口时先查"这一类怎么验" —— 判定标准、常见误判、不许跳级的检查顺序。
//
// 为什么需要：kb/tactics 里有 6 篇 428 行打法，但之前**零接线**（只是给人看的散文），
// worker 全靠自由发挥 → 漏步、把"报错文案"当注入、拿到回显就收工。
//
// 纪律：
//   - 硬限返回量（≤3 篇 × ≤1200 字），不许把 kb 变成上下文炸弹；
//   - 纯读、同输入同输出，不调模型、不写库；
//   - 只在 worker 侧常驻（规划者的活是派方向，不是查步骤）。
const kbPlaybookMaxChars = 3200

func (t *ToolSet) kbPlaybook() actool.CoreTool {
	return readTool("kb_playbook",
		"拿到一个新入口/参数时先查一次：这类东西怎么验才算命中、哪些是常见误判、检查顺序是什么（不许跳级）。"+
			"参数 shape 写你看到的形状，例如 `POST /doLogin uid/passw`、`GET /api/order?orderId=1`、`upload filename=.php`。",
		obj(map[string]any{
			"shape": str("入口形状：方法 + 路径 + 参数名（越具体越好）"),
			"limit": intp("最多几篇（默认 2，上限 3）"),
		}, "shape"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Shape string `json:"shape"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			shape := strings.TrimSpace(a.Shape)
			if shape == "" {
				return actool.Errorf("shape 必填（写出方法/路径/参数名）"), nil
			}
			root := resolveKBRoot(t.kbRoot)
			all := artexkb.LoadPlaybooks(root)
			if len(all) == 0 {
				return actool.Text("知识库里没有 tactic 素材（kb/tactics 缺失），按你自己的方法继续，但必须给可复现证据。"), nil
			}
			lim := a.Limit
			if lim <= 0 || lim > 3 {
				lim = 2
			}
			picked := artexkb.MatchPlaybooks(shape, all, lim)
			if len(picked) == 0 {
				return actool.Text("没有匹配到打法；按最小验证推进：先固定基线，再一次只改一个变量，留可复现证据。"), nil
			}
			var b strings.Builder
			for _, pb := range picked {
				s := pb.Render(8, 4)
				if b.Len()+len(s) > kbPlaybookMaxChars {
					break
				}
				b.WriteString(s)
				b.WriteString("\n")
			}
			if b.Len() == 0 {
				// 第一篇就超上限：压到最小形态，绝不让工具返回空。
				s := picked[0].Render(4, 2)
				if len(s) > kbPlaybookMaxChars {
					s = s[:kbPlaybookMaxChars]
				}
				b.WriteString(s)
			}
			return actool.Text(strings.TrimSpace(b.String())), nil
		})
}

// resolveKBRoot 找到知识库根目录：显式设置 > 环境变量 > 工作目录 > 可执行文件旁边。
// 这样同一个二进制在开发目录（./kb）和安装目录（/opt/artex/kb）都能读到打法素材。
func resolveKBRoot(explicit string) string {
	cands := make([]string, 0, 6)
	if explicit != "" {
		cands = append(cands, explicit)
	}
	if v := strings.TrimSpace(os.Getenv("ARTEX_KB_PATH")); v != "" {
		cands = append(cands, v)
	}
	cands = append(cands, "kb", "./kb", "../kb", "../../kb")
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		cands = append(cands, filepath.Join(d, "kb"), filepath.Join(d, "..", "kb"))
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(c, "tactics")); err == nil && st.IsDir() {
			return c
		}
	}
	return "kb"
}
