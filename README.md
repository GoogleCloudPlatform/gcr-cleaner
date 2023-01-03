# GCR Cleaner

GCR Cleaner deletes old container images in [Docker Hub][docker-hub], [Container Registry][container-registry], [Artifact Registry][artifact-registry], or any Docker v2 registries. This can help reduce storage costs, especially in CI/CD environments where images are created and pushed frequently.

There are multiple deployment options for GCR Cleaner. Click on your preferred
deployment option for a detailed guide:

- [Scheduled GitHub Action workflow](docs/deploy-github-actions.md)
- [Deployed to Cloud Run](docs/deploy-cloud-run.md)

For one-off tasks, you can also run GCR Cleaner locally:

```text
docker run -it us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli
```

If you want gcr-cleaner to inherit the authentication from your local gcloud installation, you must mount the gcloud directory into the container:

```text
docker run -v "${HOME}/.config/gcloud:/.config/gcloud" -it us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli
```

**This is not an official Google product.**


## Container images

Pre-built container images are available at the following locations. We do not
offer versioned container images.

```text
asia-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
europe-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner
```


## Server Payload &amp; parameters

**⚠️ This section is for the _server_ payload. If you are using the CLI tool,
run `gcr-cleaner -h` to see the list of flags and their descriptions.**

The payload is expected to be JSON with the following fields:

- `repos` - List of the full names of the repositories to clean (e.g.
  `["us-docker.pkg.dev/project/my/repo", "gcr.io/my/repo"]`. This field is
  required.

- `grace` - Relative duration in which to ignore references. This value is
  specified as a time duration value like "5s" or "3h". If set, refs newer than
  the duration will not be deleted. If unspecified, the default is no grace
  period (all untagged image refs are deleted).

- `keep` - If an integer is provided, it will always keep that minimum number of
  images. Note that it will not consider images inside the `grace` duration. GCR
  Cleaner attempts to keep the most recently created images, but there are some
  caveats. Some community tooling sets container creation time to a date back in
  1980, which breaks the default sorting algorithm. As such, GCR Cleaner uses
  the following sorting algorithm for container images:

    - If either of the containers were created before Docker even existed, it
      sorts by the date the container was uploaded to the registry.

    - If two containers were created at the same timestamp, it sorts by the date
      the container was uploaded to the registry.

    - In all other situations, it sorts by the timestamp the container was
      created.

  This algorithm exists to preserve ordering for containers that are moved
  between registries.

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

    **NOTE!** On Artifact Registry, you must grant additional permissions to the service account in order to query the registry. The most minimal permissions are `roles/storage.objectViewer`.

    **WARNING!** If the authenticated principal has access to many Container
    Registry or Artifact Registry repos, this will be very slow! This is because
    the Docker v2 API does not support server-side filtering, meaning GCR
    Cleaner must download a manifest of all repositories to which you have
    access and then do client-side filtering. The most granular filter is at the
    _host_ layer, meaning GCR Cleaner will perform a list operation on `gcr.io`
    (for Container Registry) or `us-docker.pkg.dev` (for Artifact Registry),
    parse the response and do client-side filtering to match against the
    provided patterns, then start deleting. To re-iterate, this operation is
    **not segmented by project** - if the authenticated principal has access to
    10,000 repos, the client will need to filter through 10,000 repos. The
    easiest way to mitigate this is to practice the Principle of Least Privilege
    and create a dedicated service account that has granular permissions on a
    subset of repositories.


## Permissions

This section lists the minimum required permissions depending on the target
cleanup system.

#### Artifact Registry

The service account running GCR cleaner must have
`roles/artifactregistry.repoAdmin` or greater on the Artifact Registry
repositories. Here is an example for setting that permissions via `gcloud`:

```sh
gcloud artifacts repositories add-iam-policy-binding "my-repo" \
  --project "my-project" \
  --location "us" \
  --member "serviceAccount:gcr-cleaner@my-project.iam.gserviceaccount.com" \
  --role "roles/artifactregistry.repoAdmin"
```

#### Container Registry

Container Registry stores images in Google Cloud Storage, so the service account
running GCR Cleaner must have read and write permissions on the underlying Cloud
Storage bucket. Here is an example for setting that permission via `gsutil`:

```sh
gsutil acl ch -u gcr-cleaner@my-project.iam.gserviceaccount.com:W gs://artifacts.my-project.appspot.com
```

To clean up Container Registry images hosted in specific regions, update the
bucket name to include the region:

```text
gs://eu.artifacts.my-project.appspot.com
```

If you plan on using the `recursive` functionality, you must also grant the
service account "Browser" permissions:

```sh
gcloud projects add-iam-policy-binding "my-project" \
  --member "serviceAccount:gcr-cleaner@my-project.iam.gserviceaccount.com" \
  --role "roles/browser"
```


## Debugging

By default, GCR Cleaner only emits user-level logging at the "info" level. More logs are available at the "debug" level. To configure the log level, set the `GCRCLEANER_LOG` environment variable to the desired log value:

```sh
export GCRCLEANER_LOG=debug
```

In debug mode, GCR Cleaner will print **a lot** of information, including its
entire decision process for candidate deletion. If you open an issue, please
include these debug logs as they are very helpful in finding and fixing any
bugs.


## Concurrency

By default, GCR Cleaner will attempt to perform operations in parallel. You can
customize the concurrency with `-concurrency` on the CLI or by setting the
environment variable `GCRCLEANER_CONCURRENCY` on the server. It defaults to 20.


[artifact-registry]: https://cloud.google.com/artifact-registry
[container-registry]: https://cloud.google.com/container-registry
[docker-hub]: https://hub.docker.com
[go-re]: https://golang.org/pkg/regexp/syntax/
