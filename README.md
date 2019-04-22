# GCR Cleaner

[![GoDoc](https://godoc.org/github.com/sethvargo/gcr-cleaner?status.svg)][gcr-cleaner-godoc]

GCR Cleaner deletes untagged images in Google Container Registry. This can help
reduce costs and keep your container images list in order.

GCR Cleaner is available as a Go library (in `pkg/gcrcleaner`), but it is
designed to be deployed as a [Cloud Run][cloud-run] service and invoked
periodically via [Cloud Scheduler][cloud-scheduler] via [Cloud
Pub/Sub][cloud-pubsub].

```text
+-------------------+    +-----------------+    +-------------+    +-------+
|  Cloud Scheduler  | -> |  Cloud Pub/Sub  | -> |  Cloud Run  | -> |  GCR  |
+-------------------+    +-----------------+    +-------------+    +-------+
```

GCR Cleaner is largely inspired by [ahmetb](https://twitter.com/ahmetb)'s
[gcrgc.sh][gcrgc.sh], but it is written in Go and is designed to be run as a
service.


## Setup

1. Install the [Cloud SDK][cloud-sdk] for your operating system. Alternatively,
   you can run these commands from [Cloud Shell][cloud-shell], which has the SDK
   and other popular tools pre-installed.

1. Export your project ID as an environment variable. The rest of this setup
   assumes this environment variable is set.

   ```text
   PROJECT_ID=my-project
   ```

   Note this is your project _ID_, not the project _number_ or _name_.

1. Enable the Google APIs - this only needs to be done once per project:

    ```text
    gcloud services enable --project ${PROJECT_ID} \
      appengine.googleapis.com \
      compute.googleapis.com \
      cloudbuild.googleapis.com \
      cloudscheduler.googleapis.com \
      pubsub.googleapis.com \
      run.googleapis.com
    ```

    This operation can take a few minutes, especially for recently-created
    projects.

1. Build the `gcr-cleaner` container with Cloud Build:

    ```text
    gcloud builds submit \
      --project ${PROJECT_ID} \
      --tag gcr.io/${PROJECT_ID}/gcr-cleaner \
      .
    ```

1. Deploy the `gcr-cleaner` container on Cloud Run:

    ```text
    gcloud beta run deploy gcr-cleaner \
      --project ${PROJECT_ID} \
      --image gcr.io/${PROJECT_ID}/gcr-cleaner \
      --region us-central1 \
      --timeout 60s \
      --quiet
    ```

1. Capture the URL of the Cloud Run service:

    ```text
    SERVICE_URL=$(gcloud beta run services describe gcr-cleaner --project ${PROJECT_ID} --region us-central1 --format 'value(status.domain)')
    ```

1. Grant the Cloud Run service account access to delete references in Google
   Container Registry:

    ```text
    PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format 'value(projectNumber)')
    ```

    ```text
    gsutil acl ch -u ${PROJECT_NUMBER}-compute@developer.gserviceaccount.com:W gs://artifacts.${PROJECT_ID}.appspot.com
    ```

    To cleanup refs in _other_ GCP projects, replace `PROJECT_ID` with the
    target project ID. For example, if the Cloud Run service was running in
    "project-a" and you wanted to grant it permission to cleanup refs in
    "gcr.io/project-b/image", you would need to grant the Cloud Run service
    account in project-a permission on `artifacts.projects-b.appspot.com`.

1. Grant Cloud Pub/Sub the ability to invoke the Cloud Run function:

    ```text
    gcloud projects add-iam-policy-binding ${PROJECT_ID} \
      --member serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-pubsub.iam.gserviceaccount.com \
      --role roles/iam.serviceAccountTokenCreator
    ```

    ```text
    gcloud iam service-accounts create cloud-run-pubsub-invoker \
      --project ${PROJECT_ID} \
      --display-name "Cloud Run Pub/Sub Invoker"
    ```

    ```text
    gcloud beta run services add-iam-policy-binding gcr-cleaner \
      --project ${PROJECT_ID} \
      --member serviceAccount:cloud-run-pubsub-invoker@${PROJECT_ID}.iam.gserviceaccount.com \
      --role roles/run.invoker
    ```

1. Create a Cloud Pub/Sub topic and subscription with a service account that has
   permission to invoke Cloud Run:

    ```text
    gcloud pubsub topics create gcr-cleaner \
      --project ${PROJECT_ID}
    ```

    ```text
    gcloud beta pubsub subscriptions create cloud-run-gcr-cleaner \
      --project ${PROJECT_ID} \
      --topic gcr-cleaner \
      --message-retention-duration 15m \
      --push-endpoint ${SERVICE_URL}/pubsub \
      --push-auth-service-account cloud-run-pubsub-invoker@${PROJECT_ID}.iam.gserviceaccount.com
    ```

1. Create a Cloud Scheduler Pub/Sub job to invoke the function every day:

    ```text
    gcloud app create \
      --project ${PROJECT_ID} \
      --region us-central \
      --quiet
    ```

    ```text
    # Replace this with the full name of the repository for which you
    # want to cleanup old references.
    REPO="gcr.io/my-project/my-image"
    ```

    ```text
    gcloud beta scheduler jobs create pubsub gcrclean-myimage \
      --project ${PROJECT_ID} \
      --description "Cleanup ${REPO}" \
      --topic gcr-cleaner \
      --message-body "{\"repo\":\"${REPO}\"}" \
      --schedule "every monday 09:00"
    ```

    You can create multiple Cloud Scheduler instances against the same Cloud
    Pub/Sub topic with different payloads to clean multiple GCR repositories.

1. _(Optional)_ Run the scheduled job now:

    ```text
    gcloud beta scheduler jobs run gcrclean-myimage \
      --project ${PROJECT_ID}
    ```

    Note: for initial job deployments, you must wait a few minutes before
    invoking.


## Payload &amp; Parameters

The payload is expected to be JSON with the following fields:

- `repo` - Full name of the repository to clean, in the format
  `gcr.io/project/repo`. This field is required.

- `grace` - Relative duration in which to ignore references. This value is
  specified as a time duration value like "5s" or "3h". If set, refs newer than
  the duration will not be deleted. If unspecified, the default is no grace
  period (all untagged image refs are deleted).


## FAQ

**How do I clean up multiple Google Container Registry repos at once?**
<br>
To clean multiple repos, create a Cloud Scheduler job for each repo, altering
the payload to use the correct repo.

**Why are you using Cloud Pub/Sub?**
<br>
At the time of this writing, Cloud Scheduler does not send authentication
information to Cloud Run. As such, it is not possible for Cloud Scheduler to
invoke a private Cloud Run function. This is a known issue and, when resolved,
can be updated to drop Cloud Pub/Sub from the process. The server is already
configurable to do this, since it serves both a `/pubsub` and `/http` endpoint.


## License

This library is licensed under Apache 2.0. Full license text is available in
[LICENSE](https://github.com/sethvargo/gcr-cleaner/tree/master/LICENSE).

[cloud-build]: https://cloud.google.com/build/
[cloud-pubsub]: https://cloud.google.com/pubsub/
[cloud-run]: https://cloud.google.com/run/
[cloud-scheduler]: https://cloud.google.com/scheduler/
[cloud-shell]: https://cloud.google.com/shell
[cloud-sdk]: https://cloud.google.com/sdk
[gcrgc.sh]: https://gist.github.com/ahmetb/7ce6d741bd5baa194a3fac6b1fec8bb7
[gcr-cleaner-godoc]: https://godoc.org/github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner
