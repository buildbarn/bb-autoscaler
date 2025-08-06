package aws_test

import (
	"context"
	"testing"

	"github.com/buildbarn/bb-autoscaler/internal/mock"
	as_aws "github.com/buildbarn/bb-autoscaler/pkg/cloud/aws"
	"github.com/buildbarn/bb-remote-execution/pkg/proto/buildqueuestate"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"go.uber.org/mock/gomock"
)

func TestBuildQueueLifecycleHookHandler(t *testing.T) {
	ctrl := gomock.NewController(t)

	buildQueue := mock.NewMockBuildQueueStateClient(ctrl)
	lhh := as_aws.NewBuildQueueLifecycleHookHandler(buildQueue, "instance")

	t.Run("Succes", func(t *testing.T) {
		buildQueue.EXPECT().TerminateWorkers(
			context.Background(),
			&buildqueuestate.TerminateWorkersRequest{
				WorkerIdPattern: map[string]string{
					"instance": "i-59438147357578398",
				},
			}).Return(&emptypb.Empty{}, nil)

		require.NoError(t, lhh.HandleEC2InstanceTerminating("i-59438147357578398"))
	})

	t.Run("Failure", func(t *testing.T) {
		buildQueue.EXPECT().TerminateWorkers(
			context.Background(),
			&buildqueuestate.TerminateWorkersRequest{
				WorkerIdPattern: map[string]string{
					"instance": "i-59438147357578398",
				},
			}).Return(nil, status.Error(codes.Internal, "Server on fire"))

		require.Equal(
			t,
			status.Error(codes.Internal, "Server on fire"),
			lhh.HandleEC2InstanceTerminating("i-59438147357578398"))
	})
}
