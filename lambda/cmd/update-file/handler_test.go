package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"safe-upload-platform-lambda/internal/clients"
	"safe-upload-platform-lambda/internal/models"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// --- mocks --------------------------------------------------------------

type mockS3 struct {
	deleteObjectFn func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockS3) CreateMultipartUpload(ctx context.Context, p *s3.CreateMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	return &s3.CreateMultipartUploadOutput{}, nil
}
func (m *mockS3) CompleteMultipartUpload(ctx context.Context, p *s3.CompleteMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	return &s3.CompleteMultipartUploadOutput{}, nil
}
func (m *mockS3) DeleteObject(ctx context.Context, p *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, p, opts...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

type mockDynamo struct {
	updateItemFn func(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

func (m *mockDynamo) PutItem(ctx context.Context, p *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}
func (m *mockDynamo) GetItem(ctx context.Context, p *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{}, nil
}
func (m *mockDynamo) UpdateItem(ctx context.Context, p *dynamodb.UpdateItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, p, opts...)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

// --- helpers ------------------------------------------------------------

func newTestHandler(s3c clients.S3API, dyn clients.DynamoAPI) *Handler {
	return &Handler{cfg: &clients.Config{
		S3: s3c, Dynamo: dyn,
		Bucket: "test-bucket", Table: "test-table",
	}}
}

func event(objectKey, versionID, scanStatus string) GuardDutyScanEvent {
	return GuardDutyScanEvent{
		Detail: ScanDetail{
			S3ObjectDetails:   S3Object{ObjectKey: objectKey, VersionID: versionID},
			ScanResultDetails: ScanResultDetails{ScanResultStatus: scanStatus},
		},
	}
}

// --- fileIDFromKey table test -------------------------------------------

func TestFileIDFromKey(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"standard upload path", "uploads/abc-123/file.txt", "abc-123"},
		{"nested path", "uploads/abc-123/sub/file.txt", "abc-123"},
		{"single segment falls back to key", "filename.txt", "filename.txt"},
		{"empty string", "", ""},
		{"leading slash", "/uploads/xyz/file.txt", "uploads"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fileIDFromKey(c.in); got != c.want {
				t.Errorf("fileIDFromKey(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// --- Handle tests -------------------------------------------------------

func TestHandle_NoThreatsFound(t *testing.T) {
	var updateCalled bool
	var statusValue string
	dyn := &mockDynamo{
		updateItemFn: func(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			updateCalled = true
			if v, ok := in.ExpressionAttributeValues[":status"].(*dynamotypes.AttributeValueMemberS); ok {
				statusValue = v.Value
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	s3m := &mockS3{
		deleteObjectFn: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			t.Error("DeleteObject must not be called on NO_THREATS_FOUND")
			return nil, nil
		},
	}
	h := newTestHandler(s3m, dyn)

	if err := h.Handle(context.Background(), event("uploads/abc/file.txt", "v1", "NO_THREATS_FOUND")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !updateCalled {
		t.Error("UpdateItem was not called")
	}
	if statusValue != models.StatusClean {
		t.Errorf("status: got %q, want %q", statusValue, models.StatusClean)
	}
}

func TestHandle_ThreatsFound_HappyPath(t *testing.T) {
	var deleteCalled, updateCalled bool
	var capturedVersionID, capturedKey, capturedBucket string
	s3m := &mockS3{
		deleteObjectFn: func(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			deleteCalled = true
			capturedVersionID = *in.VersionId
			capturedKey = *in.Key
			capturedBucket = *in.Bucket
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	var statusValue string
	var hasExpires bool
	dyn := &mockDynamo{
		updateItemFn: func(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			updateCalled = true
			if v, ok := in.ExpressionAttributeValues[":status"].(*dynamotypes.AttributeValueMemberS); ok {
				statusValue = v.Value
			}
			_, hasExpires = in.ExpressionAttributeValues[":expires"]
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	h := newTestHandler(s3m, dyn)

	if err := h.Handle(context.Background(), event("uploads/abc/file.txt", "v42", "THREATS_FOUND")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !deleteCalled {
		t.Error("DeleteObject was not called")
	}
	if capturedVersionID != "v42" {
		t.Errorf("VersionId: got %q, want v42", capturedVersionID)
	}
	if capturedKey != "uploads/abc/file.txt" {
		t.Errorf("Key: got %q", capturedKey)
	}
	if capturedBucket != "test-bucket" {
		t.Errorf("Bucket: got %q, want test-bucket", capturedBucket)
	}
	if !updateCalled {
		t.Error("UpdateItem was not called")
	}
	if statusValue != models.StatusDeleted {
		t.Errorf("status: got %q, want %q", statusValue, models.StatusDeleted)
	}
	if !hasExpires {
		t.Error("expires_at TTL was not set on deleted record")
	}
}

func TestHandle_ThreatsFound_MissingVersionId(t *testing.T) {
	s3m := &mockS3{
		deleteObjectFn: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			t.Error("DeleteObject must not be called without versionId")
			return nil, nil
		},
	}
	dyn := &mockDynamo{
		updateItemFn: func(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			t.Error("UpdateItem must not be called without versionId")
			return nil, nil
		},
	}
	h := newTestHandler(s3m, dyn)

	err := h.Handle(context.Background(), event("uploads/abc/file.txt", "", "THREATS_FOUND"))
	if err == nil {
		t.Fatal("expected error for missing versionId; got nil")
	}
	if !strings.Contains(err.Error(), "missing versionId") {
		t.Errorf("error: %v, want 'missing versionId'", err)
	}
}

func TestHandle_UnknownScanStatus(t *testing.T) {
	s3m := &mockS3{
		deleteObjectFn: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			t.Error("DeleteObject must not be called on unknown status")
			return nil, nil
		},
	}
	dyn := &mockDynamo{
		updateItemFn: func(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			t.Error("UpdateItem must not be called on unknown status")
			return nil, nil
		},
	}
	h := newTestHandler(s3m, dyn)

	if err := h.Handle(context.Background(), event("uploads/abc/file.txt", "v1", "WEIRD_STATUS")); err != nil {
		t.Fatalf("Handle: unexpected error %v", err)
	}
}

func TestHandle_S3DeleteError(t *testing.T) {
	s3m := &mockS3{
		deleteObjectFn: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, errors.New("s3 down")
		},
	}
	dyn := &mockDynamo{
		updateItemFn: func(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			t.Error("UpdateItem must not be called if S3 delete fails")
			return nil, nil
		},
	}
	h := newTestHandler(s3m, dyn)

	err := h.Handle(context.Background(), event("uploads/abc/file.txt", "v1", "THREATS_FOUND"))
	if err == nil {
		t.Fatal("expected error propagating from S3 delete; got nil")
	}
	if !strings.Contains(err.Error(), "failed to delete infected file") {
		t.Errorf("error: %v, want 'failed to delete infected file'", err)
	}
}
