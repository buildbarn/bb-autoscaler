package main

import (
	"context"
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/eks"
	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-autoscaler/pkg/proto/configuration/bb_autoscaler"
	"github.com/buildbarn/bb-storage/pkg/cloud/aws"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
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
	prometheusClient, err := api.NewClient(api.Config{
		Address: configuration.PrometheusEndpoint,
	})
	if err != nil {
		log.Fatal("Error creating Prometheus client: ", err)
	}
	v1api := v1.NewAPI(prometheusClient)
	result, _, err := v1api.Query(context.Background(), configuration.PrometheusQuery, time.Now())
	if err != nil {
		log.Fatal("Error querying Prometheus: ", err)
	}

	// Parse the metrics returned by Prometheus and convert them to
	// a map that's indexed by the platform.
	type desiredWorkersKey struct {
		instanceNamePrefix string
		platform           string
		sizeClass          uint32
	}
	vector := result.(model.Vector)
	desiredWorkersMap := make(map[desiredWorkersKey]float64, len(vector))
	for _, sample := range vector {
		// Don't require instance_name_prefix to be present.
		// When empty, Prometheus omits the label entirely.
		instanceNamePrefix := sample.Metric["instance_name_prefix"]

		platformStr, ok := sample.Metric["platform"]
		if !ok {
			log.Fatalf("Metric %s does not contain a \"platform\" label", sample.Metric)
		}
		var platform remoteexecution.Platform
		if err := protojson.Unmarshal([]byte(platformStr), &platform); err != nil {
			log.Fatal("Failed to unmarshal \"platform\" label of metric %s: %s", sample.Metric, err)
		}

		sizeClassStr, ok := sample.Metric["size_class"]
		if !ok {
			log.Fatalf("Metric %s does not contain a \"size_class\" label", sample.Metric)
		}
		sizeClass, err := strconv.ParseUint(string(sizeClassStr), 10, 32)
		if err != nil {
			log.Fatal("Failed to parse \"size_class\" label of metric %s: %s", sample.Metric, err)
		}

		desiredWorkersMap[desiredWorkersKey{
			instanceNamePrefix: string(instanceNamePrefix),
			platform:           prototext.Format(&platform),
			sizeClass:          uint32(sizeClass),
		}] = float64(sample.Value)
	}

	log.Print("[2/2] Adjusting desired capacity of ASGs")
	sess, err := aws.NewSessionFromConfiguration(configuration.AwsSession)
	if err != nil {
		log.Fatal("Failed to create AWS session: ", err)
	}
	autoScaling := autoscaling.New(sess)
	eksSession := eks.New(sess)
	for _, nodeGroup := range configuration.NodeGroups {
		platform, _ := protojson.Marshal(nodeGroup.Platform)
		log.Printf("Instance name prefix %#v platform %s size class %d", nodeGroup.InstanceNamePrefix, string(platform), nodeGroup.SizeClass)
		workersPerCapacityUnit := nodeGroup.WorkersPerCapacityUnit
		log.Print("Workers per capacity unit: ", workersPerCapacityUnit)

		// Obtain the desired number of workers from the
		// Prometheus metrics gathered previously.
		desiredWorkers, ok := desiredWorkersMap[desiredWorkersKey{
			instanceNamePrefix: nodeGroup.InstanceNamePrefix,
			platform:           prototext.Format(nodeGroup.Platform),
			sizeClass:          nodeGroup.SizeClass,
		}]
		if ok {
			log.Print("Desired number of workers: ", desiredWorkers)

			// Obtain the minimum/maximum size of the ASG,
			// so that we can clamp the desired capacity.
			var oldDesiredCapacity, minSize, maxSize int64
			switch kind := nodeGroup.Kind.(type) {
			case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName:
				output, err := autoScaling.DescribeAutoScalingGroups(
					&autoscaling.DescribeAutoScalingGroupsInput{
						AutoScalingGroupNames: []*string{&kind.AutoScalingGroupName},
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
				output, err := eksSession.DescribeNodegroup(
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
			newDesiredCapacity := (int64(math.Ceil(desiredWorkers)) + workersPerCapacityUnit - 1) / workersPerCapacityUnit
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
					if _, err := autoScaling.SetDesiredCapacity(&autoscaling.SetDesiredCapacityInput{
						AutoScalingGroupName: &kind.AutoScalingGroupName,
						DesiredCapacity:      &newDesiredCapacity,
					}); err != nil {
						log.Fatalf("Failed to set desired capacity of ASG %#v: %s", kind.AutoScalingGroupName, err)
					}
				case *bb_autoscaler.NodeGroupConfiguration_EksManagedNodeGroup:
					if _, err := eksSession.UpdateNodegroupConfig(&eks.UpdateNodegroupConfigInput{
						ClusterName:   &kind.EksManagedNodeGroup.ClusterName,
						NodegroupName: &kind.EksManagedNodeGroup.NodeGroupName,
						ScalingConfig: &eks.NodegroupScalingConfig{
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
