package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	as_aws "github.com/buildbarn/bb-autoscaler/pkg/cloud/aws"
	"github.com/buildbarn/bb-autoscaler/pkg/proto/configuration/bb_asg_lifecycle_hook"
	"github.com/buildbarn/bb-remote-execution/pkg/proto/buildqueuestate"
	"github.com/buildbarn/bb-storage/pkg/cloud/aws"
	"github.com/buildbarn/bb-storage/pkg/global"
	"github.com/buildbarn/bb-storage/pkg/program"
	"github.com/buildbarn/bb-storage/pkg/util"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	program.RunMain(func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
		if len(os.Args) != 2 {
			return status.Error(codes.InvalidArgument, "Usage: bb_asg_lifecycle_hook bb_asg_lifecycle_hook.jsonnet")
		}
		var configuration bb_asg_lifecycle_hook.ApplicationConfiguration
		if err := util.UnmarshalConfigurationFromFile(os.Args[1], &configuration); err != nil {
			return util.StatusWrapf(err, "Failed to read configuration from %s", os.Args[1])
		}
		lifecycleState, grpcClientFactory, err := global.ApplyConfiguration(configuration.Global)
		if err != nil {
			return util.StatusWrap(err, "Failed to apply global configuration options")
		}

		// AWS session for fetching EC2 lifecycle events from SQS.
		cfg, err := aws.NewConfigFromConfiguration(configuration.AwsSession, "LifecycleHookSQSMessageHandler")
		if err != nil {
			return util.StatusWrap(err, "Failed to create AWS session")
		}
		autoScalingClient := autoscaling.NewFromConfig(cfg)
		sqsClient := sqs.NewFromConfig(cfg)

		// gRPC client for marking workers in the scheduler as
		// terminating.
		schedulerConnection, err := grpcClientFactory.NewClientFromConfiguration(configuration.Scheduler)
		if err != nil {
			return util.StatusWrap(err, "Failed to create scheduler RPC client")
		}
		schedulerClient := buildqueuestate.NewBuildQueueStateClient(schedulerConnection)

		r := as_aws.NewSQSReceiver(
			sqsClient,
			configuration.SqsUrl,
			10*time.Minute,
			as_aws.NewLifecycleHookSQSMessageHandler(
				autoScalingClient,
				as_aws.NewBuildQueueLifecycleHookHandler(
					schedulerClient,
					configuration.InstanceIdLabel)),
			util.DefaultErrorLogger)

		lifecycleState.MarkReadyAndWait(siblingsGroup)

		// Process EC2 lifecycle events from SQS.
		for {
			if err := r.PerformSingleRequest(ctx, siblingsGroup); err != nil {
				log.Print("Failed to receive messages from SQS: ", err)
				t := time.NewTimer(10 * time.Second)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return nil
				}
			}
		}
	})
}
