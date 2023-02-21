# Deploy to Cloud Build

This document describes how to use GCR Cleaner in [Cloud Build][cloud-build]
with [Artifact Registry][artifact-registry]

1.  Grant a role `roles/artifactregistry.repoAdmin` to the [Cloud Build service
    account][cloud-build-service-account] because it need
    `artifactregistry.repositories.deleteArtifacts` permission.

1.  Export your project ID as an environment variable.

    ```sh
    export PROJECT_ID="my-project"
    ```

1.  Create a YAML file named cloudbuild.yaml which will always keep three images:

      ```yaml
      steps:
        - name: asia-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli:latest
          args:
            - -repo
            - "asia-docker.pkg.dev/my-project/my-repo/my-image"
            - -keep
            - "3"
            - -tag-filter-any
            - ".*"
      ```

1.  Manual trigger Cloud Build using [gcloud CLI][cloud-cli] to check it:

    ```sh
    gcloud builds submit \
      --project "${PROJECT_ID}" \
      --config cloudbuild.yaml .
    ```

[cloud-build]: https://cloud.google.com/build
[artifact-registry]: https://cloud.google.com/artifact-registry
[cloud-cli]: https://cloud.google.com/cli
[cloud-build-service-account]: https://cloud.google.com/build/docs/cloud-build-service-account
