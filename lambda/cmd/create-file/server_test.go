package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"safe-upload-platform-lambda/internal/clients"
	"safe-upload-platform-lambda/internal/models"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/awslabs/aws-lambda-go-api-proxy/core"
	chimux "github.com/go-chi/chi/v5"
)

// --- mocks --------------------------------------------------------------

type mockS3 struct {
	createMultipartUploadFn   func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	completeMultipartUploadFn func(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	deleteObjectFn            func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockS3) CreateMultipartUpload(ctx context.Context, p *s3.CreateMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	if m.createMultipartUploadFn != nil {
		return m.createMultipartUploadFn(ctx, p, opts...)
	}
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String("mock-upload-id")}, nil
}

func (m *mockS3) CompleteMultipartUpload(ctx context.Context, p *s3.CompleteMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	if m.completeMultipartUploadFn != nil {
		return m.completeMultipartUploadFn(ctx, p, opts...)
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (m *mockS3) DeleteObject(ctx context.Context, p *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, p, opts...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

type mockS3Presign struct {
	presignUploadPartFn func(context.Context, *s3.UploadPartInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	presignGetObjectFn  func(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

func (m *mockS3Presign) PresignUploadPart(ctx context.Context, p *s3.UploadPartInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.presignUploadPartFn != nil {
		return m.presignUploadPartFn(ctx, p, opts...)
	}
	return &v4.PresignedHTTPRequest{URL: "https://example.test/upload"}, nil
}

func (m *mockS3Presign) PresignGetObject(ctx context.Context, p *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.presignGetObjectFn != nil {
		return m.presignGetObjectFn(ctx, p, opts...)
	}
	return &v4.PresignedHTTPRequest{URL: "https://example.test/download"}, nil
}

type mockDynamo struct {
	putItemFn    func(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	getItemFn    func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	updateItemFn func(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

func (m *mockDynamo) PutItem(ctx context.Context, p *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, p, opts...)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamo) GetItem(ctx context.Context, p *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFn != nil {
		return m.getItemFn(ctx, p, opts...)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamo) UpdateItem(ctx context.Context, p *dynamodb.UpdateItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, p, opts...)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

// --- helpers ------------------------------------------------------------

func newTestServer(s3c clients.S3API, s3p clients.S3PresignAPI, dyn clients.DynamoAPI) *Server {
	return &Server{cfg: &clients.Config{
		S3: s3c, S3Presign: s3p, Dynamo: dyn,
		Bucket: "test-bucket", Table: "test-table",
	}}
}

// newProxyReq builds an *http.Request whose context carries the API Gateway
// proxy request context — what core.GetAPIGatewayContextFromContext reads.
// Uses the chi adapter's own helper so the private ctxKey is set correctly.
func newProxyReq(t *testing.T, method, path, body, apiKeyID string) *http.Request {
	t.Helper()
	ra := core.RequestAccessor{}
	req, err := ra.EventToRequestWithContext(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod: method,
		Path:       path,
		Body:       body,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{APIKeyID: apiKeyID},
		},
	})
	if err != nil {
		t.Fatalf("EventToRequestWithContext: %v", err)
	}
	return req
}

// withURLParam adds a chi URL param to the request context so chimux.URLParam
// resolves it inside the handler (mimicking what the chi router does).
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chimux.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chimux.RouteCtxKey, rctx))
}

func mustMarshalRecord(t *testing.T, rec models.FileRecord) map[string]dynamotypes.AttributeValue {
	t.Helper()
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return item
}

// --- CreateFile tests ---------------------------------------------------

func TestCreateFile_Success(t *testing.T) {
	dyn := &mockDynamo{}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := newProxyReq(t, "POST", "/files", `{"name":"smoke.txt","size":12}`, "test-key-id")
	w := httptest.NewRecorder()

	srv.CreateFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp CreateFileResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UploadID != "mock-upload-id" {
		t.Errorf("upload_id: got %q, want mock-upload-id", resp.UploadID)
	}
	if len(resp.Parts) != 1 {
		t.Errorf("parts count: got %d, want 1 (size=12 < 5MB chunk)", len(resp.Parts))
	}
	if resp.ChunkSize != chunkSize {
		t.Errorf("chunk_size: got %d, want %d", resp.ChunkSize, chunkSize)
	}
}

func TestCreateFile_InvalidBody(t *testing.T) {
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, &mockDynamo{})
	req := newProxyReq(t, "POST", "/files", `not json`, "test-key-id")
	w := httptest.NewRecorder()

	srv.CreateFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestCreateFile_MissingFields(t *testing.T) {
	cases := []string{
		`{"name":"","size":12}`,
		`{"name":"x","size":0}`,
		`{"name":"x","size":-1}`,
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, &mockDynamo{})
	for _, body := range cases {
		req := newProxyReq(t, "POST", "/files", body, "test-key-id")
		w := httptest.NewRecorder()
		srv.CreateFile(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%s: got %d, want 400", body, w.Code)
		}
	}
}

func TestCreateFile_NoAPIKeyID(t *testing.T) {
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, &mockDynamo{})
	req := newProxyReq(t, "POST", "/files", `{"name":"x","size":1}`, "") // empty apiKeyID
	w := httptest.NewRecorder()

	srv.CreateFile(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", w.Code)
	}
}

func TestCreateFile_S3Error(t *testing.T) {
	s3m := &mockS3{
		createMultipartUploadFn: func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			return nil, errors.New("s3 boom")
		},
	}
	srv := newTestServer(s3m, &mockS3Presign{}, &mockDynamo{})
	req := newProxyReq(t, "POST", "/files", `{"name":"x","size":1}`, "test-key-id")
	w := httptest.NewRecorder()

	srv.CreateFile(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestCreateFile_DynamoError(t *testing.T) {
	dyn := &mockDynamo{
		putItemFn: func(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("dynamo boom")
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := newProxyReq(t, "POST", "/files", `{"name":"x","size":1}`, "test-key-id")
	w := httptest.NewRecorder()

	srv.CreateFile(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

// --- DownloadFile tests -------------------------------------------------

func TestDownloadFile_Success(t *testing.T) {
	rec := models.FileRecord{ID: "abc", Name: "f.txt", Size: 12, Status: models.StatusClean, S3Key: "uploads/abc/f.txt"}
	dyn := &mockDynamo{
		getItemFn: func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: mustMarshalRecord(t, rec)}, nil
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := withURLParam(newProxyReq(t, "GET", "/files/abc", "", "test-key-id"), "id", "abc")
	w := httptest.NewRecorder()

	srv.DownloadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp DownloadFileResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.URL == "" {
		t.Error("URL: empty, want presigned URL")
	}
	if resp.ID != "abc" {
		t.Errorf("id: got %q, want abc", resp.ID)
	}
}

func TestDownloadFile_StatusNotClean(t *testing.T) {
	rec := models.FileRecord{ID: "abc", Status: models.StatusPendingScan}
	dyn := &mockDynamo{
		getItemFn: func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: mustMarshalRecord(t, rec)}, nil
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := withURLParam(newProxyReq(t, "GET", "/files/abc", "", "test-key-id"), "id", "abc")
	w := httptest.NewRecorder()

	srv.DownloadFile(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", w.Code)
	}
}

func TestDownloadFile_NotFound(t *testing.T) {
	dyn := &mockDynamo{
		getItemFn: func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := withURLParam(newProxyReq(t, "GET", "/files/abc", "", "test-key-id"), "id", "abc")
	w := httptest.NewRecorder()

	srv.DownloadFile(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

// --- CompleteUpload tests -----------------------------------------------

func TestCompleteUpload_HappyPath(t *testing.T) {
	rec := models.FileRecord{ID: "abc", Status: models.StatusPendingUpload, UploadID: "u1", S3Key: "uploads/abc/f.txt"}
	var updateCalled bool
	dyn := &mockDynamo{
		getItemFn: func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: mustMarshalRecord(t, rec)}, nil
		},
		updateItemFn: func(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			updateCalled = true
			// verify status flips to pending_scan
			v, ok := in.ExpressionAttributeValues[":status"].(*dynamotypes.AttributeValueMemberS)
			if !ok || v.Value != models.StatusPendingScan {
				t.Errorf("status update: got %v, want %s", in.ExpressionAttributeValues[":status"], models.StatusPendingScan)
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := withURLParam(newProxyReq(t, "POST", "/files/abc/complete-upload",
		`{"parts":[{"part_number":1,"etag":"abc"}]}`, "test-key-id"), "id", "abc")
	w := httptest.NewRecorder()

	srv.CompleteUpload(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !updateCalled {
		t.Error("UpdateItem was not called")
	}
}

func TestCompleteUpload_WrongStatus(t *testing.T) {
	rec := models.FileRecord{ID: "abc", Status: models.StatusClean, UploadID: "u1"}
	dyn := &mockDynamo{
		getItemFn: func(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: mustMarshalRecord(t, rec)}, nil
		},
	}
	srv := newTestServer(&mockS3{}, &mockS3Presign{}, dyn)
	req := withURLParam(newProxyReq(t, "POST", "/files/abc/complete-upload",
		`{"parts":[{"part_number":1,"etag":"abc"}]}`, "test-key-id"), "id", "abc")
	w := httptest.NewRecorder()

	srv.CompleteUpload(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", w.Code)
	}
}
