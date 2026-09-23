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
// It returns an error before accepting links, traversal, duplicate entries, or oversized content.
// 遇到链接、路径穿越、重复条目或超大内容时返回错误。
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
	// seen rejects duplicate or case-folded paths on every target.
	// seen 在所有目标平台上拒绝重复或大小写折叠后冲突的路径。
	seen map[string]bool
}

// newTracker creates the bounded extraction accounting state.
// newTracker 创建有界解压计数状态。
func newTracker() *tracker {
	return &tracker{seen: make(map[string]bool)}
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
	key := strings.ToLower(clean)
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
	track := newTracker()
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

// extractTarGzip validates regular files and directories in a gzip compressed TAR archive.
// extractTarGzip 校验 gzip 压缩 TAR 归档中的普通文件和目录。
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
	track := newTracker()
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("unsupported TAR entry %q", header.Name)
		}
		size := header.Size
		if header.Typeflag == tar.TypeDir {
			size = 0
		}
		path, err := track.target(header.Name, destination, root, size)
		if err != nil {
			return err
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
