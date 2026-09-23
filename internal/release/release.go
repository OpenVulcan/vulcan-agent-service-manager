package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ServiceRepository is the exact owner and repository for service releases.
// ServiceRepository 是服务发布仓库的准确所有者及仓库名。
const ServiceRepository = "OpenVulcan/vulcan-agent-service"

// ManagerRepository is the exact owner and repository for manager releases.
// ManagerRepository 是管理器发布仓库的准确所有者及仓库名。
const ManagerRepository = "OpenVulcan/vulcan-agent-service-manager"

// ChinaProxyBase is an optional, explicitly selected GitHub download proxy.
// ChinaProxyBase 是必须由用户主动选择的 GitHub 下载代理。
const ChinaProxyBase = "https://gh-proxy.com"

// tagPattern accepts only simple version tags and prevents URL path injection.
// tagPattern 仅接受简单版本标签，防止 URL 路径注入。
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Asset is verified metadata returned by the canonical GitHub Releases API.
// Asset 是权威 GitHub Releases API 返回并通过检查的资产元数据。
type Asset struct {
	// Name is the exact archive filename.
	// Name 是精确的归档文件名。
	Name string
	// URL is the canonical GitHub browser download address.
	// URL 是 GitHub 官方浏览器下载地址。
	URL string
	// Size is the published asset size in bytes.
	// Size 是发布资产的字节大小。
	Size int64
	// SHA256 is the hexadecimal GitHub asset digest.
	// SHA256 是 GitHub 资产摘要的十六进制表示。
	SHA256 string
}

// Release holds one published tag and its verified asset catalog.
// Release 保存一个已发布标签及其通过检查的资产目录。
type Release struct {
	// Tag is the published version tag.
	// Tag 是已发布的版本标签。
	Tag string
	// Assets maps exact filenames to their metadata.
	// Assets 按精确文件名映射资产元数据。
	Assets map[string]Asset
}

// Source controls only the transfer endpoint; release metadata always comes from GitHub.
// Source 只控制文件传输端点；发布元数据始终来自 GitHub。
type Source struct {
	// Kind is github or mirror.
	// Kind 为 github 或 mirror。
	Kind string
	// MirrorBase is an HTTPS proxy prefix used only for mirror transfers.
	// MirrorBase 是仅用于镜像传输的 HTTPS 代理前缀。
	MirrorBase string
}

// Client owns bounded HTTP requests and redirects for Release operations.
// Client 持有用于发布操作的有界 HTTP 请求及重定向配置。
type Client struct {
	// HTTP is the transport shared by metadata and download calls.
	// HTTP 是元数据与下载请求共用的传输客户端。
	HTTP *http.Client
	// APIBase allows a local test server to replace the GitHub API.
	// APIBase 允许本地测试服务器替换 GitHub API。
	APIBase string
}

// NewClient returns a GitHub Release client with a request deadline and HTTPS-only redirects.
// NewClient 返回带请求截止时间及仅限 HTTPS 重定向的 GitHub 发布客户端。
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 30 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 10 {
				return errors.New("download redirect is insecure or excessive")
			}
			return nil
		},
	}, APIBase: "https://api.github.com"}
}

// ValidateTag checks a version tag before it enters a URL or asset name.
// ValidateTag 在版本标签进入 URL 或资产名之前检查其格式。
func ValidateTag(tag string) error {
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("invalid version tag %q", tag)
	}
	return nil
}

// CompareTags compares two stable Release tags without losing large version components.
// CompareTags 比较两个稳定发布标签，并避免大版本数字溢出或精度丢失。
func CompareTags(left, right string) (int, error) {
	if err := ValidateTag(left); err != nil {
		return 0, err
	}
	if err := ValidateTag(right); err != nil {
		return 0, err
	}
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	for index := range leftParts {
		leftNumber, leftErr := strconv.ParseUint(leftParts[index], 10, 64)
		rightNumber, rightErr := strconv.ParseUint(rightParts[index], 10, 64)
		if leftErr != nil || rightErr != nil {
			return 0, errors.New("release version component exceeds supported range")
		}
		if leftNumber < rightNumber {
			return -1, nil
		}
		if leftNumber > rightNumber {
			return 1, nil
		}
	}
	return 0, nil
}

// ValidateSource requires an explicit HTTPS proxy for mirror transfers.
// ValidateSource 要求镜像传输显式提供 HTTPS 代理地址。
func ValidateSource(source Source) error {
	if source.Kind == "github" {
		return nil
	}
	if source.Kind != "mirror" {
		return fmt.Errorf("unknown source %q", source.Kind)
	}
	parsed, err := url.Parse(source.MirrorBase)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("mirror base must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

// Fetch resolves the latest published release, or a named tag, from the canonical API.
// Fetch 从权威 API 解析最新已发布版本或指定标签。
func (c *Client) Fetch(ctx context.Context, repository, tag string) (Release, error) {
	if repository != ServiceRepository && repository != ManagerRepository {
		return Release{}, fmt.Errorf("unrecognized release repository %q", repository)
	}
	path := "/repos/" + repository + "/releases/latest"
	if tag != "" {
		if err := ValidateTag(tag); err != nil {
			return Release{}, err
		}
		path = "/repos/" + repository + "/releases/tags/" + url.PathEscape(tag)
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(metadataCtx, http.MethodGet, strings.TrimRight(c.APIBase, "/")+path, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vasm-release-client")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub release request failed: HTTP %d", resp.StatusCode)
	}
	// Limit metadata to a reasonable size so a damaged endpoint cannot consume unlimited memory.
	// 限制元数据大小，避免异常端点无限占用内存。
	var raw struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw); err != nil {
		return Release{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if err := ValidateTag(raw.TagName); err != nil {
		return Release{}, err
	}
	if tag != "" && raw.TagName != tag {
		return Release{}, errors.New("release tag does not match request")
	}
	if raw.Draft || raw.Prerelease {
		return Release{}, errors.New("draft or prerelease is not installable")
	}
	result := Release{Tag: raw.TagName, Assets: make(map[string]Asset)}
	for _, item := range raw.Assets {
		if item.Name == "" || item.Size <= 0 || !strings.HasPrefix(item.Digest, "sha256:") {
			continue
		}
		hash := strings.TrimPrefix(item.Digest, "sha256:")
		if len(hash) != 64 {
			continue
		}
		if _, err := hex.DecodeString(hash); err != nil {
			continue
		}
		parsed, err := url.Parse(item.BrowserDownloadURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Path != "/"+repository+"/releases/download/"+raw.TagName+"/"+item.Name {
			continue
		}
		result.Assets[item.Name] = Asset{Name: item.Name, URL: item.BrowserDownloadURL, Size: item.Size, SHA256: strings.ToLower(hash)}
	}
	return result, nil
}

// FindAsset returns one exact archive, rejecting releases that omit its digest.
// FindAsset 返回一份精确归档，并拒绝未提供摘要的发布资产。
func (r Release) FindAsset(name string) (Asset, error) {
	asset, ok := r.Assets[name]
	if !ok {
		return Asset{}, fmt.Errorf("release %s has no verified asset %s", r.Tag, name)
	}
	return asset, nil
}

// URL returns the selected transport URL without changing the canonical asset identity.
// URL 返回所选传输地址，同时保持权威资产身份不变。
func (s Source) URL(asset Asset) (string, error) {
	if err := ValidateSource(s); err != nil {
		return "", err
	}
	if s.Kind == "github" {
		return asset.URL, nil
	}
	return strings.TrimRight(s.MirrorBase, "/") + "/" + asset.URL, nil
}

// Download streams an exact asset into a temporary file, verifies size and SHA-256, then commits it.
// Download 将精确资产流式写入暂存文件，校验大小与 SHA-256 后提交。
func (c *Client) Download(ctx context.Context, source Source, asset Asset, destination string, progress func(int64, int64)) error {
	endpoint, err := source.URL(asset)
	if err != nil {
		return err
	}
	if asset.Size <= 0 || len(asset.SHA256) != 64 {
		return errors.New("asset metadata is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".vasm-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vasm-release-client")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s failed: HTTP %d", endpoint, resp.StatusCode)
	}
	hasher := sha256.New()
	buffer := make([]byte, 128<<10)
	var written int64
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			written += int64(n)
			if written > asset.Size {
				return fmt.Errorf("asset %s exceeds published size", asset.Name)
			}
			if _, err := temp.Write(buffer[:n]); err != nil {
				return err
			}
			if _, err := hasher.Write(buffer[:n]); err != nil {
				return err
			}
			if progress != nil {
				progress(written, asset.Size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if written != asset.Size || hex.EncodeToString(hasher.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("asset %s failed size or SHA-256 verification", asset.Name)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), destination)
}
