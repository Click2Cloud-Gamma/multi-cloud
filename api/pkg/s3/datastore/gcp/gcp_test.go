package gcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	backendpb "github.com/opensds/multi-cloud/backend/proto"
	. "github.com/opensds/multi-cloud/s3/pkg/exception"
	"github.com/opensds/multi-cloud/s3/pkg/model"
	pb "github.com/opensds/multi-cloud/s3/proto"
	"github.com/stretchr/testify/assert"
	"github.com/webrtcn/s3client"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

type TestGcpAdapter struct {
	backend *backendpb.BackendDetail
	session *s3client.Client
}

func TestInit(t *testing.T) {

	//actual
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	// expected
	backend2 := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	endpoint := backend.Endpoint
	AccessKeyID := backend.Access
	AccessKeySecret := backend.Security
	sess := s3client.NewClient(endpoint, AccessKeyID, AccessKeySecret)

	adap := TestGcpAdapter{backend: backend, session: sess}

	storeAdapater := Init(backend2)
	assert.Equal(t, adap.backend, storeAdapater.backend, "Backend Doesn't match")

}

func TestGcpAdapter_PUT(t *testing.T) {
	stream, _ := http.NewRequest(http.MethodPut, "/v1/s3/mybucket/Linuxcommand.jpg", nil)

	ctx := context.WithValue(stream.Context(), "operation", "upload")

	object2 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
		Size:       100}
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	storeAdapater := Init(backend)
	content := []byte("Did gyre and gimble in the wabe")
	co := bytes.NewReader(content)

	err2 := storeAdapater.PUT(co, object2, ctx)

	assert.Equal(t, S3Error{Code: 200, Description: ""}, err2, "Upload object to GCP failed")
}
func timeCost(start time.Time, method string) {
	_ = time.Since(start)
}
func hash(filename string) (md5String string, err error) {
	defer timeCost(time.Now(), "Hash")
	fi, err := os.Open(filename)
	if err != nil {
		return
	}
	defer fi.Close()
	reader := bufio.NewReader(fi)
	md5Ctx := md5.New()
	_, err = io.Copy(md5Ctx, reader)
	if err != nil {
		return
	}
	cipherStr := md5Ctx.Sum(nil)
	value := base64.StdEncoding.EncodeToString(cipherStr)
	return value, nil
}
func TestGcpAdapter_GET(t *testing.T) {
	var start int64
	var end int64
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	endpoint := backend.Endpoint
	AccessKeyID := backend.Access
	AccessKeySecret := backend.Security
	sess := s3client.NewClient(endpoint, AccessKeyID, AccessKeySecret)
	adap := TestGcpAdapter{backend: backend, session: sess}
	bucket := adap.session.NewBucket()
	gcpObject := bucket.NewObject(adap.backend.BucketName)
	object1 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	_, err := gcpObject.Get(object1.ObjectKey, &s3client.GetObjectOption{})

	if err != nil {

	} else {

	}
	backend2 := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	object2 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	storeAdapater := Init(backend2)
	request, _ := http.NewRequest(http.MethodPut, backend.Endpoint, nil)
	ctx := context.WithValue(request.Context(), "operation", "download")
	_, err2 := storeAdapater.GET(object2, ctx, start, end)
	assert.Equal(t, S3Error{Code: 200, Description: ""}, err2, "Download failed")
}
func TestGcpAdapter_DELETE(t *testing.T) {
	object := &pb.DeleteObjectInput{
		Key:    "Linuxcommand.jpg",
		Bucket: "bucketname",
	}
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	storeAdapater := Init(backend)
	request, _ := http.NewRequest(http.MethodDelete, backend.Endpoint, nil)
	ctx := context.WithValue(request.Context(), "operation", "")
	err := storeAdapater.DELETE(object, ctx)
	assert.Equal(t, S3Error{Code: 200, Description: ""}, err, "Delete the object from GCP failed")

}

func TestGcpAdapter_InitMultipartUpload(t *testing.T) {
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	storeAdapater := Init(backend)
	stream, _ := http.NewRequest(http.MethodPut, "/storage/browser/mybucket/Linuxcommand.jpg", nil)

	ctx := context.WithValue(stream.Context(), "operation", "uploads")

	object2 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	_, err2 := storeAdapater.InitMultipartUpload(object2, ctx)

	assert.Equal(t, S3Error{Code: 200, Description: ""}, err2, "Init Multipart upload parts failed")
}
func TestGCpAdapter_UploadPart(t *testing.T) {

	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	storeAdapater := Init(backend)
	bucket := storeAdapater.session.NewBucket()
	object := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	newObjectKey := object.BucketName + "/" + object.ObjectKey
	gcpObject := bucket.NewObject(storeAdapater.backend.BucketName)
	uploader := gcpObject.NewUploads(newObjectKey)
	multipartUpload := &pb.MultipartUpload{}

	res, err := uploader.Initiate(nil)

	if err != nil {

	} else {
		multipartUpload.Bucket = object.BucketName
		multipartUpload.Key = object.ObjectKey
		multipartUpload.UploadId = res.UploadID

	}
	content := []byte("Did gyre and gimble in the wabe")
	co := bytes.NewReader(content)

	stream, _ := http.NewRequest(http.MethodPut, "/v1/s3/mybucket/Linuxcommand.jpg", nil)

	ctx := context.WithValue(stream.Context(), "operation", "uploads")

	_, error := storeAdapater.UploadPart(co, multipartUpload, 1, 0, ctx)

	assert.Equal(t, S3Error{Code: 200, Description: ""}, error, "Upload parts to GCP failed")

}
func TestGCPAdapter_CompleteMultipartUpload(t *testing.T) {

	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	storeAdapater := Init(backend)
	bucket := storeAdapater.session.NewBucket()
	object := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	newObjectKey := object.BucketName + "/" + object.ObjectKey
	gcpObject := bucket.NewObject(storeAdapater.backend.BucketName)
	uploader := gcpObject.NewUploads(newObjectKey)
	multipartUpload := &pb.MultipartUpload{}

	res, err := uploader.Initiate(nil)

	if err != nil {

	} else {
		multipartUpload.Bucket = object.BucketName
		multipartUpload.Key = object.ObjectKey
		multipartUpload.UploadId = res.UploadID

	}
	content := []byte("Did gyre and gimble in the wabe")
	co := bytes.NewReader(content)
	stream, _ := http.NewRequest(http.MethodPut, "/v1/s3/mybucket/Linuxcommand.jpg", nil)
	ctx := context.WithValue(stream.Context(), "operation", "uploads")
	result, _ := storeAdapater.UploadPart(co, multipartUpload, 1, 0, ctx)
	var Uploads UploadParts

	Uploads.ETag = result.ETag
	Uploads.PartNumber = result.PartNumber
	Uploads.Xmlns = result.Xmlns

	var parts model.Part
	parts.PartNumber = result.PartNumber
	parts.ETag = result.ETag
	UploadParts := &model.CompleteMultipartUpload{}
	UploadParts.Xmlns = Uploads.Xmlns
	UploadParts.Part = append(UploadParts.Part, parts)
	//*completeParts = append(*completeParts, *parts)
	_, errors := storeAdapater.CompleteMultipartUpload(multipartUpload, UploadParts, ctx)
	assert.Equal(t, S3Error{Code: 200, Description: ""}, errors, "Complete upload part failed")

}
func TestGcpAdapter_AbortMultipartUploadAdapter(t *testing.T) {
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	storeAdapater := Init(backend)
	bucket := storeAdapater.session.NewBucket()
	object := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "bucketname",
	}
	newObjectKey := object.BucketName + "/" + object.ObjectKey
	gcpObject := bucket.NewObject(storeAdapater.backend.BucketName)
	uploader := gcpObject.NewUploads(newObjectKey)
	multipartUpload := &pb.MultipartUpload{}

	res, err := uploader.Initiate(nil)

	if err != nil {

	} else {

		multipartUpload.Bucket = object.BucketName
		multipartUpload.Key = object.ObjectKey
		multipartUpload.UploadId = res.UploadID

	}
	content := []byte("Did gyre and gimble in the wabe")
	co := bytes.NewReader(content)

	stream, _ := http.NewRequest(http.MethodPut, "/v1/s3/mybucket/Linuxcommand.jpg", nil)
	ctx := context.WithValue(stream.Context(), "operation", "uploads")
	storeAdapater.UploadPart(co, multipartUpload, 1, 0, ctx)

	errors := storeAdapater.AbortMultipartUpload(multipartUpload, ctx)
	assert.Equal(t, S3Error{Code: 200, Description: ""}, errors, "Unable to Abort the multipart Upload")
}
func TestGcpAdapter_ListParts(t *testing.T) {
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	storeAdapater := Init(backend)
	bucket := storeAdapater.session.NewBucket()
	object := &pb.Object{
		ObjectKey:  "test.txt",
		BucketName: "bucketname",
	}
	newObjectKey := object.BucketName + "/" + object.ObjectKey
	gcpObject := bucket.NewObject(storeAdapater.backend.BucketName)
	uploader := gcpObject.NewUploads(newObjectKey)
	res, _ := uploader.Initiate(nil)
	stream, _ := http.NewRequest(http.MethodPut, "/v1/s3/mybucket/Linuxcommand.jpg", nil)

	ctx := context.WithValue(stream.Context(), "operation", "uploads")

	listParts := &pb.ListParts{}
	listParts.UploadId = res.UploadID
	listParts.Key = object.ObjectKey
	listParts.Bucket = object.BucketName
	_, error := storeAdapater.ListParts(listParts, ctx)

	assert.Equal(t, S3Error{Code: 200, Description: ""}, error, "Listing the upload parts failed")
}

func TestGcpAdapter_GetObjectInfo(t *testing.T) {
	backend := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}
	endpoint := backend.Endpoint
	AccessKeyID := backend.Access
	AccessKeySecret := backend.Security
	sess := s3client.NewClient(endpoint, AccessKeyID, AccessKeySecret)

	adap := TestGcpAdapter{backend: backend, session: sess}

	bucket := adap.session.NewBucket()

	gcpObject := bucket.NewObject(adap.backend.BucketName)
	object1 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "archanabucket",
	}

	gcpObject.Get(object1.ObjectKey, &s3client.GetObjectOption{})

	backend2 := &backendpb.BackendDetail{
		Access:     "###############",
		BucketName: "bucket_Name",
		Endpoint:   "##################",
		Id:         "123",
		Name:       "name",
		Region:     "##########",
		Security:   "########################",
		TenantId:   "ghgsadfhghshhsad",
	}

	object2 := &pb.Object{
		ObjectKey:  "Linuxcommand.jpg",
		BucketName: "archanabucket",
	}

	storeAdapater := Init(backend2)
	request, _ := http.NewRequest(http.MethodPut, backend.Endpoint, nil)
	ctx := context.WithValue(request.Context(), "operation", "download")
	_, err2 := storeAdapater.GetObjectInfo(backend.BucketName, object2.ObjectKey, ctx)

	assert.Equal(t, S3Error{Code: 200, Description: ""}, err2, "Download failed")

}

type UploadParts struct {
	Xmlns      string `xml:"xmlns,attr"`
	PartNumber int64  `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}
