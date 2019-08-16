// Copyright 2019 The OpenSDS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/globalsign/mgo/bson"
	"github.com/micro/go-micro/client"
	"github.com/opensds/multi-cloud/backend/proto"
	"github.com/opensds/multi-cloud/dataflow/pkg/model"
	flowtype "github.com/opensds/multi-cloud/dataflow/pkg/model"
	"github.com/opensds/multi-cloud/datamover/pkg/amazon/s3"
	"github.com/opensds/multi-cloud/datamover/pkg/azure/blob"
	"github.com/opensds/multi-cloud/datamover/pkg/ceph/s3"
	"github.com/opensds/multi-cloud/datamover/pkg/db"
	"github.com/opensds/multi-cloud/datamover/pkg/gcp/s3"
	"github.com/opensds/multi-cloud/datamover/pkg/hw/obs"
	"github.com/opensds/multi-cloud/datamover/pkg/ibm/cos"
	. "github.com/opensds/multi-cloud/datamover/pkg/utils"
	pb "github.com/opensds/multi-cloud/datamover/proto"
	s3utils "github.com/opensds/multi-cloud/s3/pkg/utils"
	osdss3 "github.com/opensds/multi-cloud/s3/proto"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"time"
)

var simuRoutines = 10
var PART_SIZE int64 = 16 * 1024 * 1024 //The max object size that can be moved directly, default is 16M.
var JOB_RUN_TIME_MAX = 2592000         //seconds, equals 1 day
var s3client osdss3.S3Service
var bkendclient backend.BackendService
var logfile *os.File
var err error
var filepath = "/opt/"

const WT_DOWLOAD = 48
const WT_UPLOAD = 48
const WT_DELETE = 4

var logger = log.New(os.Stdout, "", log.LstdFlags)

type Migration interface {
	Init()
	HandleMsg(msg string)
}

func Init() {
	logger.Println("Migration init.")
	s3client = osdss3.NewS3Service("s3", client.DefaultClient)
	bkendclient = backend.NewBackendService("backend", client.DefaultClient)
}

func HandleMsg(msgData []byte) error {
	var job pb.RunJobRequest

	err := json.Unmarshal(msgData, &job)
	if err != nil {
		logger.Printf("unmarshal failed, err:%v\n", err)
		return err
	}

	//Check the status of job, and run it if needed

	status := db.DbAdapter.GetJobStatus(job.Id)
	if status != flowtype.JOB_STATUS_PENDING {
		logger.Printf("job[id#%s] is not in %s status.\n", job.Id, flowtype.JOB_STATUS_PENDING)
		return nil //No need to consume this message again
	}
	j := flowtype.Job{Id: bson.ObjectIdHex(job.Id)}
	jobid := fmt.Sprintf("%x", string(j.Id))
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}
	defer logfile.Close()

	logger.Printf("HandleMsg:job=%+v\n", job)
	go runjob(&job)
	return nil
}

func doMove(ctx context.Context, objs []*osdss3.Object, capa chan int64, th chan int, srcLoca *LocationInfo,
	destLoca *LocationInfo, remainSource bool, job *model.Job) {
	//Only three routines allowed to be running at the same time
	//th := make(chan int, simuRoutines).
	jobid := fmt.Sprintf("%x", string(job.Id))
	db.DbAdapter.UpdateJob(job)
	if job.Msg == "Migration Aborted" {
		job.TimeRequired = 0
		job.EndTime = time.Now()
		job.Status = flowtype.JOB_STATUS_ABORTED
		db.DbAdapter.UpdateJob(job)

		return
	}
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}
	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)
	defer logfile.Close()
	locMap := make(map[string]*LocationInfo)
	for i := 0; i < len(objs); i++ {
		// To Cancel Job
		db.DbAdapter.UpdateJob(job)
		if job.Msg == "Migration Aborted" {
			job.TimeRequired = 0
			job.EndTime = time.Now()
			job.Status = flowtype.JOB_STATUS_ABORTED
			db.DbAdapter.UpdateJob(job)
			break
		}
		if objs[i].Tier == s3utils.Tier999 {
			// archived object cannot be moved currently
			logger.Printf("Object(key:%s) is archived, cannot be migrated.\n", objs[i].ObjectKey)
			continue
		}
		logger.Printf(" [INFO] Begin to move obj :%s \n", objs[i].ObjectKey)
		go move(ctx, objs[i], capa, th, srcLoca, destLoca, remainSource, locMap, job)
		//Create one routine
		th <- 1
		log.Printf(" doMigrate: produce 1 routine, len(th):%d.\n", len(th))
	}
	if job.Msg == "Migration Aborted" {
		db.DbAdapter.UpdateJob(job)
		return
	}
}

func MoveObj(obj *osdss3.Object, srcLoca *LocationInfo, destLoca *LocationInfo, job *model.Job) error {
	if job.Msg == "Migration Aborted" {
		job.TimeRequired = 0
		job.EndTime = time.Now()
		job.Status = flowtype.JOB_STATUS_ABORTED
		db.DbAdapter.UpdateJob(job)
		return errors.New(job.Msg)
	}
	jobid := fmt.Sprintf("%x", string(job.Id))
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}
	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)
	defer logfile.Close()

	logger.Printf("[INFO] Move object[%s] from bucket '%s' to bucket '%s', size is %d.\n", obj.ObjectKey, srcLoca.BucketName,
		destLoca.BucketName, obj.Size)
	if obj.Size <= 0 {
		return nil
	}
	buf := make([]byte, obj.Size)
	var size int64 = 0
	var err error = nil
	var downloader, uploader MoveWorker
	downloadObjKey := obj.ObjectKey
	if srcLoca.VirBucket != "" {
		downloadObjKey = srcLoca.VirBucket + "/" + downloadObjKey
	}
	//download
	if job.Msg == "Migration Aborted" {
		err := "Migration Aborted"
		job.TimeRequired = 0
		job.EndTime = time.Now()
		job.Status = flowtype.JOB_STATUS_ABORTED
		db.DbAdapter.UpdateJob(job)

		return errors.New(err)
	}
	start_time := time.Now()
	switch srcLoca.StorType {
	case flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE, flowtype.STOR_TYPE_HW_FUSIONCLOUD:
		downloader = &obsmover.ObsMover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	case flowtype.STOR_TYPE_AWS_S3:
		downloader = &s3mover.S3Mover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	case flowtype.STOR_TYPE_IBM_COS:
		downloader = &ibmcosmover.IBMCOSMover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	case flowtype.STOR_TYPE_AZURE_BLOB:
		downloader = &blobmover.BlobMover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	case flowtype.STOR_TYPE_CEPH_S3:
		downloader = &cephs3mover.CephS3Mover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	case flowtype.STOR_TYPE_GCP_S3:
		downloader = &Gcps3mover.GcpS3Mover{}
		size, err = downloader.DownloadObj(downloadObjKey, srcLoca, buf)
	default:
		{
			logger.Printf("[ERROR] not support source backend type:%v\n", srcLoca.StorType)
			err = errors.New("not support source backend type")
		}
	}
	if err != nil {
		logger.Printf("[ERROR] download object[%s] failed.", obj.ObjectKey)
		return err
	}
	progressTimeCalculation(job, size, WT_DOWLOAD, start_time)
	if job.Msg == "Migration Aborted" {
		err := "Migration Aborted"
		job.TimeRequired = 0
		job.EndTime = time.Now()
		job.Status = flowtype.JOB_STATUS_ABORTED
		db.DbAdapter.UpdateJob(job)
		return errors.New(err)
	}
	log.Printf("Download object[%s] succeed, size=%d\n", obj.ObjectKey, size)

	uploadObjKey := obj.ObjectKey
	if srcLoca.VirBucket != "" {
		uploadObjKey = destLoca.VirBucket + "/" + uploadObjKey
	}

	switch destLoca.StorType {
	case flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE, flowtype.STOR_TYPE_HW_FUSIONCLOUD:
		uploader = &obsmover.ObsMover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	case flowtype.STOR_TYPE_AWS_S3:
		uploader = &s3mover.S3Mover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	case flowtype.STOR_TYPE_IBM_COS:
		uploader = &ibmcosmover.IBMCOSMover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	case flowtype.STOR_TYPE_AZURE_BLOB:
		uploader = &blobmover.BlobMover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	case flowtype.STOR_TYPE_CEPH_S3:
		uploader = &cephs3mover.CephS3Mover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	case flowtype.STOR_TYPE_GCP_S3:
		uploader = &Gcps3mover.GcpS3Mover{}
		err = uploader.UploadObj(uploadObjKey, destLoca, buf)
	default:
		logger.Printf("[ERROR] not support destination backend type:%v\n", destLoca.StorType)
		return errors.New("not support destination backend type.")
	}
	if err != nil {
		logger.Printf("[ERROR] upload object[bucket:%s,key:%s] failed, err:%v.\n", destLoca.BucketName, uploadObjKey, err)
		if job.Msg == "Migration Aborted" {
			err := "Migration Aborted"
			job.TimeRequired = 0
			job.EndTime = time.Now()
			job.Status = flowtype.JOB_STATUS_ABORTED
			db.DbAdapter.UpdateJob(job)
			return errors.New(err)
		}
		return err
	} else {
		log.Printf("[INFO] upload object[bucket:%s,key:%s] successfully.\n", destLoca.BucketName, uploadObjKey)
		//js.Uploaded=js.Uploaded+size

		progressTimeCalculation(job, size, WT_DOWLOAD, start_time)
		if job.Msg == "Migration Aborted" {
			err := "Migration Aborted"
			job.TimeRequired = 0
			job.EndTime = time.Now()
			job.Status = flowtype.JOB_STATUS_ABORTED
			db.DbAdapter.UpdateJob(job)
			return errors.New(err)
		}
	}

	return err
}

func multiPartDownloadInit(srcLoca *LocationInfo) (mover MoveWorker, err error) {

	switch srcLoca.StorType {
	case flowtype.STOR_TYPE_AWS_S3:
		mover := &s3mover.S3Mover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err
	case flowtype.STOR_TYPE_IBM_COS:
		mover := &ibmcosmover.IBMCOSMover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err
	case flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE, flowtype.STOR_TYPE_HW_FUSIONCLOUD:
		mover := &obsmover.ObsMover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err
	case flowtype.STOR_TYPE_AZURE_BLOB:
		mover := &blobmover.BlobMover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err
	case flowtype.STOR_TYPE_CEPH_S3:
		mover := &cephs3mover.CephS3Mover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err
	case flowtype.STOR_TYPE_GCP_S3:
		mover := &Gcps3mover.GcpS3Mover{}
		err := mover.MultiPartDownloadInit(srcLoca)
		return mover, err

	default:
		logger.Printf("unsupport storType[%s] to init multipart download.\n", srcLoca.StorType)
	}

	return nil, errors.New("unsupport storage type.")
}

func multiPartUploadInit(objKey string, destLoca *LocationInfo) (mover MoveWorker, uploadId string, err error) {
	uploadId = ""
	switch destLoca.StorType {
	case flowtype.STOR_TYPE_AWS_S3:
		mover = &s3mover.S3Mover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return
	case flowtype.STOR_TYPE_IBM_COS:
		mover = &ibmcosmover.IBMCOSMover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return
	case flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE, flowtype.STOR_TYPE_HW_FUSIONCLOUD:
		mover = &obsmover.ObsMover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return
	case flowtype.STOR_TYPE_AZURE_BLOB:
		mover = &blobmover.BlobMover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return
	case flowtype.STOR_TYPE_CEPH_S3:
		mover = &cephs3mover.CephS3Mover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return
	case flowtype.STOR_TYPE_GCP_S3:
		mover = &Gcps3mover.GcpS3Mover{}
		uploadId, err = mover.MultiPartUploadInit(objKey, destLoca)
		return mover, uploadId, err
	default:
		logger.Printf(" unsupport storType[%s] to download.\n", destLoca.StorType)
	}

	return nil, uploadId, errors.New("unsupport storage type")
}

func abortMultipartUpload(objKey string, destLoca *LocationInfo, mover MoveWorker) error {
	switch destLoca.StorType {
	case flowtype.STOR_TYPE_AWS_S3, flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE,
		flowtype.STOR_TYPE_HW_FUSIONCLOUD, flowtype.STOR_TYPE_AZURE_BLOB, flowtype.STOR_TYPE_CEPH_S3, flowtype.STOR_TYPE_GCP_S3, flowtype.STOR_TYPE_IBM_COS:
		return mover.AbortMultipartUpload(objKey, destLoca)
	default:
		logger.Printf(" unsupport storType[%s] to download.\n", destLoca.StorType)
	}

	return errors.New("unsupport storage type")
}

func completeMultipartUpload(objKey string, destLoca *LocationInfo, mover MoveWorker) error {
	switch destLoca.StorType {
	case flowtype.STOR_TYPE_AWS_S3, flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE,
		flowtype.STOR_TYPE_HW_FUSIONCLOUD, flowtype.STOR_TYPE_AZURE_BLOB, flowtype.STOR_TYPE_CEPH_S3, flowtype.STOR_TYPE_GCP_S3, flowtype.STOR_TYPE_IBM_COS:
		return mover.CompleteMultipartUpload(objKey, destLoca)
	default:
		logger.Printf(" unsupport storType[%s] to download.\n", destLoca.StorType)
	}
	return errors.New("unsupport storage type")
}

func addMultipartUpload(objKey, virtBucket, backendName, uploadId string) {
	// some cloud vendor, like azure, does not support user to delete uncomplete multipart upload data, and no uploadId provided,
	// so we do not need to manage the uncomplete multipart upload data.
	if len(uploadId) == 0 {
		return
	}
	record := osdss3.MultipartUploadRecord{ObjectKey: objKey, Bucket: virtBucket, Backend: backendName, UploadId: uploadId}
	record.InitTime = time.Now().Unix()

	s3client.AddUploadRecord(context.Background(), &record)
	// TODO: Need consider if add failed
}

func deleteMultipartUpload(objKey, virtBucket, backendName, uploadId string) {
	// some cloud vendor, like azure, does not support user to delete uncomplete multipart upload data, and no uploadId provided,
	// so we do not need to manage the uncomplete multipart upload data.
	if len(uploadId) == 0 {
		return
	}

	record := osdss3.MultipartUploadRecord{ObjectKey: objKey, Bucket: virtBucket, Backend: backendName, UploadId: uploadId}
	s3client.DeleteUploadRecord(context.Background(), &record)
}

func MultipartMoveObj(obj *osdss3.Object, srcLoca *LocationInfo, destLoca *LocationInfo, job *model.Job) error {
	db.DbAdapter.UpdateJob(job)
	if job.Msg == "Migration Aborted" {
		job.Status = flowtype.JOB_STATUS_ABORTED
		job.TimeRequired = 0
		job.EndTime = time.Now()
		return errors.New(job.Msg)
	}
	jobid := fmt.Sprintf("%x", string(job.Id))
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}
	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)
	defer logfile.Close()
	partCount := int64(obj.Size / PART_SIZE)
	if obj.Size%PART_SIZE != 0 {
		partCount++
	}

	logger.Printf("[INFO] Move object[%s] from  bucket '%s' to bucket '%s', size is %d.\n", obj.ObjectKey, srcLoca.BucketName,
		destLoca.BucketName, obj.Size)
	downloadObjKey := obj.ObjectKey
	if srcLoca.VirBucket != "" {
		downloadObjKey = srcLoca.VirBucket + "/" + downloadObjKey
	}
	uploadObjKey := obj.ObjectKey
	if destLoca.VirBucket != "" {
		uploadObjKey = destLoca.VirBucket + "/" + uploadObjKey
	}

	buf := make([]byte, PART_SIZE)
	var i int64
	var err error
	var uploadMover, downloadMover MoveWorker
	var uploadId string
	currPartSize := PART_SIZE
	for i = 0; i < partCount; i++ {
		if job.Msg == "Migration Aborted" {
			job.TimeRequired = 0
			job.EndTime = time.Now()
			job.Status = flowtype.JOB_STATUS_ABORTED
			db.DbAdapter.UpdateJob(job)
			break
			return errors.New(job.Msg)
		}
		partNumber := i + 1
		offset := int64(i) * PART_SIZE
		if i+1 == partCount {
			currPartSize = obj.Size - offset
			buf = nil
			buf = make([]byte, currPartSize)
		}

		//download
		start := offset
		end := offset + currPartSize - 1
		if partNumber == 1 {
			downloadMover, err = multiPartDownloadInit(srcLoca)
			if err != nil {
				return err
			}
		}
		start_time := time.Now()
		readSize, err := downloadMover.DownloadRange(downloadObjKey, srcLoca, buf, start, end)
		//TODO ***** here we are getting passed capacity of part)
		if err != nil {
			logger.Printf("[ERROR] Download failed %v ", err)
			return err
		} else {
			progressTimeCalculation(job, currPartSize, WT_DOWLOAD, start_time)
			if job.Msg == "Migration Aborted" {
				job.TimeRequired = 0
				job.EndTime = time.Now()
				job.Status = flowtype.JOB_STATUS_ABORTED
				db.DbAdapter.UpdateJob(job)
				break
				return errors.New(job.Msg)
			}
		}
		if int64(readSize) != currPartSize {
			logger.Printf("[ERROR] internal error, currPartSize=%d, readSize=%d\n", currPartSize, readSize)
			return errors.New(DMERR_InternalError)
		}
		//upload
		if partNumber == 1 {
			//init multipart upload
			uploadMover, uploadId, err = multiPartUploadInit(uploadObjKey, destLoca)
			if err != nil {
				job.Msg = err.Error()
				return err
			} else {
				addMultipartUpload(obj.ObjectKey, destLoca.VirBucket, destLoca.BakendName, uploadId)
			}
		}
		err1 := uploadMover.UploadPart(uploadObjKey, destLoca, currPartSize, buf, partNumber, offset)
		if err1 != nil {
			err := abortMultipartUpload(obj.ObjectKey, destLoca, uploadMover)
			if err != nil {
				logger.Printf("[ERROR] Abort s3 multipart upload failed, err:%v\n", err)
			} else {
				deleteMultipartUpload(obj.ObjectKey, destLoca.VirBucket, destLoca.BakendName, uploadId)
			}
			//job.Msg = "Upload failed: " + err.Error()
			logger.Printf("[ERROR] multipart upload failed %v ", err1)
			return err1
		} else {
			progressTimeCalculation(job, currPartSize, WT_DOWLOAD, start_time)
			if job.Msg == "Migration Aborted" {
				job.TimeRequired = 0
				job.EndTime = time.Now()
				job.Status = flowtype.JOB_STATUS_ABORTED
				db.DbAdapter.UpdateJob(job)
				err1 := abortMultipartUpload(obj.ObjectKey, destLoca, uploadMover)
				if err1 != nil {
					logger.Printf("[ERROR] abort s3 multipart upload failed, err:%v\n", err1)
				} else {
					logger.Printf("[INFO] Abort Multi-Part Upload Successfully")
					deleteMultipartUpload(obj.ObjectKey, destLoca.VirBucket, destLoca.BakendName, uploadId)

				}
				break
				return errors.New(job.Msg)
			}
			log.Printf("Upload range [objectkey: %s, partnumber#%d, offset#%d successfully.\n", obj.ObjectKey, partNumber, offset)
		}
		//completeParts = append(completeParts, completePart)
	}
	if job.Msg == "Migration Aborted" {
		return errors.New(job.Msg)
	}

	err = completeMultipartUpload(uploadObjKey, destLoca, uploadMover)
	if err != nil {
		logger.Println(err.Error())
		err := abortMultipartUpload(obj.ObjectKey, destLoca, uploadMover)
		if err != nil {
			logger.Printf("[ERROR] abort s3 multipart upload failed, err:%v\n", err)
		} else {
			deleteMultipartUpload(obj.ObjectKey, destLoca.VirBucket, destLoca.BakendName, uploadId)
		}
	} else {
		deleteMultipartUpload(obj.ObjectKey, destLoca.VirBucket, destLoca.BakendName, uploadId)
		log.Printf("CompleteMultipartUpload successfully %s", obj.ObjectKey)
	}

	return err
}

func deleteObj(ctx context.Context, obj *osdss3.Object, loca *LocationInfo) error {
	objKey := obj.ObjectKey
	if loca.VirBucket != "" {
		objKey = loca.VirBucket + "/" + objKey
	}
	var err error = nil
	switch loca.StorType {
	case flowtype.STOR_TYPE_AWS_S3:
		mover := s3mover.S3Mover{}
		err = mover.DeleteObj(objKey, loca)
	case flowtype.STOR_TYPE_IBM_COS:
		mover := ibmcosmover.IBMCOSMover{}
		err = mover.DeleteObj(objKey, loca)
	case flowtype.STOR_TYPE_HW_OBS, flowtype.STOR_TYPE_HW_FUSIONSTORAGE, flowtype.STOR_TYPE_HW_FUSIONCLOUD:
		mover := obsmover.ObsMover{}
		err = mover.DeleteObj(objKey, loca)
	case flowtype.STOR_TYPE_AZURE_BLOB:
		mover := blobmover.BlobMover{}
		err = mover.DeleteObj(objKey, loca)
	case flowtype.STOR_TYPE_CEPH_S3:
		mover := cephs3mover.CephS3Mover{}
		err = mover.DeleteObj(objKey, loca)
	case flowtype.STOR_TYPE_GCP_S3:
		mover := Gcps3mover.GcpS3Mover{}
		err = mover.DeleteObj(objKey, loca)
	default:
		logger.Printf("delete object[objkey:%s] from backend storage failed.\n", obj.ObjectKey)
		err = errors.New(DMERR_UnSupportBackendType)
	}

	if err != nil {
		return err
	}

	//delete metadata
	if loca.VirBucket != "" {
		delMetaReq := osdss3.DeleteObjectInput{Bucket: loca.VirBucket, Key: obj.ObjectKey}
		_, err = s3client.DeleteObject(ctx, &delMetaReq)
		if err != nil {
			logger.Printf("delete object metadata of obj[bucket:%s,objKey:%s] failed, err:%v\n", loca.VirBucket,
				obj.ObjectKey, err)
		} else {
			logger.Printf("Delete object metadata of obj[bucket:%s,objKey:%s] successfully.\n", loca.VirBucket,
				obj.ObjectKey)
		}
	}

	return err
}

func move(ctx context.Context, obj *osdss3.Object, capa chan int64, th chan int, srcLoca *LocationInfo,
	destLoca *LocationInfo, remainSource bool, locaMap map[string]*LocationInfo, job *model.Job) {

	logger.Printf("Obj[%s] is stored in the backend is [%s], default backend is [%s], target backend is [%s].\n",
		obj.ObjectKey, obj.Backend, srcLoca.BakendName, destLoca.BakendName)

	succeed := true
	needMove := true
	newSrcLoca, err := refreshSrcLocation(ctx, obj, srcLoca, destLoca, locaMap)
	if err != nil {
		needMove = false
		succeed = false
	}
	db.DbAdapter.UpdateJob(job)
	if job.Msg == "Migration Aborted" {
		job.Status = flowtype.JOB_STATUS_ABORTED
		job.TimeRequired = 0
		job.EndTime = time.Now()
		db.DbAdapter.UpdateJob(job)
		return
	}

	if needMove {
		//move object
		part_size, err := strconv.ParseInt(os.Getenv("PARTSIZE"), 10, 64)
		logger.Printf("part_size=%d, err=%v.\n", part_size, err)
		if err == nil {
			//part_size must be more than 5M and less than 100M
			if part_size >= 5 && part_size <= 100 {
				PART_SIZE = part_size * 1024 * 1024
				logger.Printf("Set PART_SIZE to be %d.\n", PART_SIZE)
			}
		}
		db.DbAdapter.UpdateJob(job)
		if job.Msg == "Migration Aborted" {
			job.Status = flowtype.JOB_STATUS_ABORTED
			job.TimeRequired = 0
			job.EndTime = time.Now()
			db.DbAdapter.UpdateJob(job)
			return
		}
		if obj.Size <= PART_SIZE {
			err = MoveObj(obj, newSrcLoca, destLoca, job)
		} else {
			err = MultipartMoveObj(obj, newSrcLoca, destLoca, job)
		}

		if err != nil {
			job.Msg = err.Error()
			succeed = false
		}
	}
	//TODO: what if update metadata failed
	//add object metadata to the destination bucket if destination is not self-defined
	if succeed && destLoca.VirBucket != "" {
		obj.BucketName = destLoca.VirBucket
		obj.Backend = destLoca.BakendName
		obj.LastModified = time.Now().Unix()
		_, err := s3client.CreateObject(ctx, obj)
		if err != nil {
			logger.Printf("add object metadata of obj [objKey:%s] to bucket[name:%s] failed, err:%v.\n", obj.ObjectKey,
				obj.BucketName, err)
		} else {
			logger.Printf("add object metadata of obj [objKey:%s] to bucket[name:%s] succeed.\n", obj.ObjectKey,
				obj.BucketName)
		}
	}

	//Delete source data if needed
	logger.Printf("remainSource for object[%s] is:%v.", obj.ObjectKey, remainSource)
	if succeed && !remainSource {
		deleteObj(ctx, obj, newSrcLoca)
		//TODO: what if delete failed
		//TODO: what if delete failed
	}
	jobid := fmt.Sprintf("%x", string(job.Id))
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}
	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)
	defer logfile.Close()

	if succeed {
		//If migrate success, update capacity
		logger.Printf(" [INFO] %s object migrated successfully.", obj.ObjectKey)
		capa <- obj.Size
		logger.Printf(" [INFO]  %v size of migrated object ", obj.Size)
		progressTimeCalculation(job, obj.Size, WT_DELETE, time.Now())
		logger.Printf("[INFO] Progress %d", job.Progress)

	} else {
		if job.Status != flowtype.JOB_STATUS_ABORTED {
			logger.Printf(" [ERROR] migrate object[%s] failed.", obj.ObjectKey)
		} else {
			logger.Printf(" [INFO] migrate object[%s] aborted.", obj.ObjectKey)
		}
		capa <- -1
	}
	t := <-th
	log.Printf(" [INFO] migrate: consume %d routine, len(th)=%d\n", t, len(th))
}

func updateJob(j *flowtype.Job) {
	for i := 1; i <= 3; i++ {
		err := db.DbAdapter.UpdateJob(j)
		if err == nil {
			break
		}
		if i == 3 {
			logger.Printf(" [ERROR] update the finish status of job in database failed three times, no need to try more.")
		}
	}
}

func runjob(in *pb.RunJobRequest) error {
	logfile, err = os.OpenFile(filepath+in.Id+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}

	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)

	log.Println("Runjob is called in datamover service.")
	log.Printf("Request: %+v\n", in)

	// set context tiemout
	ctx := context.Background()

	dur := getCtxTimeout()
	_, ok := ctx.Deadline()
	if !ok {
		ctx, _ = context.WithTimeout(ctx, dur)
	}

	// init job
	j := flowtype.Job{Id: bson.ObjectIdHex(in.Id)}
	j.StartTime = time.Now()
	j.Status = flowtype.JOB_STATUS_RUNNING
	updateJob(&j)
	//jobs.Set(in.Id, cancel)
	// get location information
	srcLoca, destLoca, err := getLocationInfo(ctx, &j, in)
	if err != nil {
		logger.Printf("[ERROR] Incorrect Credentials")
		j.Msg = "Incorrect Credentials"
		j.Status = flowtype.JOB_STATUS_FAILED
		j.EndTime = time.Now()
		j.TimeRequired = int64(0)
		updateJob(&j)
		return err
	}

	// get total count and total size of objects need to be migrated
	//totalCount, totalSize, err := countObjs(ctx, in)
	//j.TotalCount = totalCount
	//j.TotalCapacity = totalSize
	//Start count obj- TODO PRIVATE
	var offset, limit int32 = 0, 1000
	objs, err := getObjs(ctx, in, srcLoca, offset, limit)
	if err != nil {
		logger.Printf("[ERROR] Incorrect Credentials")
		//update database
		j.Msg = "Incorrect Credentials"
		j.Status = flowtype.JOB_STATUS_FAILED
		j.EndTime = time.Now()
		j.TimeRequired = int64(0)
		db.DbAdapter.UpdateJob(&j)
		return err
	}

	totalObj := len(objs)
	if totalObj == 0 {
		logger.Printf("[WARN] Bucket is empty.")
		j.Msg = "Bucket is empty"
		j.TimeRequired = int64(0)
	}
	for i := 0; i < totalObj; i++ {
		j.TotalCount++
		j.TotalCapacity += objs[i].Size
	}
	//js:= &jobstatus{}
	// End TODO
	// TODO Here we are getting Total size and Count

	if err != nil || j.TotalCount == 0 || j.TotalCapacity == 0 {
		j.Status = flowtype.JOB_STATUS_FAILED
		j.EndTime = time.Now()
		j.TimeRequired = int64(0)
		updateJob(&j)
		return err
	}

	updateJob(&j)
	// used to transfer capacity(size) of objects
	capa := make(chan int64)
	// concurrent go routines is limited to be simuRoutines
	th := make(chan int, simuRoutines)
	//var offset, limit int32 = 0, 1000
	for {
		objs, err := getObjs(ctx, in, srcLoca, offset, limit)
		if err != nil {
			//update database
			j.Status = flowtype.JOB_STATUS_FAILED
			j.EndTime = time.Now()
			j.TimeRequired = int64(0)
			db.DbAdapter.UpdateJob(&j)
			return err
		}

		num := len(objs)
		if num == 0 {
			break
		}
		// migration start
		//Do migration for each object.
		logger.Printf("Migration Started. \n")
		go doMove(ctx, objs, capa, th, srcLoca, destLoca, in.RemainSource, &j)
		if len(objs) < int(limit) {
			break
		}
		offset = offset + int32(num)
	}

	var capacity, count, passedCount, totalObjs int64 = 0, 0, 0, j.TotalCount
	tmout := false
	for {
		select {
		case c := <-capa:
			{ //if c is less than 0, that means the object is migrated failed.
				count++
				if c >= 0 {
					passedCount++
					capacity += c
				}

				var deci int64 = totalObjs / 10
				if totalObjs < 100 || count == totalObjs || count%deci == 0 {
					//update database
					j.PassedCount = (int64(passedCount))
					//j.PassedCapacity = capacity
					if capacity == j.TotalCapacity {
						j.TimeRequired = int64(0)
						j.Progress = int64(capacity * 100 / j.TotalCapacity)
					}
					logger.Printf("[INFO] Passed capacity:%d,TotalCapacity:%d Progress:%d\n", capacity, j.TotalCapacity, j.Progress)
					if capacity ==j.TotalCapacity{
						logger.Printf("[INFO] Migration Completed Successfully.")
					}
					db.DbAdapter.UpdateJob(&j)
				}
			}
		case <-time.After(time.Duration(JOB_RUN_TIME_MAX) * time.Second):
			{
				tmout = true
				logger.Println("Timout.")
				j.Msg = "Time out"
			}
		}
		if count >= totalObjs || tmout {
			log.Printf("break, capacity=%d, timout=%v, count=%d, passed count=%d\n", capacity, tmout, count, passedCount)
			close(capa)
			close(th)
			break
		}
	}

	var ret error = nil
	j.PassedCount = int64(passedCount)
	if passedCount < totalObjs {
		errmsg := strconv.FormatInt(totalObjs, 10) + " objects, passed " + strconv.FormatInt(passedCount, 10)
		if j.Msg == "Migration Aborted" {
			logger.Printf("[INFO]: run job aborted: %s\n", errmsg)
			logger.Printf("Migration aborted successfully")
		} else {
			logger.Printf("run job failed: %s\n", errmsg)
		}
		ret = errors.New("failed")
		if j.Msg == "" {
			if totalObjs > 1 {
				j.Msg = "Migration failed: " + strconv.FormatInt(passedCount, 10) + " object migrated out of " + strconv.FormatInt(totalObjs, 10) + " objects"
			} else {
				j.Msg = "Migration failed: " + strconv.FormatInt(passedCount, 10) + " object migrated out of " + strconv.FormatInt(totalObjs, 10) + " object"
			}
		}
		if j.Status != flowtype.JOB_STATUS_ABORTED {
			j.Status = flowtype.JOB_STATUS_FAILED
		}
	} else {

		if totalObjs > 1 {
			j.Msg = "Migration Successful: " + strconv.FormatInt(totalObjs, 10) + " objects migrated"
		} else {
			j.Msg = "Migration Successful: " + strconv.FormatInt(totalObjs, 10) + " object migrated"
		}
		j.TimeRequired = int64(0)
		j.Status = flowtype.JOB_STATUS_SUCCEED
	}

	j.EndTime = time.Now()
	for i := 1; i <= 3; i++ {
		err := db.DbAdapter.UpdateJob(&j)
		if err == nil {
			break
		}
		if i == 3 {
			logger.Printf(" [ERROR] update the finish status of job in database failed three times, no need to try more.")
		}
	}
	defer logfile.Close()
	return ret
}
func progressTimeCalculation(job *model.Job, size int64, wt float64, start_time time.Time) {

	jobid := fmt.Sprintf("%x", string(job.Id))
	logfile, err = os.OpenFile(filepath+jobid+".txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
	}

	logger := log.New(logfile, "", log.LstdFlags)
	var mw = io.MultiWriter(logfile, os.Stdout)
	logger.SetOutput(mw)
	if wt >= 40 {
		job.PassedCapacity = job.PassedCapacity + wt*float64(size)/100
		job.PassedCapacity = math.Round(job.PassedCapacity*100) / 100
		speed := float64(size) / float64(time.Now().Sub(start_time).Seconds())
		if job.Avg < speed {
			job.Avg = speed
		} //else {	if speed < 0.1*job.Avg{
		//	job.Avg=speed
		//}
		job.TimeRequired = 3 * int64((float64(job.TotalCapacity)*(1-(WT_DELETE/100))-job.PassedCapacity)*100/(WT_UPLOAD*job.Avg))
		job.Progress = int64(job.PassedCapacity * 100 / float64(job.TotalCapacity))
		//job.Progress= job.Progress+float64(WT_DOWLOAD*float64(size/job.TotalCapacity))
		logger.Printf("[INFO] Progress %d  Time-required = %d seconds", job.Progress, int64(job.TimeRequired))
		db.DbAdapter.UpdateJob(job)
	} else {
		PassedCapacity := job.PassedCapacity + float64(size)*(wt/100)
		job.PassedCapacity = math.Round(PassedCapacity*100) / 100
		job.Progress = int64(job.PassedCapacity * 100 / float64(job.TotalCapacity))
		logger.Printf("[INFO] Progress %d", job.Progress)
		db.DbAdapter.UpdateJob(job)
	}
	defer logfile.Close()
}
