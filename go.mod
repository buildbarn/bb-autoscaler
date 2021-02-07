module github.com/buildbarn/bb-autoscaler

go 1.15

replace github.com/gordonklaus/ineffassign => github.com/gordonklaus/ineffassign v0.0.0-20201223204552-cba2d2a1d5d9

require (
	github.com/aws/aws-sdk-go v1.37.6
	github.com/bazelbuild/remote-apis v0.0.0-20201209220655-9e72daff42c9
	github.com/buildbarn/bb-storage v0.0.0-20210207101039-9507e33a5caf
	github.com/golang/protobuf v1.4.3
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/common v0.15.0
)
