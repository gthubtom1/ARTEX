// Package egress 是「出口执行闸」：所有会发网络包的执行器（execx/remote/recheck/replay）
// 在 spawn 之前统一问一次 Check（server 注入 netguard.Verify，fail-closed），
// 并统一注入代理环境变量。未注入时为空实现（测试/无出口配置场景）。
package egress

import "context"

// Check 由 server 注入：返回错误即拒绝执行（fail-closed）。
var Check func(context.Context) error

// ProxyEnv 由 server 注入（HTTP_PROXY/HTTPS_PROXY/ALL_PROXY）。
var ProxyEnv []string

// Guard 在执行前调用。
func Guard(ctx context.Context) error {
	if Check == nil {
		return nil
	}
	return Check(ctx)
}
