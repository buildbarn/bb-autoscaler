module github.com/buildbarn/bb-autoscaler

go 1.16

replace github.com/gordonklaus/ineffassign => github.com/gordonklaus/ineffassign v0.0.0-20201223204552-cba2d2a1d5d9

require (
	github.com/aws/aws-sdk-go v1.37.28
	github.com/bazelbuild/remote-apis v0.0.0-20210309154856-0943dc4e70e1
	github.com/buildbarn/bb-storage v0.0.0-20210402082800-9b54035385d2
	github.com/golang/protobuf v1.4.3
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/common v0.15.0
)
