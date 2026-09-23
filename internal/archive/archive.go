package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaxExpandedBytes bounds extraction of a full service release.
// MaxExpandedBytes 限制完整服务发布包解压后的总字节数。
const MaxExpandedBytes int64 = 4 << 30

// MaxEntries bounds the number of paths accepted from an archive.
// MaxEntries 限制归档内可接受的路径数量。
const MaxEntries = 100000

// Extract validates and extracts a release archive with one expected root directory.
// Extract 校验并解压只含一个预期根目录的发布归档。
// archivePath is the downloaded archive, destination is a new empty directory, and root is the expected top-level name.
// archivePath 为已下载归档，destination 为新的空目录，root 为预期的顶层目录名。
// It accepts contained TAR symbolic links and rejects traversal, duplicate entries, or oversized content.
// 允许目标位于包内的 TAR 符号链接，拒绝路径穿越、重复条目和超大内容。
func Extract(archivePath, destination, root string) error {
	if root == "" || strings.ContainsAny(root, `/\`) || root == "." || root == ".." {
		return errors.New("invalid archive root")
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, destination, root)
	}
	if strings.HasSuffix(archivePath, ".tar.gz") {
		return extractTarGzip(archivePath, destination, root)
	}
	return errors.New("unsupported archive type")
}

// tracker records expanded size and unique normalized paths during extraction.
// tracker 在解压过程中记录展开大小和唯一规范化路径。
type tracker struct {
	// count is the number of entries already seen.
	// count 是已处理条目数量。
	count int
	// bytes is the total number of expanded file bytes.
	// bytes 是已展开文件的总字节数。
	bytes int64
	// seen rejects paths that collide on the destination filesystem.
	// seen 拒绝在目标文件系统上发生冲突的路径。
	seen map[string]bool
	// caseFold selects case-insensitive path keys when the destination filesystem requires them.
	// caseFold 在目标文件系统不区分大小写时启用折叠路径键。
	caseFold bool
}

// newTracker creates the bounded extraction accounting state.
// newTracker 创建有界解压计数状态。
func newTracker(caseFold bool) *tracker {
	return &tracker{seen: make(map[string]bool), caseFold: caseFold}
}

// target validates one archive member and returns its destination beneath the extraction root.
// target 校验单个归档成员并返回解压根目录内的目标路径。
func (t *tracker) target(name, destination, root string, size int64) (string, error) {
	if size < 0 || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	clean := path.Clean(trimmed)
	if clean != trimmed || (clean != root && !strings.HasPrefix(clean, root+"/")) {
		return "", fmt.Errorf("archive path leaves expected root: %q", name)
	}
	key := clean
	if t.caseFold {
		key = strings.ToLower(clean)
	}
	if t.seen[key] {
		return "", fmt.Errorf("duplicate archive path %q", name)
	}
	if size > MaxExpandedBytes-t.bytes {
		return "", errors.New("archive expansion limit exceeded")
	}
	t.seen[key] = true
	t.count++
	t.bytes += size
	if t.count > MaxEntries {
		return "", errors.New("archive expansion limit exceeded")
	}
	if clean == root {
		return destination, nil
	}
	return filepath.Join(destination, filepath.FromSlash(strings.TrimPrefix(clean, root+"/"))), nil
}

// filesystemCaseFold probes the new extraction directory for case-insensitive name lookup.
// filesystemCaseFold 探测新解压目录是否采用大小写不敏感的文件名查找。
func filesystemCaseFold(destination string) (bool, error) {
	probe, err := os.CreateTemp(destination, ".vasm-case-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(probe.Name())
	if err := probe.Close(); err != nil {
		return false, err
	}
	upper := filepath.Join(destination, strings.ToUpper(filepath.Base(probe.Name())))
	upperInfo, err := os.Stat(upper)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	probeInfo, err := os.Stat(probe.Name())
	if err != nil {
		return false, err
	}
	if !os.SameFile(probeInfo, upperInfo) {
		return false, errors.New("case-sensitivity probe collided with another file")
	}
	return true, nil
}

// writeFile copies exactly size bytes into a newly created regular file.
// writeFile 将恰好 size 字节复制到新建的普通文件。
func writeFile(destination string, reader io.Reader, size int64, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm()&0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(output, reader, size)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// extractZip validates every ZIP entry before writing it to the destination.
// extractZip 在将 ZIP 条目写入目标目录之前逐项校验。
func extractZip(archivePath, destination, root string) error {
	input, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer input.Close()
	caseFold, err := filesystemCaseFold(destination)
	if err != nil {
		return err
	}
	track := newTracker(caseFold)
	for _, entry := range input.File {
		info := entry.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported ZIP entry %q", entry.Name)
		}
		size := int64(entry.UncompressedSize64)
		if info.IsDir() {
			size = 0
		}
		path, err := track.target(entry.Name, destination, root, size)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		writeErr := writeFile(path, reader, size, info.Mode())
		closeErr := reader.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// tarLink records a symbolic link to create after all regular archive entries are written.
// tarLink 记录在全部普通归档条目写入后创建的符号链接。
type tarLink struct {
	// name is the validated destination path inside the extraction directory.
	// name 是解压目录内经校验的目标路径。
	name string
	// target is the original relative link text stored in the archive.
	// target 是归档中保存的原始相对链接文本。
	target string
}

// extractTarGzip validates regular files, directories, and contained relative links in a gzip TAR archive.
// extractTarGzip 校验 gzip TAR 归档中的普通文件、目录和包内相对链接。
func extractTarGzip(archivePath, destination, root string) error {
	input, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer input.Close()
	decompressor, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer decompressor.Close()
	reader := tar.NewReader(decompressor)
	caseFold, err := filesystemCaseFold(destination)
	if err != nil {
		return err
	}
	track := newTracker(caseFold)
	var links []tarLink
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return createTarLinks(destination, links)
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeSymlink {
			return fmt.Errorf("unsupported TAR entry %q", header.Name)
		}
		size := header.Size
		if header.Typeflag != tar.TypeReg {
			size = 0
		}
		path, err := track.target(header.Name, destination, root, size)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeSymlink {
			// Links are delayed so later file writes never traverse an archive-owned symlink.
			// 延迟创建链接，避免后续文件写入穿过归档自带的符号链接。
			if err := validateTarLink(header.Name, header.Linkname, root); err != nil {
				return err
			}
			links = append(links, tarLink{name: path, target: header.Linkname})
			continue
		}
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := writeFile(path, reader, size, os.FileMode(header.Mode)); err != nil {
			return err
		}
	}
}

// validateTarLink checks that a relative link target stays below the expected archive root.
// validateTarLink 检查相对链接目标仍位于预期归档根目录内。
func validateTarLink(name, linkTarget, root string) error {
	if linkTarget == "" || strings.HasPrefix(linkTarget, "/") || strings.Contains(linkTarget, "\\") || strings.ContainsRune(linkTarget, 0) {
		return fmt.Errorf("unsafe TAR symbolic link %q -> %q", name, linkTarget)
	}
	resolved := path.Clean(path.Join(path.Dir(name), linkTarget))
	if resolved == root || !strings.HasPrefix(resolved, root+"/") {
		return fmt.Errorf("TAR symbolic link leaves expected root: %q -> %q", name, linkTarget)
	}
	return nil
}

// createTarLinks materializes validated links and confirms their final targets remain in the extracted tree.
// createTarLinks 创建经校验的链接，并确认最终目标仍在解压目录内。
func createTarLinks(destination string, links []tarLink) error {
	resolvedRoot, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return err
	}
	for _, link := range links {
		parent := filepath.Dir(link.name)
		relativeParent, err := filepath.Rel(destination, parent)
		if err != nil {
			return err
		}
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err != nil || resolvedParent != filepath.Join(resolvedRoot, relativeParent) {
			return fmt.Errorf("TAR symbolic link parent is not a real directory: %q", link.name)
		}
		if err := os.Symlink(link.target, link.name); err != nil {
			return err
		}
	}
	for _, link := range links {
		resolved, err := filepath.EvalSymlinks(link.name)
		if err != nil {
			return fmt.Errorf("TAR symbolic link has no target: %q: %w", link.name, err)
		}
		relative, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("TAR symbolic link escapes extraction: %q", link.name)
		}
	}
	return nil
}
