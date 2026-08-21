package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func attachmentImportCommand() *cli.Command {
	return &cli.Command{
		Name:  "import",
		Usage: "attach a local file as a Zotero-managed file",
		Description: "Creates an imported_file child, uploads the file through Zotero's Local API, and verifies managed metadata. " +
			"The MIME type is detected from the staged bytes unless --content-type overrides it. --source-url stores provenance and is not downloaded. " +
			"Managed imports are local-only, capped at 128 MiB, and require 'Always Allow' authorization.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "parent", Usage: "existing bibliographic parent item key"},
			&cli.StringFlag{Name: "file", Usage: "local regular-file path"},
			&cli.StringFlag{Name: "title", Value: "Attachment", Usage: "attachment title"},
			&cli.StringFlag{Name: "source-url", Usage: "source/provenance URL stored on the attachment (not downloaded)"},
			&cli.StringFlag{Name: "filename", Usage: "managed filename (default: local basename)"},
			&cli.StringFlag{Name: "content-type", Usage: "MIME type override (default: detect from staged bytes)"},
			&cli.BoolFlag{Name: "allow-duplicate", Usage: "create another attachment even when this parent has the same MD5"},
			&cli.BoolFlag{Name: "dry-run", Usage: "validate and show every planned phase without authorizing or writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation"},
		},
		Action: attachmentImportAction,
	}
}

func attachmentImportAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "attachment import")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("managed attachment import is local-only; the --web profile is read-only")
	}
	parentKey := strings.TrimSpace(cmd.String("parent"))
	if parentKey == "" {
		return errors.New("missing --parent item key; see `zot attachment import --help`")
	}
	sourcePath := cmd.String("file")
	if sourcePath == "" {
		return errors.New("missing --file path; see `zot attachment import --help`")
	}
	title := strings.TrimSpace(cmd.String("title"))
	if title == "" {
		return errors.New("attachment title must not be empty")
	}
	sourceURL := strings.TrimSpace(cmd.String("source-url"))
	if err := validateAttachmentSourceURL(sourceURL); err != nil {
		return err
	}
	staged, err := stageAttachmentFile(sourcePath, cmd.String("filename"), cmd.String("content-type"))
	if err != nil {
		return err
	}
	defer staged.close()

	client, library, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	libraryDTO := output.NewLibrary(library)
	if err := validateAttachmentParent(ctx, client, library, parentKey); err != nil {
		return err
	}
	duplicate, err := findDuplicateAttachment(ctx, client, library, parentKey, staged.md5, staged.size)
	if err != nil {
		return err
	}

	record := output.AttachmentImport{
		Status: "planned", Stage: "preflight", ParentKey: parentKey,
		Filename: staged.filename, ContentType: staged.contentType,
		Size: staged.size, MD5: staged.md5,
	}
	if duplicate != nil && !cmd.Bool("allow-duplicate") {
		record.Status = "duplicate"
		record.AttachmentKey = &duplicate.Key
		record.Filename = duplicate.Filename
		record.FileStatus = attachmentImportFileStatus(*duplicate)
		return emitAttachmentImport(cmd, mode, libraryDTO, record)
	}
	if cmd.Bool("dry-run") {
		return emitAttachmentImport(cmd, mode, libraryDTO, record)
	}

	// The plan block informs the interactive confirmation. With --yes there is no
	// prompt, so it would only pre-echo fields the outcome block already reports —
	// duplicated noise on a preflight failure (#82). Print it only when prompting.
	if mode == output.ModeHuman && !cmd.Bool("yes") {
		fmt.Fprintf(out(cmd), "Target: %s - %s\n", library.Name, client.BaseURL())
		fmt.Fprintf(out(cmd), "Parent: %s\nFile: %s (%d bytes)\nMD5: %s\n",
			parentKey, staged.filename, staged.size, staged.md5)
		fmt.Fprintln(out(cmd), "Plan: create metadata -> authorize upload -> upload bytes -> register -> verify")
		if duplicate != nil {
			fmt.Fprintf(out(cmd), "Warning: attachment %s has the same MD5; --allow-duplicate was set.\n", duplicate.Key)
		}
		if !confirm(os.Stdin, out(cmd), fmt.Sprintf("Import %s under %s?", staged.filename, parentKey)) {
			fmt.Fprintln(out(cmd), "Aborted.")
			return nil
		}
	}

	if err := ensureImportWriteAuthority(ctx, cmd, client, mode); err != nil {
		record.Status = "failed"
		// The authority errors (missing/expired/out-of-scope lease, no write API)
		// are curated, actionable messages — surface them rather than a generic line.
		return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
			"authorization-required", err.Error())
	}

	attachmentKey, err := createImportedAttachmentMetadata(ctx, client, library, parentKey, title, sourceURL, staged.contentType)
	if err != nil {
		if errors.Is(err, zotero.ErrWriteOutcomeUnknown) {
			record.Status = "partial"
			record.Stage = "metadata-create-unknown"
			return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
				"metadata-create-unknown", "Zotero may have created attachment metadata, but its key could not be determined")
		}
		record.Status = "failed"
		return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
			"metadata-create-failed", safeImportFailure("Zotero did not create the attachment metadata", err))
	}
	record.AttachmentKey = &attachmentKey
	record.Status = "partial"
	record.Stage = "metadata-created"

	metadata := zotero.AttachmentUploadMetadata{
		MD5: staged.md5, Filename: staged.filename, Size: staged.size,
		MTime: staged.mtime, ContentType: staged.contentType,
	}
	authorization, err := client.AuthorizeAttachmentUpload(ctx, library, attachmentKey, metadata)
	if err != nil {
		return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
			"upload-authorize-failed", safeImportFailure("Zotero did not authorize the file upload", err))
	}
	record.Stage = "authorized"

	if !authorization.Exists {
		if _, err := staged.file.Seek(0, io.SeekStart); err != nil {
			return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
				"staged-file-failed", "could not rewind the staged attachment")
		}
		if err := client.UploadAuthorizedAttachment(ctx, authorization, staged.file, staged.size); err != nil {
			return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
				"upload-failed", safeImportFailure("Zotero did not accept the file bytes", err))
		}
		record.Stage = "uploaded"
		registerErr := client.RegisterAttachmentUpload(ctx, library, attachmentKey, authorization.UploadKey)
		if registerErr == nil {
			record.Stage = "registered"
		}
		attachment, verification, verifyErr := verifyImportedAttachment(
			ctx, client, library, attachmentKey, parentKey, title, sourceURL, *staged)
		setAttachmentImportVerification(&record, attachment, verification)
		if verification.OK() {
			record.Status = "imported"
			record.Stage = "verified"
			record.Filename = attachment.Filename
			if registerErr != nil {
				fmt.Fprintln(errOut(cmd), "zot: warning: registration response failed, but focused verification confirmed the import")
			}
			return emitAttachmentImport(cmd, mode, libraryDTO, record)
		}
		if registerErr != nil {
			return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
				"register-failed", safeImportFailure("Zotero did not confirm file registration", registerErr))
		}
		return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
			"verification-failed", safeImportFailure("the registered attachment failed focused verification", verifyErr))
	}

	attachment, verification, verifyErr := verifyImportedAttachment(
		ctx, client, library, attachmentKey, parentKey, title, sourceURL, *staged)
	setAttachmentImportVerification(&record, attachment, verification)
	if !verification.OK() {
		return finishAttachmentImportFailure(cmd, mode, libraryDTO, record,
			"verification-failed", safeImportFailure("Zotero reported existing bytes but verification failed", verifyErr))
	}
	record.Status = "imported"
	record.Stage = "verified"
	record.Filename = attachment.Filename
	return emitAttachmentImport(cmd, mode, libraryDTO, record)
}

func validateAttachmentParent(ctx context.Context, client *zotero.Client, library zotero.LibraryRef, parentKey string) error {
	raw, err := client.RawItem(ctx, library, parentKey)
	if err != nil {
		if errors.Is(err, zotero.ErrNotFound) {
			return fmt.Errorf("no parent item with key %q in %s", parentKey, library.Name)
		}
		return friendly(err)
	}
	identity, err := zotero.DecodeItemIdentity(raw)
	if err != nil {
		return fmt.Errorf("decode parent item %q: %w", parentKey, err)
	}
	switch identity.ItemType {
	case "attachment", "note", "annotation":
		return fmt.Errorf("item %q has type %q and cannot be a bibliographic attachment parent", parentKey, identity.ItemType)
	}
	return nil
}

func findDuplicateAttachment(ctx context.Context, client *zotero.Client, library zotero.LibraryRef, parentKey, checksum string, size int64) (*zotero.Attachment, error) {
	// Read only the parent's direct attachment children, via the children route.
	// The unfiltered /items route drops an itemKey scope the moment itemType is
	// added (verified on Zotero 9.0.6: itemKey=K&itemType=attachment returns the
	// whole library, not K's children), which made this scan walk every
	// attachment and abort on the first one under a different parent (#80). The
	// children route scopes correctly and is the same transport `zot show` uses.
	raws, err := client.AllRawChildItems(ctx, library, parentKey, zotero.ChildItemsOptions{
		ItemType: "attachment", Limit: 100,
	})
	if err != nil {
		return nil, friendly(err)
	}
	matches := make([]zotero.Attachment, 0)
	for i, raw := range raws {
		var envelope zotero.Envelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("decode child attachment %d: %w", i, err)
		}
		attachment, err := envelope.AttachmentData()
		if err != nil {
			return nil, fmt.Errorf("decode child attachment %d: %w", i, err)
		}
		// The children route already scopes to this parent; skip anything foreign
		// defensively rather than aborting, so a future transport change can never
		// resurrect #80 as a hard failure.
		if attachment.ParentKey != parentKey {
			continue
		}
		if enclosure, ok := envelope.Links["enclosure"]; ok && enclosure.Href != "" {
			attachment.Enclosure = &zotero.AttachmentEnclosure{
				Href: enclosure.Href, Type: enclosure.Type, Title: enclosure.Title, Length: enclosure.Length,
			}
		}
		if attachment.MD5 != nil && strings.EqualFold(*attachment.MD5, checksum) {
			if attachment.Enclosure != nil && attachment.Enclosure.Length != nil && *attachment.Enclosure.Length != size {
				return nil, fmt.Errorf("attachment %q has matching MD5 but conflicting size metadata", attachment.Key)
			}
			matches = append(matches, attachment)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Key < matches[j].Key })
	return &matches[0], nil
}

// ensureImportWriteAuthority establishes authority for a managed import. An
// interactive human is their own authority but must choose "Always Allow" (the
// import performs several authenticated phases, and a single-use key would be
// spent on the first). A non-interactive import requires a write lease permitting
// attachment.import; its bound key is persistent, so the multi-phase flow works.
func ensureImportWriteAuthority(ctx context.Context, cmd *cli.Command, client *zotero.Client, mode output.Mode) error {
	if err := client.RequireWriteCapability(ctx); err != nil {
		return writeFriendly(err)
	}
	if writeIsInteractive(cmd, mode) {
		client.SetWriteAuthorizer(zotero.AllowAllWrites())
		return ensureRememberedLocalKey(ctx, cmd, client)
	}
	return installLeaseAuthority(client)
}

func ensureRememberedLocalKey(ctx context.Context, cmd *cli.Command, client *zotero.Client) error {
	if k := loadLocalKey(); k != "" {
		client.SetLocalKey(k)
	}
	if client.HasLocalKey() {
		return nil
	}
	fmt.Fprintln(errOut(cmd), "Authorizing with Zotero - choose 'Always Allow' in the app...")
	remember, err := client.Authorize(ctx, "zotgo")
	if err != nil {
		return writeFriendly(err)
	}
	if !remember {
		return errors.New("attachment import requires 'Always Allow' because it performs multiple authenticated writes")
	}
	if err := saveLocalKey(client.LocalKey()); err != nil {
		fmt.Fprintf(errOut(cmd), "zot: warning: could not save the remembered local key: %v\n", err)
	}
	return nil
}

func createImportedAttachmentMetadata(ctx context.Context, client *zotero.Client, library zotero.LibraryRef, parentKey, title, sourceURL, contentType string) (string, error) {
	item := map[string]any{
		"itemType": "attachment", "parentItem": parentKey,
		"linkMode": "imported_file", "title": title, "contentType": contentType,
	}
	if sourceURL != "" {
		item["url"] = sourceURL
	}
	body, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("encode attachment metadata: %w", err)
	}
	result, err := client.CreateItemsReturningKeys(ctx, zotero.OpAttachmentImport, library, []json.RawMessage{body})
	if err != nil {
		return "", writeFriendly(err)
	}
	if len(result.Failed) != 0 {
		failure, failedAtZero := result.Failed["0"]
		if !failedAtZero || len(result.Failed) != 1 || len(result.Successful) != 0 || len(result.Unchanged) != 0 {
			return "", fmt.Errorf("%w: conflicting attachment create response", zotero.ErrWriteOutcomeUnknown)
		}
		if failure.Message == "" {
			return "", errors.New("attachment metadata was rejected by Zotero")
		}
		return "", fmt.Errorf("attachment metadata was rejected by Zotero: %s", failure.Message)
	}
	createdKey, ok := result.Successful["0"]
	if !ok || createdKey == "" || len(result.Successful) != 1 || len(result.Unchanged) != 0 {
		return "", fmt.Errorf("%w: unexpected attachment create response", zotero.ErrWriteOutcomeUnknown)
	}
	return createdKey, nil
}

func verifyImportedAttachment(ctx context.Context, client *zotero.Client, library zotero.LibraryRef, key, parentKey, title, sourceURL string, staged stagedAttachment) (zotero.Attachment, output.AttachmentImportVerification, error) {
	raw, err := client.RawItem(ctx, library, key)
	if err != nil {
		return zotero.Attachment{}, output.AttachmentImportVerification{}, friendly(err)
	}
	if err := zotero.RequireItemType(raw, "attachment"); err != nil {
		return zotero.Attachment{}, output.AttachmentImportVerification{}, fmt.Errorf("decode imported attachment %q: %w", key, err)
	}
	attachment, err := zotero.DecodeAttachment(raw)
	if err != nil {
		return zotero.Attachment{}, output.AttachmentImportVerification{}, fmt.Errorf("decode imported attachment %q: %w", key, err)
	}
	verification := output.AttachmentImportVerification{
		Parent:         attachment.ParentKey == parentKey,
		ManagedStorage: attachment.LinkMode == "imported_file" && attachment.Enclosure != nil,
		Title:          attachment.Title == title,
		SourceURL:      attachment.URL == sourceURL,
		Filename:       attachment.Filename == staged.filename,
		ContentType:    attachment.ContentType == staged.contentType,
		Size:           attachment.Enclosure != nil && attachment.Enclosure.Length != nil && *attachment.Enclosure.Length == staged.size,
		Checksum:       attachment.MD5 != nil && strings.EqualFold(*attachment.MD5, staged.md5),
		ActualFilename: attachment.Filename,
	}
	if verification.OK() {
		return attachment, verification, nil
	}
	var mismatches []string
	checks := []struct {
		name string
		ok   bool
	}{
		{"parent", verification.Parent}, {"managed storage", verification.ManagedStorage},
		{"title", verification.Title}, {"source URL", verification.SourceURL},
		{"filename", verification.Filename}, {"content type", verification.ContentType},
		{"size", verification.Size}, {"checksum", verification.Checksum},
	}
	for _, check := range checks {
		if !check.ok {
			mismatches = append(mismatches, check.name)
		}
	}
	return attachment, verification, fmt.Errorf("verification mismatch: %s", strings.Join(mismatches, ", "))
}

func setAttachmentImportVerification(record *output.AttachmentImport, attachment zotero.Attachment, verification output.AttachmentImportVerification) {
	if attachment.Key == "" {
		return
	}
	record.FileStatus = attachmentImportFileStatus(attachment)
	record.Verification = &verification
}

func attachmentImportFileStatus(attachment zotero.Attachment) *output.AttachmentImportFileStatus {
	status := &output.AttachmentImportFileStatus{}
	switch attachment.LinkMode {
	case "linked_url":
		status.State, status.Reason = "not-applicable", "attachment links to a URL and has no managed file"
	case "linked_file":
		status.State, status.Reason = "linked-unverified", "the API provides no portable existence check for linked files"
	case "imported_file", "imported_url":
		switch {
		case attachment.Enclosure == nil:
			status.State, status.Reason = "unavailable", "Zotero did not advertise a file location"
		case attachment.Enclosure.Length != nil:
			status.State, status.Reason = "metadata-available", "Zotero advertised a file location and size metadata"
		default:
			status.State, status.Reason = "location-advertised", "Zotero advertised a file location without size metadata"
		}
	default:
		status.State, status.Reason = "unknown", "unrecognized attachment link mode"
	}
	return status
}

func safeImportFailure(prefix string, err error) string {
	var statusErr zotero.StatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("%s (HTTP %d)", prefix, statusErr.StatusCode)
	}
	switch {
	case errors.Is(err, zotero.ErrWriteUnauthorized):
		return prefix + " (authorization was rejected or expired)"
	case errors.Is(err, zotero.ErrPreconditionFailed):
		return prefix + " (the attachment changed concurrently)"
	case errors.Is(err, zotero.ErrPreconditionRequired):
		return prefix + " (Zotero required a missing precondition)"
	default:
		return prefix
	}
}

func finishAttachmentImportFailure(cmd *cli.Command, mode output.Mode, library *output.Library, record output.AttachmentImport, code, message string) error {
	record.Failure = &output.AttachmentImportFailure{Code: code, Message: message}
	if err := emitAttachmentImport(cmd, mode, library, record); err != nil {
		return err
	}
	return cli.Exit("", 1)
}

func emitAttachmentImport(cmd *cli.Command, mode output.Mode, library *output.Library, record output.AttachmentImport) error {
	if mode == output.ModeHuman {
		w := out(cmd)
		fmt.Fprintf(w, "Status: %s\nStage: %s\nParent: %s\n", record.Status, record.Stage, record.ParentKey)
		if record.AttachmentKey != nil {
			fmt.Fprintf(w, "Attachment: %s\n", *record.AttachmentKey)
		}
		fmt.Fprintf(w, "Filename: %s\nContent type: %s\nSize: %d\nMD5: %s\n", record.Filename, record.ContentType, record.Size, record.MD5)
		if record.FileStatus != nil {
			fmt.Fprintf(w, "File status: %s - %s\n", record.FileStatus.State, record.FileStatus.Reason)
		}
		if record.Status == "planned" {
			fmt.Fprintln(w, "Dry run - metadata alone would not contain file bytes; upload and registration are required.")
		}
		if record.Failure != nil {
			fmt.Fprintf(w, "Failure: %s\n", record.Failure.Message)
		}
		return nil
	}
	return emitOne(out(cmd), mode, output.KindAttachmentImport, library, record, nil)
}
