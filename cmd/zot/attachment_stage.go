package main

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

type stagedAttachment struct {
	file        *os.File
	filename    string
	contentType string
	size        int64
	mtime       int64
	md5         string
}

func (s *stagedAttachment) close() {
	name := s.file.Name()
	_ = s.file.Close()
	_ = os.Remove(name)
}

// stageAttachmentFile copies one stable source snapshot into private storage.
// Every later phase hashes, identifies, and uploads these exact bytes.
func stageAttachmentFile(sourcePath, filenameOverride, contentTypeOverride string) (*stagedAttachment, error) {
	contentTypeOverride, err := validateAttachmentContentType(contentTypeOverride)
	if err != nil {
		return nil, err
	}
	pathInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, errors.New("attachment source is unavailable")
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, errors.New("attachment source must be a regular file")
	}
	source, err := openAttachmentSource(sourcePath)
	if err != nil {
		return nil, errors.New("attachment source could not be opened")
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil {
		return nil, errors.New("attachment source could not be inspected")
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("attachment source must be a regular file")
	}
	if before.Size() == 0 {
		return nil, errors.New("attachment source is empty")
	}
	if before.Size() > zotero.MaxAttachmentFileSize {
		return nil, fmt.Errorf("attachment source exceeds zotgo's %d-byte managed-upload safety limit", zotero.MaxAttachmentFileSize)
	}
	filename, err := attachmentImportFilename(sourcePath, filenameOverride)
	if err != nil {
		return nil, err
	}

	staged, err := os.CreateTemp("", "zotgo-attachment-import-*")
	if err != nil {
		return nil, fmt.Errorf("create private attachment staging file: %w", err)
	}
	cleanup := func() {
		name := staged.Name()
		_ = staged.Close()
		_ = os.Remove(name)
	}
	hash := md5.New() // #nosec G401 -- Zotero's upload protocol requires MD5.
	size, err := io.Copy(io.MultiWriter(staged, hash), io.LimitReader(source, zotero.MaxAttachmentFileSize+1))
	if err != nil {
		cleanup()
		return nil, errors.New("attachment source could not be staged")
	}
	after, err := source.Stat()
	if err != nil {
		cleanup()
		return nil, errors.New("attachment source could not be reinspected")
	}
	if size != before.Size() || size > zotero.MaxAttachmentFileSize ||
		after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		cleanup()
		return nil, errors.New("attachment source changed while it was being staged")
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, fmt.Errorf("rewind staged attachment: %w", err)
	}
	header := make([]byte, 512)
	n, err := staged.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		cleanup()
		return nil, fmt.Errorf("inspect staged attachment: %w", err)
	}
	contentType := http.DetectContentType(header[:n])
	if contentTypeOverride != "" {
		contentType = contentTypeOverride
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, fmt.Errorf("rewind staged attachment: %w", err)
	}
	mtime := before.ModTime().UnixMilli()
	if mtime < 0 {
		cleanup()
		return nil, errors.New("attachment source modification time predates 1970")
	}
	return &stagedAttachment{
		file: staged, filename: filename, contentType: contentType,
		size: size, mtime: mtime, md5: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func validateAttachmentContentType(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return "", fmt.Errorf("invalid --content-type MIME type: %w", err)
	}
	typeName, subtype, ok := strings.Cut(mediaType, "/")
	if !ok || typeName == "" || subtype == "" || strings.Contains(typeName, "*") || strings.Contains(subtype, "*") {
		return "", errors.New("invalid --content-type MIME type: expected a concrete type/subtype")
	}
	return mime.FormatMediaType(mediaType, params), nil
}

func attachmentImportFilename(sourcePath, override string) (string, error) {
	filename := override
	if filename == "" {
		filename = filepath.Base(filepath.Clean(sourcePath))
	}
	if err := validateManagedFilename(filename); err != nil {
		return "", err
	}
	return filename, nil
}

func validateManagedFilename(filename string) error {
	if filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\\`) {
		return errors.New("managed filename must be a bare filename")
	}
	if !utf8.ValidString(filename) || len([]byte(filename)) > 255 {
		return errors.New("managed filename must be valid UTF-8 and no more than 255 bytes")
	}
	if strings.HasSuffix(filename, " ") || strings.HasSuffix(filename, ".") {
		return errors.New("managed filename must not end with a space or dot")
	}
	for _, r := range filename {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*`, r) {
			return fmt.Errorf("managed filename contains unsupported character %q", r)
		}
	}
	base := filename
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	reserved := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"CLOCK$": true, "CONIN$": true, "CONOUT$": true,
		"COM¹": true, "COM²": true, "COM³": true,
		"LPT¹": true, "LPT²": true, "LPT³": true,
	}
	upper := strings.ToUpper(base)
	if reserved[upper] || (len(upper) == 4 &&
		(strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9') {
		return fmt.Errorf("managed filename %q is reserved on Windows", filename)
	}
	return nil
}

func validateAttachmentSourceURL(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("--source-url must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return errors.New("--source-url must not contain credentials")
	}
	return nil
}
