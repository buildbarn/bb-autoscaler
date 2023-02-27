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
	"github.com/buildbarn/bb-storage/pkg/program"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1_apply "k8s.io/client-go/applyconfigurations/apps/v1"
	metav1_apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	program.Run(func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
		if len(os.Args) != 2 {
			return status.Error(codes.InvalidArgument, "Usage: bb_autoscaler bb_autoscaler.jsonnet")
		}
		var configuration bb_autoscaler.ApplicationConfiguration
		if err := util.UnmarshalConfigurationFromFile(os.Args[1], &configuration); err != nil {
			return util.StatusWrapf(err, "Failed to read configuration from %s", os.Args[1])
		}

		// Obtain desired number of workers from Prometheus.
		log.Printf("[1/2] Fetching desired worker count from Prometheus by running query %#v", configuration.PrometheusQuery)
		prometheusRoundTripper, err := bb_http.NewRoundTripperFromConfiguration(configuration.PrometheusHttpClient)
		if err != nil {
			return util.StatusWrap(err, "Failed to create Prometheus HTTP client")
		}
		prometheusClient, err := api.NewClient(api.Config{
			Address:      configuration.PrometheusEndpoint,
			RoundTripper: prometheusRoundTripper,
		})
		if err != nil {
			return util.StatusWrap(err, "Error creating Prometheus client")
		}
		v1api := v1.NewAPI(prometheusClient)
		result, _, err := v1api.Query(ctx, configuration.PrometheusQuery, time.Now())
		if err != nil {
			return util.StatusWrap(err, "Error querying Prometheus")
		}

		// Parse the metrics returned by Prometheus and convert them to
		// a map that's indexed by the platform.
		vector := result.(model.Vector)
		desiredWorkersMap := make(map[autoscaler.SizeClassQueueKey]float64, len(vector))
		for _, sample := range vector {
			sizeClassQueueKey, err := autoscaler.NewSizeClassQueueKeyFromMetric(sample.Metric)
			if err != nil {
				return util.StatusWrapf(err, "Metric %s", sample.Metric)
			}

			desiredWorkersMap[sizeClassQueueKey] = float64(sample.Value)
		}

		// Construct clients needed to adjust node group and
		// deployment sizes.
		log.Print("[2/2] Adjusting desired capacity of ASGs")
		var autoScalingClient *autoscaling.Client
		var eksClient *eks.Client
		var kubernetesClientset *kubernetes.Clientset
		for _, nodeGroup := range configuration.NodeGroups {
			switch nodeGroup.Kind.(type) {
			case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName, *bb_autoscaler.NodeGroupConfiguration_EksManagedNodeGroup:
				if autoScalingClient == nil {
					cfg, err := aws.NewConfigFromConfiguration(configuration.AwsSession, "Autoscaling")
					if err != nil {
						return util.StatusWrap(err, "Failed to create AWS session")
					}
					autoScalingClient = autoscaling.NewFromConfig(cfg)
					eksClient = eks.NewFromConfig(cfg)
				}
			case *bb_autoscaler.NodeGroupConfiguration_KubernetesDeployment:
				if kubernetesClientset == nil {
					config, err := rest.InClusterConfig()
					if err != nil {
						return util.StatusWrap(err, "Failed to create Kubernetes client configuration")
					}
					kubernetesClientset, err = kubernetes.NewForConfig(config)
					if err != nil {
						return util.StatusWrap(err, "Failed to create Kubernetes client")
					}
				}
			default:
				return status.Error(codes.InvalidArgument, "No ASG, EKS managed node group, or Kubernetes deployment name specified")
			}
		}

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
				oldDesiredCapacity := int32(-1)
				var minSize, maxSize int32
				switch kind := nodeGroup.Kind.(type) {
				case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName:
					output, err := autoScalingClient.DescribeAutoScalingGroups(
						ctx,
						&autoscaling.DescribeAutoScalingGroupsInput{
							AutoScalingGroupNames: []string{kind.AutoScalingGroupName},
						})
					if err != nil {
						return util.StatusWrapf(err, "Failed to obtain properties of ASG %#v", kind.AutoScalingGroupName)
					}
					if len(output.AutoScalingGroups) != 1 {
						return status.Errorf(codes.FailedPrecondition, "Obtaining properties of ASG %#v returned %d entries", kind.AutoScalingGroupName, len(output.AutoScalingGroups))
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
						return util.StatusWrapf(err, "Failed to obtain properties of EKS managed node group %#v in cluster %#v: %s", kind.EksManagedNodeGroup.NodeGroupName, kind.EksManagedNodeGroup.ClusterName)
					}
					scalingConfig := output.Nodegroup.ScalingConfig
					oldDesiredCapacity = *scalingConfig.DesiredSize
					minSize = *scalingConfig.MinSize
					maxSize = *scalingConfig.MaxSize
				case *bb_autoscaler.NodeGroupConfiguration_KubernetesDeployment:
					minSize = kind.KubernetesDeployment.MinimumReplicas
					maxSize = kind.KubernetesDeployment.MaximumReplicas
				default:
					panic("Incomplete switch on node group kind")
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
					if oldDesiredCapacity >= 0 {
						log.Printf("Changing desired capacity from %d to %d", oldDesiredCapacity, newDesiredCapacity)
					} else {
						log.Printf("Changing desired capacity to %d", newDesiredCapacity)
					}

					switch kind := nodeGroup.Kind.(type) {
					case *bb_autoscaler.NodeGroupConfiguration_AutoScalingGroupName:
						if _, err := autoScalingClient.SetDesiredCapacity(ctx, &autoscaling.SetDesiredCapacityInput{
							AutoScalingGroupName: &kind.AutoScalingGroupName,
							DesiredCapacity:      &newDesiredCapacity,
						}); err != nil {
							return util.StatusWrapf(err, "Failed to set desired capacity of ASG %#v", kind.AutoScalingGroupName)
						}
					case *bb_autoscaler.NodeGroupConfiguration_EksManagedNodeGroup:
						if _, err := eksClient.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
							ClusterName:   &kind.EksManagedNodeGroup.ClusterName,
							NodegroupName: &kind.EksManagedNodeGroup.NodeGroupName,
							ScalingConfig: &types.NodegroupScalingConfig{
								DesiredSize: &newDesiredCapacity,
							},
						}); err != nil {
							return util.StatusWrapf(err, "Failed to set desired size of EKS managed node group %#v in cluster %#v: %s", kind.EksManagedNodeGroup.NodeGroupName, kind.EksManagedNodeGroup.ClusterName)
						}
					case *bb_autoscaler.NodeGroupConfiguration_KubernetesDeployment:
						namespace := kind.KubernetesDeployment.Namespace
						name := kind.KubernetesDeployment.Name
						metaKind := "Deployment"
						metaAPIVersion := "apps/v1"
						if _, err := kubernetesClientset.
							AppsV1().
							Deployments(namespace).
							Apply(
								ctx,
								&appsv1_apply.DeploymentApplyConfiguration{
									TypeMetaApplyConfiguration: metav1_apply.TypeMetaApplyConfiguration{
										Kind:       &metaKind,
										APIVersion: &metaAPIVersion,
									},
									ObjectMetaApplyConfiguration: &metav1_apply.ObjectMetaApplyConfiguration{
										Name:      &name,
										Namespace: &namespace,
										Annotations: map[string]string{
											"kubernetes.io/change-cause": "replicas updated by bb_autoscaler",
										},
									},
									Spec: &appsv1_apply.DeploymentSpecApplyConfiguration{
										Replicas: &newDesiredCapacity,
									},
								},
								metav1.ApplyOptions{
									FieldManager: "bb_autoscaler",
									Force:        true,
								}); err != nil {
							return util.StatusWrapf(err, "Failed to change number of replicas of Kubernetes deployment %#v in namespace %#v", name, namespace)
						}
					default:
						panic("Incomplete switch on node group kind")
					}
				}
			} else {
				log.Print("WARNING: Prometheus did not return a desired number of workers")
			}
		}
		return nil
	})
}
