package models

type FileRecord struct {
	ID         string `dynamodbav:"id"          json:"id"`
	Name       string `dynamodbav:"name"        json:"name"`
	Size       int64  `dynamodbav:"size"        json:"size"`
	UploadedBy string `dynamodbav:"uploaded_by" json:"uploaded_by"`
	Status     string `dynamodbav:"status"      json:"status"`
	UploadID   string `dynamodbav:"upload_id"   json:"upload_id"`
	S3Key      string `dynamodbav:"s3_key"      json:"s3_key"`
	ExpiresAt  int64  `dynamodbav:"expires_at"  json:"expires_at"`
}
