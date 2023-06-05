# Deploy GCR Cleaner to Kubernetes Engine

This document describes how to deploy GCR Cleaner to [Kubernetes Engine][kubernetes-engine] as a [CronJob][cron-job].


1. Export your project ID as an environment variable. The rest of this setup
   assumes this environment variable is set.

   ```sh
   export PROJECT_ID="my-project"
   ```

   Note this is your project _ID_, not the project _number_ or _name_.

1. Export the Kubernetes namespace as an environment variable. This is the namespace gcr-cleaner will be deployed.

   ```sh
   export NAMESPACE="my-namespace"
   ```

1. Create a service account which will be assigned to the CronJob:

    ```sh
    gcloud iam service-accounts create "gcr-cleaner" \
      --project "${PROJECT_ID}" \
      --display-name "gcr-cleaner"
    ```
   
1. Add the [Artifact Registry Repository Administrator][artifact-repo-admin] role to the service account:

   ```sh
   gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
     --member "serviceAccount:gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com" \
     --role "roles/artifactregistry.repoAdmin"
   ```
   
1. (Optional) In order to use the `-recursive`, add [Storage Object Viewer][storage-object-viewer] role to the service account:

   ```sh
   gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
     --member "serviceAccount:gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com" \
     --role "roles/storage.objectViewer"
   ```

1. Configure the service account to use [Workload Identity][workload-identity]:

   ```sh
   gcloud iam service-accounts add-iam-policy-binding gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com \
     --role roles/iam.workloadIdentityUser \
     --member "serviceAccount:${PROJECT_ID}.svc.id.goog[${NAMESPACE}/gcr-cleaner]"
   ```

1. Configure and apply the Kubernetes resources:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
   name: gcr-cleaner
   namespace: ${NAMESPACE}
   annotations:
      iam.gke.io/gcp-service-account: gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com
---
apiVersion: batch/v1
kind: CronJob
metadata:
   name: gcr-cleaner
   namespace: ${NAMESPACE}
spec:
   concurrencyPolicy: Forbid
   schedule: "0 0 */1 * *"  # every day
   jobTemplate:
      spec:
         template:
            metadata:
               labels:
                  app: gcr-cleaner
            spec:
               containers:
                  - name: gcr-cleaner
                    image: "us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli"
                    resources:
                       requests:
                          memory: "128Mi"
                          cpu: "250m"
                       limits:
                          memory: "256Mi"
                    args:
                       - -repo=us-central1-docker.pkg.dev/my-project/my-repo
                       - -grace=720h # 30 days
                       - -recursive
                       - -tag-filter-any=.*
               restartPolicy: Never
               serviceAccountName: gcr-cleaner
               nodeSelector:
                  iam.gke.io/gke-metadata-server-enabled: "true"
```


[kubernetes-engine]: https://cloud.google.com/kubernetes-engine
[cron-job]: https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/
[artifact-repo-admin]: https://cloud.google.com/iam/docs/understanding-roles#artifactregistry.repoAdmin
[storage-object-viewer]: https://cloud.google.com/iam/docs/understanding-roles#storage.objectViewer
[workload-identity]: https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity