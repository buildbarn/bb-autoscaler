package autoscaler_test

import (
	"testing"

	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-autoscaler/pkg/autoscaler"
	"github.com/buildbarn/bb-storage/pkg/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSizeClassQueueKeyFromMetric(t *testing.T) {
	t.Run("MissingPlatformLabel", func(t *testing.T) {
		_, err := autoscaler.NewSizeClassQueueKeyFromMetric(model.Metric{
			"instance_name_prefix": "my/prefix",
			"size_class":           "10",
		})
		testutil.RequireEqualStatus(t, status.Error(codes.InvalidArgument, "Metric does not contain a \"platform\" label"), err)
	})

	t.Run("InvalidPlatformLabel", func(t *testing.T) {
		_, err := autoscaler.NewSizeClassQueueKeyFromMetric(model.Metric{
			"instance_name_prefix": "my/prefix",
			"platform":             "This is not valid JSON",
			"size_class":           "10",
		})
		testutil.RequirePrefixedStatus(t, status.Error(codes.InvalidArgument, "Failed to unmarshal \"platform\" label: "), err)
	})

	t.Run("MissingSizeClassLabel", func(t *testing.T) {
		_, err := autoscaler.NewSizeClassQueueKeyFromMetric(model.Metric{
			"instance_name_prefix": "my/prefix",
			"platform":             "{\"properties\":[{\"name\": \"os\", \"value\": \"linux\"}]}",
		})
		testutil.RequireEqualStatus(t, status.Error(codes.InvalidArgument, "Metric does not contain a \"size_class\" label"), err)
	})

	t.Run("InvalidPlatformLabel", func(t *testing.T) {
		_, err := autoscaler.NewSizeClassQueueKeyFromMetric(model.Metric{
			"instance_name_prefix": "my/prefix",
			"platform":             "{\"properties\":[{\"name\": \"os\", \"value\": \"linux\"}]}",
			"size_class":           "not a number",
		})
		testutil.RequirePrefixedStatus(t, status.Error(codes.InvalidArgument, "Failed to parse \"size_class\" label: "), err)
	})

	t.Run("Success", func(t *testing.T) {
		key, err := autoscaler.NewSizeClassQueueKeyFromMetric(model.Metric{
			"instance_name_prefix": "my/prefix",
			"platform":             "{\"properties\":[{\"name\": \"os\", \"value\": \"linux\"}]}",
			"size_class":           "4",
		})
		require.NoError(t, err)

		require.Equal(t, autoscaler.NewSizeClassQueueKeyFromConfiguration("my/prefix", &remoteexecution.Platform{
			Properties: []*remoteexecution.Platform_Property{
				{Name: "os", Value: "linux"},
			},
		}, 4), key)
		require.NotEqual(t, autoscaler.NewSizeClassQueueKeyFromConfiguration("other/prefix", &remoteexecution.Platform{
			Properties: []*remoteexecution.Platform_Property{
				{Name: "os", Value: "linux"},
			},
		}, 4), key)
		require.NotEqual(t, autoscaler.NewSizeClassQueueKeyFromConfiguration("my/prefix", &remoteexecution.Platform{
			Properties: []*remoteexecution.Platform_Property{
				{Name: "os", Value: "windows"},
			},
		}, 4), key)
		require.NotEqual(t, autoscaler.NewSizeClassQueueKeyFromConfiguration("my/prefix", &remoteexecution.Platform{
			Properties: []*remoteexecution.Platform_Property{
				{Name: "os", Value: "linux"},
			},
		}, 8), key)
	})
}
