package autoscaler

import (
	"strconv"

	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/common/model"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
)

// SizeClassQueueKey can be used as a map key to uniquely identify a
// size class queue that is tracked by bb_scheduler. SizeClassQueueKeys
// can both be constructed from Protobuf fields stored in a
// configuration file, and from Prometheus metric names.
type SizeClassQueueKey struct {
	instanceNamePrefix string
	platform           string
	sizeClass          uint32
}

// NewSizeClassQueueKeyFromConfiguration creates a SizeClassQueueKey
// based on values provided in the bb_autoscaler configuration file.
func NewSizeClassQueueKeyFromConfiguration(instanceNamePrefix string, platform *remoteexecution.Platform, sizeClass uint32) SizeClassQueueKey {
	return SizeClassQueueKey{
		instanceNamePrefix: instanceNamePrefix,
		platform:           prototext.Format(platform),
		sizeClass:          sizeClass,
	}
}

// NewSizeClassQueueKeyFromMetric creates a SizeClassQueueKey based on
// labels in a Prometheus metric name.
func NewSizeClassQueueKeyFromMetric(metric model.Metric) (SizeClassQueueKey, error) {
	// Don't require instance_name_prefix to be present.
	// When empty, Prometheus omits the label entirely.
	instanceNamePrefix := metric["instance_name_prefix"]

	platformStr, ok := metric["platform"]
	if !ok {
		return SizeClassQueueKey{}, status.Error(codes.InvalidArgument, "Metric does not contain a \"platform\" label")
	}
	var platform remoteexecution.Platform
	if err := protojson.Unmarshal([]byte(platformStr), &platform); err != nil {
		return SizeClassQueueKey{}, util.StatusWrapWithCode(err, codes.InvalidArgument, "Failed to unmarshal \"platform\" label")
	}

	sizeClassStr, ok := metric["size_class"]
	if !ok {
		return SizeClassQueueKey{}, status.Error(codes.InvalidArgument, "Metric does not contain a \"size_class\" label")
	}
	sizeClass, err := strconv.ParseUint(string(sizeClassStr), 10, 32)
	if err != nil {
		return SizeClassQueueKey{}, util.StatusWrapWithCode(err, codes.InvalidArgument, "Failed to parse \"size_class\" label")
	}

	return SizeClassQueueKey{
		instanceNamePrefix: string(instanceNamePrefix),
		platform:           prototext.Format(&platform),
		sizeClass:          uint32(sizeClass),
	}, nil
}
