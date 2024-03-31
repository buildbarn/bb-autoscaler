local workflows_template = import 'tools/github_workflows/workflows_template.libsonnet';

workflows_template.getWorkflows(
  [
    'bb_asg_lifecycle_hook',
    'bb_autoscaler',
  ],
  [
    'bb_asg_lifecycle_hook:bb_asg_lifecycle_hook',
    'bb_autoscaler:bb_autoscaler',
  ],
)
