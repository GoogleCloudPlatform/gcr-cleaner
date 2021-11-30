# GCR Cleaner

GCR Cleaner deletes stale images in Google Cloud [Container
Registry][container-registry] or Google Cloud [Artifact
Registry][artifact-registry]. This can help reduce costs and keep your container
images list in order.

GCR Cleaner is designed to be deployed as a [Cloud Run][cloud-run] service and
invoked periodically via [Cloud Scheduler][cloud-scheduler].

```text
+-------------------+    +-------------+    +-------+
|  Cloud Scheduler  | -> |  Cloud Run  | -> |  GCR  |
+-------------------+    +-------------+    +-------+
```

**This is not an official Google product.**


## Setup

1. Install the [Cloud SDK][cloud-sdk] for your operating system. Alternatively,
   you can run these commands from [Cloud Shell][cloud-shell], which has the SDK
   and other popular tools pre-installed.

1. Export your project ID as an environment variable. The rest of this setup
   assumes this environment variable is set.

   ```sh
   export PROJECT_ID="my-project"
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
    gcloud iam service-accounts create "gcr-cleaner" \
      --project "${PROJECT_ID}" \
      --display-name "gcr-cleaner"
    ```

1. Deploy the `gcr-cleaner` container on Cloud Run running as the service
   account just created:

    ```sh
    gcloud --quiet run deploy "gcr-cleaner" \
      --async \
      --project ${PROJECT_ID} \
      --platform "managed" \
      --service-account "gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com" \
      --image "us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner" \
      --region "us-central1" \
      --timeout "60s"
    ```

1. Grant the service account access to delete references in Google Container
   Registry (which stores container image laters in a Cloud Storage bucket):

    ```sh
    gsutil acl ch -u gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com:W gs://artifacts.${PROJECT_ID}.appspot.com
    ```

    To cleanup refs in _other_ GCP projects, replace `PROJECT_ID` with the
    target project ID. For example, if the Cloud Run service was running in
    "project-a" and you wanted to grant it permission to cleanup refs in
    "gcr.io/project-b/image", you would need to grant the Cloud Run service
    account in project-a permission on `artifacts.projects-b.appspot.com`.

    To clean up Container Registry images hosted in specific regions, update the bucket name to include the region:

    ```text
    gs://eu.artifacts.${PROJECT_ID}.appspot.com
    ```

    If you plan on using the `recursive` functionality, you must also grant the
    service account "Browser" permissions:

    ```sh
    gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member "serviceAccount:gcr-cleaner@${PROJECT_ID}.iam.gserviceaccount.com" \
      --role "roles/browser"
    ```

1. Create a service account with permission to invoke the Cloud Run service:

    ```sh
    gcloud iam service-accounts create "gcr-cleaner-invoker" \
      --project "${PROJECT_ID}" \
      --display-name "gcr-cleaner-invoker"
    ```

    ```sh
    gcloud run services add-iam-policy-binding "gcr-cleaner" \
      --project "${PROJECT_ID}" \
      --platform "managed" \
      --region "us-central1" \
      --member "serviceAccount:gcr-cleaner-invoker@${PROJECT_ID}.iam.gserviceaccount.com" \
      --role "roles/run.invoker"
    ```

1. Create a Cloud Scheduler HTTP job to invoke the function every week:

    ```sh
    gcloud app create \
      --project "${PROJECT_ID}" \
      --region "us-central" \
      --quiet
    ```

    ```sh
    # Replace this with the full name of the repository for which you
    # want to cleanup old references, for example:
    export REPO="gcr.io/${PROJECT_ID}/my-image"
    export REPO="us-docker-pkg.dev/${PROJECT_ID}/my-repo/my-image"
    ```

    ```sh
    # Capture the URL of the Cloud Run service:
    export SERVICE_URL=$(gcloud run services describe gcr-cleaner --project "${PROJECT_ID}" --platform "managed" --region "us-central1" --format 'value(status.url)')
    ```

    ```sh
    gcloud scheduler jobs create http "gcrclean-myimage" \
      --project ${PROJECT_ID} \
      --description "Cleanup ${REPO}" \
      --uri "${SERVICE_URL}/http" \
      --message-body "{\"repos\":[\"${REPO}\"]}" \
      --oidc-service-account-email "gcr-cleaner-invoker@${PROJECT_ID}.iam.gserviceaccount.com" \
      --schedule "0 8 * * 2" \
      --time-zone="US/Eastern"
    ```

    You can create specify multiple repositories in the list to clean more than
    one repository.

1. _(Optional)_ Run the scheduled job now:

    ```sh
    gcloud scheduler jobs run "gcrclean-myimage" \
      --project "${PROJECT_ID}"
    ```

    Note: for initial job deployments, you must wait a few minutes before
    invoking.


## Payload &amp; Parameters

The payload is expected to be JSON with the following fields:

- `repos` - List of the full names of the repositories to clean (e.g.
  `["gcr.io/project/repo"]`. This field is required.

- `grace` - Relative duration in which to ignore references. This value is
  specified as a time duration value like "5s" or "3h". If set, refs newer than
  the duration will not be deleted. If unspecified, the default is no grace
  period (all untagged image refs are deleted).

- `keep` - If an integer is provided, it will always keep that minimum number
  of images. Note that it will not consider images inside the `grace` duration.

- `tag_filter_any` - If specified, any image with at **least one tag** that
  matches this given regular expression will be deleted. The image will be
  deleted even if it has other tags that do not match the given regular
  expression. The regular expressions are parsed according to the [Go regexp
  package][go-re].

- `tag_filter_all` - If specified, any image where **all tags** match this given
  regular expression will be deleted. The image will not be delete if it has
  other tags that do not match the given regular expression. The regular
  expressions are parsed according to the [Go regexp package][go-re].

- `dry_run` - If set to true, will not delete anything and outputs what would
  have been deleted.

- `recursive` - If set to true, will recursively search all child repositories.

    **NOTE!** On Container Registry, you must grant additional permissions to
    the service account in order to query the registry. The most minimal
    permissions are `roles/browser`.

    **WARNING!** If you have access to many Container Registry repos, this will
    be very slow! This is because the Docker v2 API does not support server-side
    filtering, meaning GCR Cleaner must download a manifest of all repositories
    to which you have access and then do client-side filtering.

- `tag_filter` (_Deprecated_) - This option is deprecated and only exists to
  maintain backwards compatibility with some existing broken behavior. You
  should not use it. If specified, any image where **the first tag** matches
  this given regular expression will be deleted. The image will not be deleted
  if other tags match the regular expression. The regular expressions are parsed
  according to the [Go regexp package][go-re].

- `allow_tagged` (_Deprecated_) - This option is deprecated and has no effect.
  By default, GCR Cleaner will not delete tagged images. To delete tagged
  images, specify `tag_filter_any` or `tag_filter_all`. Specifying either of
  these will enable deletion by tag.


## Running locally

In addition to the server, you can also run GCR Cleaner locally for one-off tasks using `cmd/gcr-cleaner-cli`:

```text
docker run -it us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli
```

## I just want the container!

You can build the container yourself using the included Dockerfile.
Alternatively, you can source a pre-built container from Artifact Registry or
Container Registry. All of the following URLs provide an equivalent image:

```text
gcr.io/gcr-cleaner/gcr-cleaner
asia-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
europe-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
```

## What about using Terraform!

:package: You can deploy the stack using the community-supported Terraform module [gcr-cleaner](https://registry.terraform.io/modules/mirakl/gcr-cleaner/google/latest#usage):

## FAQ

**Does it work with Cloud Pub/Sub?**
<br>
Yes! Just change the endpoint from `/http` to `/pubsub`!

**What was your inspiration?**
<br>
GCR Cleaner is largely inspired by [ahmetb](https://twitter.com/ahmetb)'s
[gcrgc.sh][gcrgc.sh], but it is written in Go and is designed to be run as a
service.

## License

This library is licensed under Apache 2.0. Full license text is available in
[LICENSE](https://github.com/sethvargo/gcr-cleaner/tree/master/LICENSE).

[artifact-registry]: https://cloud.google.com/artifact-registry
[cloud-build]: https://cloud.google.com/build/
[cloud-pubsub]: https://cloud.google.com/pubsub/
[cloud-run]: https://cloud.google.com/run/
[cloud-scheduler]: https://cloud.google.com/scheduler/
[cloud-sdk]: https://cloud.google.com/sdk
[cloud-shell]: https://cloud.google.com/shell
[container-registry]: https://cloud.google.com/container-registry
[gcr-cleaner-godoc]: https://godoc.org/github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner
[gcrgc.sh]: https://gist.github.com/ahmetb/7ce6d741bd5baa194a3fac6b1fec8bb7
[go-re]: https://golang.org/pkg/regexp/syntax/
