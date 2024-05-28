# Buildbarn Autoscaler [![Build status](https://github.com/buildbarn/bb-autoscaler/workflows/master/badge.svg)](https://github.com/buildbarn/bb-autoscaler/actions) [![PkgGoDev](https://pkg.go.dev/badge/github.com/buildbarn/bb-autoscaler)](https://pkg.go.dev/github.com/buildbarn/bb-autoscaler) [![Go Report Card](https://goreportcard.com/badge/github.com/buildbarn/bb-autoscaler)](https://goreportcard.com/report/github.com/buildbarn/bb-autoscaler)

This repository provides a utility named `bb_autoscaler` that can be used in
combination with [Buildbarn Remote Execution](https://github.com/buildbarn/bb-remote-execution)
to automatically adjust the size of Amazon EC2
[Auto Scaling Groups (ASGs)](https://docs.aws.amazon.com/autoscaling/ec2/userguide/AutoScalingGroup.html),
EKS [Managed Node Groups](https://docs.aws.amazon.com/eks/latest/userguide/managed-node-groups.html),
or Kubernetes [deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)/
[stateful sets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
of Buildbarn workers based on load. Load metrics are obtained by
querying Prometheus, which in its turn extracts metrics from
`bb_scheduler`. It relies on Prometheus to normalize the load metrics
into a desired number of workers (e.g., by using
[`quantile_over_time()`](https://prometheus.io/docs/prometheus/latest/querying/functions/#aggregation_over_time)).

Furthermore, this repository provides a tool named
`bb_asg_lifecycle_hook`, which may be used to gracefully downscale
workers running on plain EC2 instances, using
[EC2 ASG lifecycle hooks](https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html).

Note that it may not always be necessary to use utilities like these.
When using Kubernetes, it may be sufficient to create a
[Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
that uses the [Custom Metrics API](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/instrumentation/custom-metrics-api.md).
Using these tools may still be preferable if it is undesirable to
reconfigure your cluster to use the Custom Metrics API.
