package alibaba

import (
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/opensds/multi-cloud/backend/pkg/utils/constants"
	backendpb "github.com/opensds/multi-cloud/backend/proto"
	"github.com/opensds/multi-cloud/s3/pkg/datastore/driver"
	_ "os"
)

type AlibabaDriverFactory struct {
}

func (cdf *AlibabaDriverFactory) CreateDriver(backend *backendpb.BackendDetail) (driver.StorageDriver, error) {
	endpoint := backend.Endpoint
	AccessKeyID := backend.Access
	AccessKeySecret := backend.Security

	client, err := oss.New(AccessKeyID, AccessKeySecret, endpoint)
	if err != nil {
		return nil, err
	}

	adap := &OSSAdapter{backend: backend, client: client}

	return adap, nil
}

func init() {
	driver.RegisterDriverFactory(constants.BackendTypeAlibaba, &AlibabaDriverFactory{})
}
