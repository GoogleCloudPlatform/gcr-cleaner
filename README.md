# GCR Cleaner

GCR Cleaner deletes untagged images in Google Container Registry. This can help
reduce costs and keep your container images list in order.

GCR Cleaner is designed to be deployed as a [Cloud Run][cloud-run] service and
invoked periodically via [Cloud Scheduler][cloud-scheduler] via [Cloud
Pub/Sub][cloud-pubsub].

```text
+-------------------+    +-------------+    +-------+
|  Cloud Scheduler  | -> |  Cloud Run  | -> |  GCR  |
+-------------------+    +-------------+    +-------+
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

   ```sh
   export PROJECT_ID=my-project
   ```

   Note this is your project _ID_, not the project _number_ or _name_.

1. Enable the Google APIs - this only needs to be done once per project:

    ```sh
    gcloud services enable --project "${PROJECT_ID}" \
      appengine.googleapis.com \
      cloudscheduler.googleapis.com \
      run.googleapis.com
    ```

    This operation can take a few minutes, especially for recently-created
    projects.

1. Create a service account which will be assigned to the Cloud Run service:

    ```sh
    gcloud iam service-accounts create gcr-cleaner \
      --project "${PROJECT_ID}" \
      --display-name "gcr-cleaner"
    ```

1. Deploy the `gcr-cleaner` container on Cloud Run running as the service
   account just created:

    ```sh
    gcloud alpha run deploy gcr-cleaner \
      --project ${PROJECT_ID} \
      --platform "managed" \
      --service-account "gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com" \
      --image "gcr.io/gcr-cleaner/gcr-cleaner" \
      --region "us-central1" \
      --timeout "60s" \
      --quiet
    ```

1. Grant the service account access to delete references in Google Container
   Registry:

    ```sh
    gsutil acl ch -u gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com:W gs://artifacts.${PROJECT_ID}.appspot.com
    ```

    To cleanup refs in _other_ GCP projects, replace `PROJECT_ID` with the
    target project ID. For example, if the Cloud Run service was running in
    "project-a" and you wanted to grant it permission to cleanup refs in
    "gcr.io/project-b/image", you would need to grant the Cloud Run service
    account in project-a permission on `artifacts.projects-b.appspot.com`.

1. Create a service account with permission to invoke the Cloud Run service:

    ```sh
    gcloud iam service-accounts create gcr-cleaner-invoker \
      --project "${PROJECT_ID}" \
      --display-name "gcr-cleaner-invoker"
    ```

    ```sh
    gcloud beta run services add-iam-policy-binding gcr-cleaner \
      --project "${PROJECT_ID}" \
      --platform "managed" \
      --region "us-central1" \
      --member "serviceAccount:gcr-cleaner-invoker@${PROJECT_ID}.iam.gserviceaccount.com" \
      --role "roles/run.invoker"
    ```

1. Create a Cloud Scheduler HTTP job to invoke the function every day:

    ```sh
    gcloud app create \
      --project "${PROJECT_ID}" \
      --region "us-central" \
      --quiet
    ```

    ```sh
    # Replace this with the full name of the repository for which you
    # want to cleanup old references.
    export REPO="gcr.io/my-project/my-image"
    ```

    ```sh
    # Capture the URL of the Cloud Run service:
    export SERVICE_URL=$(gcloud beta run services describe gcr-cleaner --project "${PROJECT_ID}" --platform "managed" --region "us-central1" --format 'value(status.url)')
    ```

    ```sh
    gcloud beta scheduler jobs create http gcrclean-myimage \
      --project ${PROJECT_ID} \
      --description "Cleanup ${REPO}" \
      --uri "${SERVICE_URL}/http" \
      --message-body "{\"repo\":\"${REPO}\"}" \
      --oidc-service-account-email "gcr-cleaner-invoker@${PROJECT_ID}.iam.gserviceaccount.com" \
      --schedule "every monday 09:00"
    ```

    You can create multiple Cloud Scheduler instances against the same Cloud Run
    service with different payloads to clean multiple GCR repositories.

1. _(Optional)_ Run the scheduled job now:

    ```sh
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

**Does it work with Cloud Pub/Sub?**
<br>
Yes! Just change the endpoint from `/http` to `/pubsub`!

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
