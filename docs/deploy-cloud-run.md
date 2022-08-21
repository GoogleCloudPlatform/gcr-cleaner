# Deploy GCR Cleaner to Cloud Run

This document describes how to deploy GCR Cleaner to Cloud Run [Cloud
Run][cloud-run] and invoke it via [Cloud Scheduler][cloud-scheduler]. There is
also a community-supported [Terraform module for gcr-cleaner][tf-module].


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

1. Grant the service account access to delete references. See
   [Permissions](../README.md#permissions) for more information.

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

[cloud-run]: https://cloud.google.com/run/
[cloud-scheduler]: https://cloud.google.com/scheduler/
[cloud-sdk]: https://cloud.google.com/sdk
[cloud-shell]: https://cloud.google.com/shell
[tf-module]: https://registry.terraform.io/modules/mirakl/gcr-cleaner/google/latest#usage
