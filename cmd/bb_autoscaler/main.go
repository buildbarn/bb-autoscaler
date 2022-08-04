package main

import (
	"context"
	"log"
	"math"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/buildbarn/bb-autoscaler/pkg/autoscaler"
	"github.com/buildbarn/bb-autoscaler/pkg/proto/configuration/bb_autoscaler"
	"github.com/buildbarn/bb-storage/pkg/cloud/aws"
	bb_http "github.com/buildbarn/bb-storage/pkg/http"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"google.golang.org/protobuf/encoding/protojson"
)

// bb_autoscaler: Automatically adjust the capacity of Amazon EC2 Auto
// Scaling Groups (ASG) based on Prometheus metrics.
//
// This utility can be used to automatically scale the number of
// Buildbarn workers based on queue size metrics exposed by
// bb_scheduler. It is intended to run periodically (e.g., through a
// Kubernetes cron job).
//
// This tool is written in such a way that it almost literally takes the
// values obtained from Prometheus and uses those as the desired ASG
// capacity. Any smartness in the autoscaling behavior should be added
// by using PromQL functions such as quantile_over_time(),
// max_over_time(), etc.

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: bb_autoscaler bb_autoscaler.jsonnet")
	}
	var configuration bb_autoscaler.ApplicationConfiguration
	if err := util.UnmarshalConfigurationFromFile(os.Args[1], &configuration); err != nil {
		log.Fatalf("Failed to read configuration from %s: %s", os.Args[1], err)
	}

	// Obtain desired number of workers from Prometheus.
	log.Printf("[1/2] Fetching desired worker count from Prometheus by running query %#v", configuration.PrometheusQuery)
	prometheusRoundTripper, err := bb_http.NewRoundTripperFromConfiguration(configuration.PrometheusHttpClient)
	if err != nil {
		log.Fatal("Failed to create Prometheus HTTP client: ", err)
	}
	prometheusClient, err := api.NewClient(api.Config{
		Address:      configuration.PrometheusEndpoint,
		RoundTripper: prometheusRoundTripper,
	})
	if err != nil {
		log.Fatal("Error creating Prometheus client: ", err)
	}
	ctx := context.Background()
	v1api := v1.NewAPI(prometheusClient)
	result, _, err := v1api.Query(ctx, configuration.PrometheusQuery, time.Now())
	if err != nil {
		log.Fatal("Error querying Prometheus: ", err)
	}

	// Parse the metrics returned by Prometheus and convert them to
	// a map that's indexed by the platform.
	vector := result.(model.Vector)
	desiredWorkersMap := make(map[autoscaler.SizeClassQueueKey]float64, len(vector))
	for _, sample := range vector {
		sizeClassQueueKey, err := autoscaler.NewSizeClassQueueKeyFromMetric(sample.Metric)
		if err != nil {
			log.Fatalf("Metric %s: %s", sample.Metric, err)
		}

		desiredWorkersMap[sizeClassQueueKey] = float64(sample.Value)
	}

	log.Print("[2/2] Adjusting desired capacity of ASGs")
	cfg, err := aws.NewConfigFromConfiguration(configuration.AwsSession, "Autoscaling")
	if err != nil {
		log.Fatal("Failed to create AWS session: ", err)
	}
	autoScalingClient := autoscaling.NewFromConfig(cfg)
	eksClient := eks.NewFromConfig(cfg)
	for _, nodeGroup := range configuration.NodeGroups {
		platform, _ := protojson.Marshal(nodeGroup.Platform)
		log.Printf("Instance name prefix %#v platform %s size class %d", nodeGroup.InstanceNamePrefix, string(platform), nodeGroup.SizeClass)
		workersPerCapacityUnit := nodeGroup.WorkersPerCapacityUnit
		log.Print("Workers per capacity unit: ", workersPerCapacityUnit)

		// Obtain the desired number of workers from the
		// Prometheus metrics gathered previously.
		desiredWorkers, ok := desiredWorkersMap[autoscaler.NewSizeClassQueueKeyFromConfiguration(
			nodeGroup.InstanceNamePrefix,
			nodeGroup.Platform,
			nodeGroup.SizeClass,
		)]
		if ok {
			log.Print("Desired number of workers: ", desiredWorkers)

			// Obtain the minimum/maximum size of the ASG,
			// so that we can clamp the desired capacity.
			var oldDesiredCapacity, minSize, maxSize int32
			switch kind := nodeGroup.Kind.(type) {
			case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName:
				output, err := autoScalingClient.DescribeAutoScalingGroups(
					ctx,
					&autoscaling.DescribeAutoScalingGroupsInput{
						AutoScalingGroupNames: []string{kind.AutoScalingGroupName},
					})
				if err != nil {
					log.Fatalf("Failed to obtain properties of ASG %#v: %s", kind.AutoScalingGroupName, err)
				}
				if len(output.AutoScalingGroups) != 1 {
					log.Fatalf("Obtaining properties of ASG %#v returned %d entries", kind.AutoScalingGroupName, len(output.AutoScalingGroups))
				}
				asg := output.AutoScalingGroups[0]
				oldDesiredCapacity = *asg.DesiredCapacity
				minSize = *asg.MinSize
				maxSize = *asg.MaxSize
			case *bb_autoscaler.NodeGroupConfiguration_EksManagedNodeGroup:
				output, err := eksClient.DescribeNodegroup(
					ctx,
					&eks.DescribeNodegroupInput{
						ClusterName:   &kind.EksManagedNodeGroup.ClusterName,
						NodegroupName: &kind.EksManagedNodeGroup.NodeGroupName,
					})
				if err != nil {
					log.Fatalf("Failed to obtain properties of EKS managed node group %#v in cluster %#v: %s", kind.EksManagedNodeGroup.NodeGroupName, kind.EksManagedNodeGroup.ClusterName, err)
				}
				scalingConfig := output.Nodegroup.ScalingConfig
				oldDesiredCapacity = *scalingConfig.DesiredSize
				minSize = *scalingConfig.MinSize
				maxSize = *scalingConfig.MaxSize
			default:
				log.Fatal("No ASG or EKS managed node group name specified")
			}
			log.Print("Node group minimum size: ", minSize)
			log.Print("Node group maximum size: ", maxSize)

			// Translate the desired number of workers to
			// the desired ASG capacity.
			newDesiredCapacity := (int32(math.Ceil(desiredWorkers)) + workersPerCapacityUnit - 1) / workersPerCapacityUnit
			if newDesiredCapacity < minSize {
				newDesiredCapacity = minSize
			}
			if newDesiredCapacity > maxSize {
				newDesiredCapacity = maxSize
			}

			// Apply the desired ASG capacity.
			if newDesiredCapacity == oldDesiredCapacity {
				log.Print("Leaving desired capacity at ", newDesiredCapacity)
			} else {
				log.Printf("Changing desired capacity from %d to %d", oldDesiredCapacity, newDesiredCapacity)

				switch kind := nodeGroup.Kind.(type) {
				case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName:
					if _, err := autoScalingClient.SetDesiredCapacity(ctx, &autoscaling.SetDesiredCapacityInput{
						AutoScalingGroupName: &kind.AutoScalingGroupName,
						DesiredCapacity:      &newDesiredCapacity,
					}); err != nil {
						log.Fatalf("Failed to set desired capacity of ASG %#v: %s", kind.AutoScalingGroupName, err)
					}
				case *bb_autoscaler.NodeGroupConfiguration_EksManagedNodeGroup:
					if _, err := eksClient.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
						ClusterName:   &kind.EksManagedNodeGroup.ClusterName,
						NodegroupName: &kind.EksManagedNodeGroup.NodeGroupName,
						ScalingConfig: &types.NodegroupScalingConfig{
							DesiredSize: &newDesiredCapacity,
						},
					}); err != nil {
						log.Fatalf("Failed to set desired size of EKS managed node group %#v in cluster %#v: %s", kind.EksManagedNodeGroup.NodeGroupName, kind.EksManagedNodeGroup.ClusterName, err)
					}
				default:
					panic("Incomplete switch on node group kind")
				}
			}
		} else {
			log.Print("WARNING: Prometheus did not return a desired number of workers")
		}
	}
}
