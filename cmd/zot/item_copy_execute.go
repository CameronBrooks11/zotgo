package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// executeItemCopy creates one planned item and everything under it.
//
// Creation is strictly sequential and top-down, because each level needs the key
// the level above was assigned: a note is reparented onto the new item, an
// annotation onto the new attachment. Nothing here can be batched across levels.
//
// Partial success is an ordinary outcome, not an edge case — an item's metadata
// can land while a file upload fails — so every object reports its own status and
// nothing is deleted on failure. That follows attachment import, which made the
// same choice for the same reason: automatic rollback of a partly-written tree is
// more dangerous than leaving it visible.
func executeItemCopy(ctx context.Context, c *zotero.Client, source, target zotero.LibraryRef, index int, planned plannedItem, collections []string) output.ItemCopy {
	record := output.ItemCopy{
		Index:     index,
		Operation: output.OpCopy,
		SourceKey: planned.sourceKey,
		Type:      planned.itemType,
		Title:     planned.title,
		Children:  []output.CopiedChild{},
	}

	payload, err := copyPayload(planned.data, "", collections)
	if err != nil {
		record.Status = output.StatusFailed
		record.Failure = &output.Failure{Code: output.CodeInvalid, Message: err.Error()}
		return record
	}
	itemKey, err := createOne(ctx, c, target, payload)
	if err != nil {
		record.Status = output.StatusFailed
		record.Failure = copyFailure("Zotero did not create the item", err)
		return record
	}
	record.Key = itemKey
	record.Status = output.StatusCreated

	for i, child := range planned.children {
		record.Children = append(record.Children, copyChild(ctx, c, source, target, i, child, itemKey))
	}

	// The item exists either way; what varies is whether everything under it
	// arrived. Saying "created" over a tree with failures in it would be the same
	// lie this command was written to stop telling.
	if anyChildFailed(record.Children) {
		record.Status = output.StatusPartial
	}
	return record
}

func copyChild(ctx context.Context, c *zotero.Client, source, target zotero.LibraryRef, index int, planned plannedChild, parentKey string) output.CopiedChild {
	record := output.CopiedChild{
		Index:     index,
		Kind:      planned.kind,
		SourceKey: planned.sourceKey,
		Title:     planned.title,
	}
	if planned.skipReason != "" && planned.kind == "attachment" && !planned.managed {
		// Skipped entirely: excluded by a flag, so not even the metadata copies.
		record.Status = output.StatusSkipped
		record.Reason = planned.skipReason
		return record
	}

	payload, err := copyPayload(planned.data, parentKey, nil)
	if err != nil {
		record.Status = output.StatusFailed
		record.Failure = &output.Failure{Code: output.CodeInvalid, Message: err.Error()}
		return record
	}
	childKey, err := createOne(ctx, c, target, payload)
	if err != nil {
		record.Status = output.StatusFailed
		record.Failure = copyFailure(fmt.Sprintf("Zotero did not create the %s", planned.kind), err)
		return record
	}
	record.Key = childKey
	record.Status = output.StatusCreated

	if planned.kind != "attachment" {
		return record
	}

	if planned.managed {
		record.File = copyAttachmentBytes(ctx, c, source, target, planned, childKey)
		if record.File != nil && record.File.Status == output.StatusFailed {
			// The attachment item exists but is empty. Naming that partial is the
			// difference between a user re-attaching one file and not knowing to.
			record.Status = output.StatusPartial
		}
	} else if planned.skipReason != "" {
		record.Reason = planned.skipReason
	}

	for i, annotation := range planned.children {
		record.Children = append(record.Children, copyChild(ctx, c, source, target, i, annotation, childKey))
	}
	if anyChildFailed(record.Children) && record.Status == output.StatusCreated {
		record.Status = output.StatusPartial
	}
	return record
}

// copyAttachmentBytes carries one managed file from the source library to the
// attachment just created in the target. Zotero does not stream bytes, so this is
// "ask where the source file is, read it off disk, upload it" — which is why the
// whole command is local-only.
func copyAttachmentBytes(ctx context.Context, c *zotero.Client, source, target zotero.LibraryRef, planned plannedChild, attachmentKey string) *output.CopiedFile {
	file := &output.CopiedFile{Status: output.StatusFailed}

	path, err := c.AttachmentLocalPath(ctx, source, planned.sourceKey)
	if err != nil {
		if errors.Is(err, zotero.ErrAttachmentHasNoFile) {
			// Not a failure: Zotero says there are no bytes here. The metadata
			// copied, and saying so is more useful than an error.
			file.Status = output.StatusSkipped
			file.Stage = "resolve"
			return file
		}
		file.Stage = "resolve"
		file.Failure = copyFailure("could not locate the source file", err)
		return file
	}

	staged, err := stageAttachmentFile(path, "", "")
	if err != nil {
		file.Stage = "stage"
		file.Failure = &output.Failure{Code: output.CodeStagedFileFailed, Message: err.Error()}
		return file
	}
	defer staged.close()
	file.Filename, file.Size = staged.filename, staged.size

	authorization, err := c.AuthorizeAttachmentUpload(ctx, target, attachmentKey, zotero.AttachmentUploadMetadata{
		MD5: staged.md5, Filename: staged.filename, Size: staged.size,
		MTime: staged.mtime, ContentType: staged.contentType,
	})
	if err != nil {
		file.Stage = "authorize"
		file.Failure = copyFailure("Zotero did not authorize the file upload", err)
		return file
	}
	file.Stage = "authorized"

	if authorization.Exists {
		// Zotero already holds these exact bytes, so there is nothing to send.
		file.Status = output.StatusDuplicate
		file.Stage = "deduplicated"
		return file
	}

	if _, err := staged.file.Seek(0, io.SeekStart); err != nil {
		file.Stage = "stage"
		file.Failure = &output.Failure{Code: output.CodeStagedFileFailed, Message: "could not rewind the staged file"}
		return file
	}
	if err := c.UploadAuthorizedAttachment(ctx, authorization, staged.file, staged.size); err != nil {
		file.Stage = "upload"
		file.Failure = copyFailure("Zotero did not accept the file bytes", err)
		return file
	}
	file.Stage = "uploaded"

	if err := c.RegisterAttachmentUpload(ctx, target, attachmentKey, authorization.UploadKey); err != nil {
		file.Stage = "register"
		file.Failure = copyFailure("Zotero did not register the uploaded file", err)
		return file
	}
	file.Status = output.StatusImported
	file.Stage = "registered"
	return file
}

// createOne writes a single object and returns the key Zotero assigned. Every
// level of a copy needs its key before the level below can be written, so the
// batch API's throughput is of no use here.
func createOne(ctx context.Context, c *zotero.Client, library zotero.LibraryRef, payload json.RawMessage) (string, error) {
	result, err := c.CreateItemsReturningKeys(ctx, zotero.OpItemCopy, library, []json.RawMessage{payload})
	if err != nil {
		return "", err
	}
	for _, key := range result.Successful {
		return key, nil
	}
	for _, failure := range result.Failed {
		return "", fmt.Errorf("%s", failure.Message)
	}
	//lint:ignore ST1005 "Zotero" is a proper noun.
	return "", errors.New("Zotero accepted the write but returned no key")
}

func anyChildFailed(children []output.CopiedChild) bool {
	for _, child := range children {
		if child.Status == output.StatusFailed || child.Status == output.StatusPartial {
			return true
		}
	}
	return false
}

// copyFailure keeps Zotero's own wording out of user-facing output. Its errors
// have been observed to carry raw SQL, including the statement and its bound
// parameters, which is not something to relay.
func copyFailure(message string, err error) *output.Failure {
	failure := &output.Failure{Code: output.CodeInvalid, Message: message}
	var status zotero.StatusError
	if errors.As(err, &status) {
		failure.HTTPStatus = status.StatusCode
	}
	return failure
}
