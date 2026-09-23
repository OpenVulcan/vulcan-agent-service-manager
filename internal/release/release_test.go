package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCanonicalMetadataAndMirrorBytes verifies official identity before proxy transfer.
// TestCanonicalMetadataAndMirrorBytes 验证代理传输前先确认官方资产身份。
func TestCanonicalMetadataAndMirrorBytes(t *testing.T) {
	contents := []byte("verified manager archive")
	hash := sha256.Sum256(contents)
	corrupt := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Error("GitHub token was forwarded to a non-official endpoint")
		}
		if strings.HasPrefix(request.URL.Path, "/repos/") {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"tag_name": "v0.1.0",
				"assets": []map[string]any{{
					"name":                 "vasm-windows-x64.zip",
					"browser_download_url": "https://github.com/OpenVulcan/vulcan-agent-service-manager/releases/download/v0.1.0/vasm-windows-x64.zip",
					"size":                 len(contents),
					"digest":               "sha256:" + hex.EncodeToString(hash[:]),
				}},
			})
			return
		}
		if corrupt {
			_, _ = writer.Write([]byte("unverified proxy payload"))
			return
		}
		_, _ = writer.Write(contents)
	}))
	defer server.Close()
	client := NewClient()
	client.HTTP = server.Client()
	client.APIBase = server.URL
	client.Token = "private-test-token"
	info, err := client.Fetch(context.Background(), ManagerRepository, "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := info.FindAsset("vasm-windows-x64.zip")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), asset.Name)
	source := Source{Kind: "mirror", MirrorBase: server.URL}
	if err := client.Download(context.Background(), source, asset, target, nil); err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(target)
	if err != nil || string(downloaded) != string(contents) {
		t.Fatalf("verified download mismatch: %q, %v", downloaded, err)
	}
	corrupt = true
	if err := client.Download(context.Background(), source, asset, target, nil); err == nil {
		t.Fatal("tampered mirror bytes were accepted")
	}
	retained, err := os.ReadFile(target)
	if err != nil || string(retained) != string(contents) {
		t.Fatalf("failed download changed committed archive: %q, %v", retained, err)
	}
}

// TestRetaggedReleaseMetadata verifies that an empty tag response is refreshed by numeric release ID.
// TestRetaggedReleaseMetadata 验证标签响应缺少资产时按数字发布标识重新获取，并拒绝身份不一致的结果。
func TestRetaggedReleaseMetadata(t *testing.T) {
	// The observed GitHub tag endpoint can omit assets that its numeric endpoint already lists.
	// 实测 GitHub 标签接口可能漏掉数字发布接口已列出的资产。
	wrongIdentity := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/" + ManagerRepository + "/releases/tags/v0.1.0":
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": 7, "tag_name": "v0.1.0", "assets": []any{}})
		case "/repos/" + ManagerRepository + "/releases/7":
			resolvedTag := "v0.1.0"
			if wrongIdentity {
				resolvedTag = "v0.1.1"
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"id":       7,
				"tag_name": resolvedTag,
				"assets": []map[string]any{{
					"name":                 "vasm-windows-x64.zip",
					"browser_download_url": "https://github.com/OpenVulcan/vulcan-agent-service-manager/releases/download/v0.1.0/vasm-windows-x64.zip",
					"size":                 1,
					"digest":               "sha256:" + strings.Repeat("a", 64),
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := NewClient()
	client.HTTP = server.Client()
	client.APIBase = server.URL
	client.Token = "private-test-token"
	info, err := client.Fetch(context.Background(), ManagerRepository, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := info.FindAsset("vasm-windows-x64.zip"); err != nil {
		t.Fatal(err)
	}
	wrongIdentity = true
	if _, err := client.Fetch(context.Background(), ManagerRepository, "v0.1.0"); err == nil {
		t.Fatal("numeric release endpoint changed the requested identity")
	}
}

// TestSourceAndVersionValidation refuses unsafe proxy bases and ambiguous version tags.
// TestSourceAndVersionValidation 拒绝不安全的代理基址及含糊的版本标签。
func TestSourceAndVersionValidation(t *testing.T) {
	for _, invalid := range []Source{{Kind: "mirror", MirrorBase: "http://example.com"}, {Kind: "mirror", MirrorBase: "https://user:secret@example.com"}, {Kind: "other"}} {
		if err := ValidateSource(invalid); err == nil {
			t.Fatalf("unsafe source accepted: %+v", invalid)
		}
	}
	if err := ValidateTag("v0.1.0/../../other"); err == nil {
		t.Fatal("unsafe tag accepted")
	}
	comparison, err := CompareTags("v0.2.0", "v0.1.9")
	if err != nil || comparison <= 0 {
		t.Fatalf("version comparison failed: %d, %v", comparison, err)
	}
}
