module github.com/buildbarn/bb-autoscaler

go 1.16

replace github.com/gordonklaus/ineffassign => github.com/gordonklaus/ineffassign v0.0.0-20201223204552-cba2d2a1d5d9

require (
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.12.1
	github.com/aws/aws-sdk-go-v2/service/eks v1.10.1
	github.com/bazelbuild/remote-apis v0.0.0-20211004185116-636121a32fa7
	github.com/buildbarn/bb-storage v0.0.0-20211009063419-74e925917e4c
	github.com/golang/protobuf v1.5.2
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.31.1
	google.golang.org/protobuf v1.27.1
)
