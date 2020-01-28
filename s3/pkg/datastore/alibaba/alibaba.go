package alibaba

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	backendpb "github.com/opensds/multi-cloud/backend/proto"
	. "github.com/opensds/multi-cloud/s3/error"
	dscommon "github.com/opensds/multi-cloud/s3/pkg/datastore/common"
	"github.com/opensds/multi-cloud/s3/pkg/model"
	osdss3 "github.com/opensds/multi-cloud/s3/pkg/service"
	"github.com/opensds/multi-cloud/s3/pkg/utils"
	pb "github.com/opensds/multi-cloud/s3/proto"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"os"
)

type OSSAdapter struct {
	backend *backendpb.BackendDetail
	client  *oss.Client
}
type Range struct {
	Begin int64
	End   int64
}

//**************************PutObj******************************************//

func (ad *OSSAdapter) Put(ctx context.Context, stream io.Reader, object *pb.Object) (dscommon.PutResult, error) {
	bucket := ad.backend.BucketName
	alibababucket, err := ad.client.Bucket(bucket)
	objectId := object.BucketName + "/" + object.ObjectKey
	result := dscommon.PutResult{}

	size := object.Size
	log.Infof("put object[OSS], objectId:%s, bucket:%s, size=%d, userMd5=%s\n", objectId, bucket, size)

	if object.Tier == 0 {
		// default
		object.Tier = utils.Tier1
	}
	storClass, err := osdss3.GetNameFromTier(object.Tier, utils.OSTYPE_ALIBABA)
	if err != nil {
		log.Errorf("translate tier[%d] to aws storage class failed\n", object.Tier)
		return result, ErrInternalError
	}

	err = alibababucket.PutObject(objectId, stream, oss.ObjectStorageClass(oss.StorageClassType(storClass)))

	if err != nil {
		log.Errorf("upload object[OSS] failed, objectId:%s, err:%v", objectId, err)
	}
	log.Infof("upload object[OSS] succeed, objectId:%s, UpdateTime is:%v\n", objectId, result.UpdateTime)

	return result, ErrPutToBackendFailed
}

//*********************get obj********************************************************************//
func (ad *OSSAdapter) Get(ctx context.Context, object *pb.Object, start int64, end int64) (io.ReadCloser, error) {
	bucket := ad.backend.BucketName
	objectId := object.ObjectId
	log.Infof("get object[OSS], objectId:%s, bucket:%s\n", objectId, bucket)

	alibababucket, err := ad.client.Bucket(bucket)
	if err != nil {
		log.Error("bucket is not created, err:%v", err)
	}
	if start != 0 || end != 0 {
		body, err := alibababucket.GetObject(object.ObjectKey, oss.Range(start, end))
		if err != nil {
			log.Infof("get object[OSS] failed, objectId:%,s err:%v", objectId, err)
			return nil, ErrGetFromBackendFailed
		}
		return body, nil
	}
	log.Infof("get object[OSS] succeed, objectId:%s\n", objectId)
	return nil, nil

}

//**************************************Delete*****************************************//
func (ad *OSSAdapter) Delete(ctx context.Context, object *pb.DeleteObjectInput) error {
	bucket := ad.backend.BucketName
	objectId := object.Bucket + "/" + object.Key
	log.Infof("delete object[OSS], objectId:%s\n", objectId)

	alibababucket, err := ad.client.Bucket(bucket)

	err = alibababucket.DeleteObject(objectId)
	if err != nil {
		log.Infof("delete object[OSS] failed, objectId:%s, :%v", objectId, err)
		return ErrDeleteFromBackendFailed
	}
	log.Infof("delete object[OSS] succeed, objectId:%s.\n", objectId)
	return nil

}

//**************************************Change-Storage-Class************************************************//
func (ad *OSSAdapter) ChangeStorageClass(ctx context.Context, object *pb.Object, newClass *string) error {

	log.Infof("change storage class[OSS] of object[%s] to %s .\n", object.ObjectId, newClass)
	bucket := ad.backend.BucketName
	objectId := object.ObjectId
	alibababucket, err := ad.client.Bucket(bucket)
	srcObjectKey := object.BucketName + "/" + object.ObjectKey
	menu, err := alibababucket.CopyObject(srcObjectKey, objectId, oss.ObjectStorageClass(oss.StorageClassType(utils.OSTYPE_ALIBABA)))

	switch *newClass {
	case "STANDARD_IA":
		menu.StorClass = oss.StorageIA
	case "GLACIER":
		menu.StorClass = oss.StorageArchive
	default:
		log.Infof("[OSS] unspport storage class:%s", newClass)
		return ErrInvalidStorageClass
	}
	if err != nil {
		log.Errorf("[OSS] change storage class of object[%s] to %s failed: %v\n", object.ObjectId, newClass, err)
		return ErrPutToBackendFailed
	} else {
		log.Infof("[OSS] change storage class of object[%s] to %s succeed.\n", object.ObjectId, newClass)
	}

	return nil
}

//**********************************************Copy*************************************************************//
func (ad *OSSAdapter) Copy(ctx context.Context, stream io.Reader, target *pb.Object) (result dscommon.PutResult, err error) {
	return
}

//******************************************Multipart-Upload**********************************************************//
func (ad *OSSAdapter) InitMultipartUpload(ctx context.Context, object *pb.Object) (*pb.MultipartUpload, error) {
	bucket := ad.backend.BucketName
	objectId := object.BucketName + "/" + object.ObjectKey
	multipartUpload := &pb.MultipartUpload{}

	log.Infof("init multipart upload[OSS], objectId:%s, bucket:%s\n", objectId, bucket)
	alibababucket, err := ad.client.Bucket(bucket)

	output, err := alibababucket.InitiateMultipartUpload(objectId, oss.ObjectStorageClass(oss.StorageClassType(utils.OSTYPE_ALIBABA)))

	if err != nil {
		log.Errorf("translate tier[%d] to oss storage class failed\n", object.Tier)
		return nil, ErrInternalError
	}

	if err != nil {
		log.Infof("init multipart upload[OSS] failed, objectId:%s, err:%v", objectId, err)
		return nil, ErrBackendInitMultipartFailed
	}
	multipartUpload.Bucket = output.Bucket
	multipartUpload.Key = output.Key
	multipartUpload.UploadId = output.UploadID
	multipartUpload.ObjectId = objectId

	log.Infof("init multipart upload[OSS] succeed, objectId:%s\n", objectId)
	return multipartUpload, nil
}

//***************************************************Upload-Part***********************************************************//

func (ad *OSSAdapter) UploadPart(ctx context.Context, stream io.Reader, multipartUpload *pb.MultipartUpload, partNumber int64, upBytes int64) (*model.UploadPartResult, error) {
	bucket := ad.backend.BucketName
	objectId := multipartUpload.Bucket + "/" + multipartUpload.Key
	log.Infof("upload part[OSS], objectId:%s, partNum:%d, bytes:%d\n", objectId, partNumber, upBytes)

	alibababucket, err := ad.client.Bucket(bucket)

	input := oss.InitiateMultipartUploadResult{
		Bucket:   bucket,
		Key:      objectId,
		UploadID: multipartUpload.UploadId,
	}

	d, err := ioutil.ReadAll(stream)
	data := []byte(d)
	body := ioutil.NopCloser(bytes.NewReader(data))

	uploadoutput, err := alibababucket.UploadPart(input, body, upBytes, int(partNumber))
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(-1)
	}

	if err != nil {
		log.Infof("upload part[OSS] failed, objectId:%s, err:%v", objectId, err)
		return nil, ErrPutToBackendFailed
	}

	log.Infof("upload part[OSS] succeed, objectId:%s ", objectId)
	result := &model.UploadPartResult{Xmlns: model.Xmlns, ETag: uploadoutput.ETag, PartNumber: partNumber}

	return result, nil
}

//*******************************************Complete-Mutipart-Upload*****************************************//
func (ad *OSSAdapter) CompleteMultipartUpload(ctx context.Context, multipartUpload *pb.MultipartUpload,
	completeUpload *model.CompleteMultipartUpload) (*model.CompleteMultipartUploadResult, error) {
	bucket := ad.backend.BucketName
	objectId := multipartUpload.Bucket + "/" + multipartUpload.Key
	alibababucket, err := ad.client.Bucket(bucket)
	log.Infof("complete multipart upload[OBS], objectId:%s, bucket:%s\n", objectId, bucket)
	multipartoutput := oss.InitiateMultipartUploadResult{
		Bucket:   bucket,
		Key:      objectId,
		UploadID: multipartUpload.UploadId,
	}
	var completeParts []oss.UploadPart
	for _, p := range completeUpload.Parts {
		CompletePart := oss.UploadPart{
			ETag:       p.ETag,
			PartNumber: int(p.PartNumber),
		}
		completeParts = append(completeParts, CompletePart)
	}

	completepartinput, err := alibababucket.CompleteMultipartUpload(multipartoutput, completeParts)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(-1)
	}
	if err != nil {
		log.Infof("complete multipart upload[OBS] failed, objectid:%s, err:%v", objectId, err)
		return nil, ErrBackendCompleteMultipartFailed
	}
	completemultiuploadresult := &model.CompleteMultipartUploadResult{
		Xmlns:    model.Xmlns,
		Location: completepartinput.Location,
		Bucket:   completepartinput.Bucket,
		Key:      completepartinput.Key,
		ETag:     completepartinput.ETag,
	}

	log.Infof("complete multipart upload[OBS] succeed, objectId:%s\n", objectId)
	return completemultiuploadresult, nil
}

//**********************************************AbortMultipartUpload*******************************************//

func (ad *OSSAdapter) AbortMultipartUpload(ctx context.Context, multipartUpload *pb.MultipartUpload) error {
	bucket := ad.backend.BucketName
	objectId := multipartUpload.Bucket + "/" + multipartUpload.Key
	alibababucket, err := ad.client.Bucket(bucket)

	log.Infof("abort multipart upload[OSS], objectId:%s, bucket:%s\n", objectId, bucket)
	abortoutput, err := alibababucket.InitiateMultipartUpload(objectId)
	err = alibababucket.AbortMultipartUpload(abortoutput)
	if err != nil {
		log.Infof("abort multipart upload[OSS] failed, objectId:%s, err:%v", objectId, err)
		return ErrBackendAbortMultipartFailed
	}
	log.Infof("abort multipart upload[OSS] succeed, objectId:%s\n", objectId)
	return nil
}

//****************************************LIST--PARTS*******************************************************************//
func (ad *OSSAdapter) ListParts(context context.Context, listParts *pb.ListParts) (*model.ListPartsOutput, error) {
	bucket := ad.backend.BucketName
	alibababucket, err := ad.client.Bucket(bucket)

	input := oss.InitiateMultipartUploadResult{
		Bucket:   bucket,
		Key:      listParts.Key,
		UploadID: listParts.UploadId,
	}

	listPartsOutput, err := alibababucket.ListUploadedParts(input)

	listPartss := &model.ListPartsOutput{}
	listPartss.Bucket = listPartsOutput.Bucket
	listParts.Key = listPartsOutput.Key
	listParts.UploadId = listPartsOutput.UploadID
	listParts.MaxParts = int64(listPartsOutput.MaxParts)

	for _, p := range listPartsOutput.UploadedParts {
		part := model.Part{
			PartNumber: int64(p.PartNumber),
			ETag:       p.ETag,
		}
		listPartss.Parts = append(listPartss.Parts, part)
	}
	if err != nil {
		log.Infof("ListPartsListParts is nil:%v\n", err)
		return nil, err
	} else {
		log.Infof("ListParts successfully")
	}

	return nil, nil
}

//**************************************************************************//
func (ad *OSSAdapter) Close() error {
	//TODO
	return nil
}
