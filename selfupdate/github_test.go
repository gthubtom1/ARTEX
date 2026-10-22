package selfupdate

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchLatestUsesForkRelease(t *testing.T) {
	client := &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		// 期望值从常量推导，避免常量改了测试还留着旧仓库名（原来这里写死 RuoJi6/ARTEX = 上游）
		if got := r.URL.String(); got != "https://api.github.com/repos/"+Repo+"/releases/latest" {
			t.Fatalf("unexpected update source: %s", got)
		}
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("invalid GitHub request: %s %v", r.Method, r.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
			"tag_name":"v0.3.16",
			"assets":[
				{"name":"artex-0.3.16-linux-arm64.zip","browser_download_url":"https://github.com/RuoJi6/ARTEX/releases/download/v0.3.16/artex-0.3.16-linux-arm64.zip"},
				{"name":"SHA256SUMS","browser_download_url":"https://github.com/RuoJi6/ARTEX/releases/download/v0.3.16/SHA256SUMS"}
			]
		}`))}, nil
	})}
	rel, err := FetchLatest(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{AssetName(rel.TagName, "linux", "arm64"), sumsAsset} {
		asset, ok := rel.FindAsset(name)
		if !ok {
			t.Fatalf("missing release asset %s", name)
		}
		body, err := get(context.Background(), &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("asset"))}, nil
		})}, asset.URL)
		if err != nil {
			t.Fatalf("fork asset URL rejected: %v", err)
		}
		body.Close()
	}
}

func TestFetchLatestMissingRelease(t *testing.T) {
	client := &http.Client{Transport: releaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	_, err := FetchLatest(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), Repo) || !strings.Contains(err.Error(), "不可访问或尚未发布") {
		t.Fatalf("unexpected missing-release error: %v", err)
	}
}

// 守门：升级源必须是本 fork。指回上游的后果是「点一键更新 → 二开版被换成上游版」，
// 数据库已跑到 0004 迁移而上游没有这些表，等于把运行中的安装换成不兼容版本。
func TestRepoIsThisForkNotUpstream(t *testing.T) {
	if Repo == "RuoJi6/ARTEX" || Repo == "Autumn-27/ARTEX" {
		t.Fatalf("升级源指回了上游（%s）：点一键更新会把二开版替换成上游版", Repo)
	}
	if Repo != "gthubtom1/ARTEX" {
		t.Fatalf("升级源应为 gthubtom1/ARTEX，实际 %s", Repo)
	}
}
