module github.com/buildbarn/bb-autoscaler

go 1.16

replace github.com/gordonklaus/ineffassign => github.com/gordonklaus/ineffassign v0.0.0-20201223204552-cba2d2a1d5d9

require (
	github.com/aws/aws-sdk-go v1.40.14
	github.com/bazelbuild/remote-apis v0.0.0-20210718193713-0ecef08215cf
	github.com/buildbarn/bb-storage v0.0.0-20210804073654-6536dcb16de6
	github.com/golang/protobuf v1.5.2
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.30.0
	google.golang.org/protobuf v1.27.1
)
