package main

import (
	"context"
	"log"
	"math"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-autoscaler/pkg/proto/configuration/bb_autoscaler"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
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
		instanceName string
		platform     string
	}
	vector := result.(model.Vector)
	desiredWorkersMap := make(map[desiredWorkersKey]float64, len(vector))
	for _, sample := range vector {
		instanceName, ok := sample.Metric["instance_name"]
		if !ok {
			log.Fatalf("Metric %s does not contain an \"instance_name\" label", sample.Metric)
		}

		platformStr, ok := sample.Metric["platform"]
		if !ok {
			log.Fatalf("Metric %s does not contain a \"platform\" label", sample.Metric)
		}
		var platform remoteexecution.Platform
		if err := jsonpb.UnmarshalString(string(platformStr), &platform); err != nil {
			log.Fatal("Failed to unmarshal \"platform\" label of metric %s: %s", sample.Metric, err)
		}

		desiredWorkersMap[desiredWorkersKey{
			instanceName: string(instanceName),
			platform:     proto.MarshalTextString(&platform),
		}] = float64(sample.Value)
	}

	log.Print("[2/2] Adjusting desired capacity of ASGs")
	autoScaling := autoscaling.New(session.New())
	for _, nodeGroup := range configuration.NodeGroups {
		var marshaler jsonpb.Marshaler
		platformStr, _ := marshaler.MarshalToString(nodeGroup.Platform)
		log.Printf("Instance %#v platform %s", nodeGroup.InstanceName, platformStr)
		workersPerCapacityUnit := nodeGroup.WorkersPerCapacityUnit
		log.Print("Workers per capacity unit: ", workersPerCapacityUnit)

		// Obtain the desired number of workers from the
		// Prometheus metrics gathered previously.
		desiredWorkers, ok := desiredWorkersMap[desiredWorkersKey{
			instanceName: nodeGroup.InstanceName,
			platform:     proto.MarshalTextString(nodeGroup.Platform),
		}]
		if ok {
			log.Print("Desired number of workers: ", desiredWorkers)

			// Obtain the minimum/maximum size of the ASG,
			// so that we can clamp the desired capacity.
			output, err := autoScaling.DescribeAutoScalingGroups(
				&autoscaling.DescribeAutoScalingGroupsInput{
					AutoScalingGroupNames: []*string{&nodeGroup.AutoScalingGroupName},
				})
			if err != nil {
				log.Fatalf("Failed to obtain properties of ASG %#v: %s", nodeGroup.AutoScalingGroupName, err)
			}
			if len(output.AutoScalingGroups) != 1 {
				log.Fatalf("Obtaining properties of ASG %#v returned %d entries", nodeGroup.AutoScalingGroupName, len(output.AutoScalingGroups))
			}
			asg := output.AutoScalingGroups[0]
			log.Print("ASG minimum size: ", *asg.MinSize)
			log.Print("ASG maximum size: ", *asg.MaxSize)

			// Translate the desired number of workers to
			// the desired ASG capacity.
			desiredCapacity := (int64(math.Ceil(desiredWorkers)) + workersPerCapacityUnit - 1) / workersPerCapacityUnit
			if desiredCapacity < *asg.MinSize {
				desiredCapacity = *asg.MinSize
			}
			if desiredCapacity > *asg.MaxSize {
				desiredCapacity = *asg.MaxSize
			}

			// Apply the desired ASG capacity.
			if desiredCapacity == *asg.DesiredCapacity {
				log.Print("Leaving desired capacity at ", desiredCapacity)
			} else {
				log.Printf("Changing desired capacity from %d to %d", *asg.DesiredCapacity, desiredCapacity)
				if _, err := autoScaling.SetDesiredCapacity(&autoscaling.SetDesiredCapacityInput{
					AutoScalingGroupName: &nodeGroup.AutoScalingGroupName,
					DesiredCapacity:      &desiredCapacity,
				}); err != nil {
					log.Fatalf("Failed to set desired capacity of ASG %#v: %s", nodeGroup.AutoScalingGroupName, err)
				}
			}
		} else {
			log.Print("WARNING: Prometheus did not return a desired number of workers")
		}
	}
}
