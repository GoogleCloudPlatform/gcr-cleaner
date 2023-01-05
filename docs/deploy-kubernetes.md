# Deploy GCR Cleaner on Google Kubernetes Engine cluster

This document describes how to deploy GCR Cleaner on a Google Kubernetes Engine ([GKE][gke]) cluster.

1. Create a [GKE][gke] cluster

1. Enable and configure [Workload Identity][wi] on your cluster

1. Create a new Google Cloud IAM service account 

1. Ensure that your Google Cloud IAM service account has the right [Permissions](../README.md#permissions)

1. Create a Kubernetes service account `gcr-cleaner`

1. Allow the Kubernetes service account `gcr-cleaner` to impersonate the IAM service account by adding an IAM policy binding between the two service accounts. 
This binding allows the Kubernetes service account `gcr-cleaner` to act as the IAM service account.

1. Annotate the Kubernetes service account `gcr-cleaner` with the email address of the IAM service account

1. Create a Kubernetes namespace `gcr-cleaner`

1. Create a Kubernetes [CronJob][cronjob] resource:
```yaml
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: gcr-cleaner
  namespace: gcr-cleaner
spec:
  schedule: '0 0 */1 * *' # runs daily
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: gcr-cleaner
          restartPolicy: OnFailure
          containers:
          - name: gcr-cleaner
            image: google/cloud-sdk:latest
            imagePullPolicy: Always
            env:
            - name: GCRCLEANER_LOG
              value: debug
              args: ["gcloud", "auth", "list"]
```

## Troubleshooting

### GCR Cleaner log

It's possible to set logger to debug level. This bellow:

```yaml
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: gcr-cleaner
  namespace: gcr-cleaner
spec:
  schedule: '* * * * *' # runs at every minute
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: gcr-cleaner
          restartPolicy: OnFailure
          containers:
          - name: gcr-cleaner
            image: google/cloud-sdk:latest
            imagePullPolicy: Always
            # ▼▼▼ Set logger to debug level ▼▼▼
            env:
            - name: GCRCLEANER_LOG
              value: debug
              args: ["gcloud", "auth", "list"]
            # ▲▲▲ Set logger to debug level ▲▲▲
```

### Workload Identity

For troubleshooting information, refer to [Troubleshooting Workload Identity][twi].

[gke]: https://cloud.google.com/kubernetes-engine
[wi]: https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity
[cronjob]: https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/
[twi]: https://cloud.google.com/kubernetes-engine/docs/troubleshooting/troubleshooting-security#workload-identity
