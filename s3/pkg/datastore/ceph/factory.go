package ceph

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/opensds/multi-cloud/backend/pkg/utils/constants"
	backendpb "github.com/opensds/multi-cloud/backend/proto"
	"github.com/opensds/multi-cloud/s3/pkg/datastore/driver"
)

type CephS3DriverFactory struct {
}

func (factory *CephS3DriverFactory) CreateDriver(backend *backendpb.BackendDetail) (driver.StorageDriver, error) {
	endpoint := backend.Endpoint
	AccessKeyID := backend.Access
	AccessKeySecret := backend.Security
	region := backend.Region

	s3aksk := s3Cred{ak: AccessKeyID, sk: AccessKeySecret}
	creds := credentials.NewCredentials(&s3aksk)
	s3forcepathstyle := true
	disableSSL := true
	sess, err := session.NewSession(&aws.Config{
		Region:           &region,
		Endpoint:         &endpoint,
		Credentials:      creds,
		DisableSSL:       &disableSSL,
		S3ForcePathStyle: &s3forcepathstyle,
	})
	if err != nil {
		return nil, err
	}

	adap := &CephAdapter{backend: backend, session: sess}

	return adap, nil
}

func init() {
	driver.RegisterDriverFactory(constants.BackendTypeCeph, &CephS3DriverFactory{})
}
