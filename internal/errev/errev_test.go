package errev

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeClass(t *testing.T) {
	cases := map[string]string{
		"":               ClassSelfError,
		"env_error":      ClassEnvError,
		" ENV_ERROR ":    ClassEnvError,
		"tool_missing":   ClassToolMissing,
		"target_blocked": ClassTargetBlocked,
		"policy_blocked": ClassPolicyBlocked,
		"self_error":     ClassSelfError,
		"something-else": ClassSelfError,
		"POLICY_BLOCKED": ClassPolicyBlocked,
	}
	for in, want := range cases {
		if got := NormalizeClass(in); got != want {
			t.Errorf("NormalizeClass(%q)=%q want %q", in, got, want)
		}
	}
}

func TestEmitTruncatesAndNormalizes(t *testing.T) {
	var got struct {
		taskID, intentID int64
		class, tool, dt  string
	}
	prev := Sink
	t.Cleanup(func() { Sink = prev })
	Sink = func(ctx context.Context, taskID, intentID int64, class, tool, detail string) {
		got.taskID, got.intentID, got.class, got.tool, got.dt = taskID, intentID, class, tool, detail
	}

	long := strings.Repeat("啊", 900) + "\n第二行"
	Emit(context.Background(), 42, "bogus", "Bash", long)

	if got.taskID != 42 {
		t.Fatalf("taskID=%d", got.taskID)
	}
	if got.intentID != 0 {
		t.Fatalf("没有 IntentIDOf 时意图 id 应写 0，实际 %d", got.intentID)
	}
	if got.class != ClassSelfError {
		t.Fatalf("未知分类应归到 self_error，实际 %q", got.class)
	}
	if !strings.HasSuffix(got.dt, "…") {
		t.Fatal("超长 detail 应被截断并以 … 结尾")
	}
	if len([]rune(got.dt)) > maxDetail+1 {
		t.Fatalf("detail 长度 %d 超过上限", len([]rune(got.dt)))
	}
	if strings.Contains(got.dt, "\n") {
		t.Fatal("detail 不应包含换行（会被塞进一行 UI）")
	}
}

func TestEmitNilSinkIsNoop(t *testing.T) {
	prev := Sink
	t.Cleanup(func() { Sink = prev })
	Sink = nil
	Emit(context.Background(), 1, ClassEnvError, "curl", "boom") // 不应 panic
}

// 意图 id 从 ctx 注入的钩子解析；EmitIntent 则显式覆盖。
func TestEmitResolution(t *testing.T) {
	var gotIntent int64
	prevSink, prevOf := Sink, IntentIDOf
	t.Cleanup(func() { Sink, IntentIDOf = prevSink, prevOf })
	Sink = func(ctx context.Context, taskID, intentID int64, class, tool, detail string) {
		gotIntent = intentID
	}

	IntentIDOf = nil
	Emit(context.Background(), 4, ClassSelfError, "w", "x")
	if gotIntent != 0 {
		t.Fatalf("未注入钩子时应写 0，实际 %d", gotIntent)
	}

	IntentIDOf = func(context.Context) int64 { return 92 }
	Emit(context.Background(), 4, ClassSelfError, "w", "x")
	if gotIntent != 92 {
		t.Fatalf("应从 ctx 解析出意图 92，实际 %d", gotIntent)
	}

	EmitIntent(context.Background(), 4, -7, ClassSelfError, "w", "x")
	if gotIntent != 0 {
		t.Fatalf("负数意图 id 应归 0，实际 %d", gotIntent)
	}
}

func TestClassifyHTTP(t *testing.T) {
	blocked := []int{401, 403, 429, 503}
	for _, s := range blocked {
		if got := ClassifyHTTP(s); got != ClassTargetBlocked {
			t.Errorf("ClassifyHTTP(%d)=%q want target_blocked", s, got)
		}
	}
	for _, s := range []int{200, 302, 404, 500} {
		if got := ClassifyHTTP(s); got != ClassSelfError {
			t.Errorf("ClassifyHTTP(%d)=%q want self_error", s, got)
		}
	}
}

func TestLooksLike(t *testing.T) {
	if !LooksLikeTargetBlocked("HTTP 403 Forbidden from /admin") {
		t.Fatal("应识别为目标拒绝")
	}
	if LooksLikeTargetBlocked("dial tcp 127.0.0.1:7897: connect: connection refused") {
		t.Fatal("误判为目标拒绝")
	}
	if !LooksLikeEnvError("proxyconnect tcp: dial tcp 127.0.0.1:7897: connect: connection refused") {
		t.Fatal("应识别为环境错误")
	}
	if LooksLikeEnvError("error parsing json") {
		t.Fatal("误判为环境错误")
	}
}
