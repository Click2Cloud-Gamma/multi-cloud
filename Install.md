# Installation Steps for S2C Feature in Cloud Brain

### Install Dependencies

```cassandraql
apt-get update && apt-get install -y git make curl wget libltdl7 libseccomp2 libffi-dev gawk
```

### Install Docker

```cassandraql
wget https://download.docker.com/linux/ubuntu/dists/xenial/pool/stable/amd64/docker-ce_18.06.1~ce~3-0~ubuntu_amd64.deb
dpkg -i docker-ce_18.06.1~ce~3-0~ubuntu_amd64.deb
```

### Install docker-compose

```cassandraql
curl -L "https://github.com/docker/compose/releases/download/1.23.1/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose
```

### Install Golang

```cassandraql
wget https://storage.googleapis.com/golang/go1.12.1.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.12.1.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
echo 'export GOPATH=$HOME/gopath' >> /etc/profile
source /etc/profile
```

### Clone OpenSDS Multi-Cloud branch from Click2Cloud

```cassandraql
mkdir -p /root/gopath/src/github.com/opensds/multi-cloud
git clone -b cloud_brain_features  https://github.com/Click2Cloud/multi-cloud-pv.git
```
* Enter Username: Click2Cloud-Gamma
* Password: Root#123$$

```cassandraql
cp -rf multi-cloud-pv  /root/gopath/src/github.com/opensds/multi-cloud
cd /root/gopath/src/github.com/opensds/multi-cloud
make docker
```

##### NOTE: If mongoDB is installed manually on machine then replace “datastore” with “IP address” in “docker-compose.yaml” file and use “authentication-type”=noauth

### Check status & Logs:
```cassandraql
docker ps -a
docker logs { container ID }
```

## Testing

#### Step- 1: Make a migration plan
* POST method : http://{{ 127.0.0.1 }}:8089/v1/adminTenantId/plans
###### Request body 
````cassandraql
{
    "name":"awstoXYZ",
    "sourceConn":{"storType":"aws-s3","connConfig":[
                 {"key":"endpoint","value":"s3.us-east.cloud-object-storage.appdomain.cloud"},
	         {"key":"bucketname","value":"test-abhi"},
		 {"key":"region","value":"us-east"},
		 {"key":"access","value":"671386ae7e2f492b90d7c"},
	         {"key":"security","value":"4f3a43ffa3d1b87b04b1ddf29aa565ecee"}
        ]},

 

    "destConn":{"storType":"aws-s3","connConfig":[
               {"key":"endpoint","value":"s3.amazonaws.com"},
               {"key":"bucketname","value":"opensdstest1"},
               {"key":"region","value":"us-east-1"},
               {"key":"access","value":"AKIAIWRZ7PFY2AQKUA"},
               {"key":"security","value":"gAGHdjk6h0Odi5Mr0PDE5f5KlmMgyetBTyN"}
          ]},
    "type":"migration",
    "remainSource": true
}
````

###### Response body

```cassandraql
{
    "plan": {
        "id": "5d393b27cfa3c95358c9f394",
        "name": "awstoXYZ",
        "type": "migration",
        "sourceConn": {
            "storType": "aws-s3",
            "connConfig": [
                {
                    "key": "endpoint",
                    "value": "s3.us-east.cloud-object-storage.appdomain.cloud"
                },
                {
                    "key": "bucketname",
                    "value": "test-abhi"
                },
                {
                    "key": "region",
                    "value": "us-east"
                },
                {
                    "key": "access",
                    "value": "671386ae7e2f492b90ea04778893d87c"
                },
                {
                    "key": "security",
                    "value": "4f3a43ffa3ea439d245a3ca561b87b04b1ddf29aa565ecee"
                }
            ]
        },
        "destConn": {
            "storType": "aws-s3",
            "connConfig": [
                {
                    "key": "endpoint",
                    "value": "s3.amazonaws.com"
                },
                {
                    "key": "bucketname",
                    "value": "opensdstest1"
                },
                {
                    "key": "region",
                    "value": "us-east-1"
                },
                {
                    "key": "access",
                    "value": "AKIAIWRZ7P7V6Y2AQKUA"
                },
                {
                    "key": "security",
                    "value": "gAGHdjk6h0OtsliVSi5Mr0PDE5f5KlmMgyetBTyN"
                }
            ]
        },
        "filter": {},
        "remainSource": true,
        "tenantId": "adminTenantId"
    }
}
```

#### Step-2 To run the migration plan

* POST method : http://{{ 127.0.0.1 }}:8089/v1/adminTenantId/plans/{{ Plan-ID }}/run

###### Response body 

```cassandraql
{
    "jobId": "5d393b31cfa3c95358c9f395"
}
```

##### To get Job Status with Progress 

* GET method: http://{{ 127.0.0.1 }}:8089/v1/adminTenantId/jobs/{{ JOB-ID }}

###### Response Body
```cassandraql
{
    "job": {
        "id": "5d393b31cfa3c95358c9f395",
        "type": "migration",
        "planName": "awbazssszzee",
        "planId": "5d393b27cfa3c95358c9f394",
        "description": "for test",
        "sourceLocation": "test-abhi",
        "destLocation": "opensdstest1",
        "status": "running",
        "createTime": 1564031793,
        "endTime": -62135596800,
        "totalCapacity": 3322468738,
        "passedCapacity": 883445736.4,
        "totalCount": 9,
        "passedCount": 6,
        "progress": 26
    }
}
```

##### To get Log for any specific Job

* GET method: http://{{ 127.0.0.1 }}:8089/v1/adminTenantId/jobs/{{ JOB-ID }}/logs


References for API:
1. [OpenAPI doc](http://petstore.swagger.io/?url=https://raw.githubusercontent.com/opensds/multi-cloud/master/openapi-spec/swagger.yaml)
