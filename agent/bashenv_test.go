package agent

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/envfilter"
)

// P8-7 的纵深防御：norma 的 Bash 工具是 append(os.Environ(), extra...)，所以
// 光靠"extra 里不加密钥"没用 —— 父进程环境里的 ARTEX_CRED_KEY 会原样继承进子进程，
// worker 里一句 `env` 就把凭据主密钥打进 transcript。
//
// 唯一可靠的盖法是**显式追加空值**（K=），让子进程里该变量为空。
// 而且直连模式下 proxyEnv 返回 nil，这时也必须盖住 —— 这条测试就是守这个。
func TestBashEnvBlanksInheritedSecrets(t *testing.T) {
	cases := map[string][]string{
		"直连（proxyEnv 返回 nil）": envfilter.BlankInherited(nil),
		"走代理":                envfilter.BlankInherited(proxyEnv("http://127.0.0.1:7897", "")),
		"走代理 + CA":           envfilter.BlankInherited(proxyEnv("http://127.0.0.1:7897", "/tmp/ca.pem")),
	}
	for name, env := range cases {
		joined := strings.Join(env, "\n")
		for _, must := range []string{"ARTEX_CRED_KEY=", "ARTEX_PG_DSN="} {
			if !strings.Contains(joined, must) {
				t.Fatalf("%s：BashEnv 里缺少显式空值 %q，子进程会继承父进程的真实值\nenv=%v", name, must, env)
			}
		}
	}

	// 代理项必须保留（盖密钥不能把代理也盖掉）
	env := envfilter.BlankInherited(proxyEnv("http://127.0.0.1:7897", ""))
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HTTP_PROXY=http://127.0.0.1:7897") {
		t.Fatal("盖密钥时把 HTTP_PROXY 弄丢了")
	}
	if !strings.Contains(joined, "ALL_PROXY=http://127.0.0.1:7897") {
		t.Fatal("盖密钥时把 ALL_PROXY 弄丢了")
	}
}
