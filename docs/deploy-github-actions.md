# Deploy GCR Cleaner to GitHub Actions

This document describes how to invoke GCR Cleaner via cron in GitHub Actions.
[GitHub Actions][github-actions] is a CI/CD solution provided by GitHub, and it
is free for open source projects. There are multiple triggers for GitHub Actions
worklflows, including [cron scheduler][github-actions-cron].

The easiest way to use GCR Cleaner with GitHub Actions is via the pre-built
`gcr-cleaner-cli` container and a scheduled cron GitHub Actions workflow.

```yaml
# .github/workflows/gcr-cleaner.yml
name: 'gcr-cleaner'

on:
  schedule:
    - cron: '0 0 */1 * *' # runs daily
  workflow_dispatch: # allows for manual invocation

jobs:
  gcr-cleaner:
    runs-on: 'ubuntu-latest'
    steps:
      # configure based on your registry
      - uses: 'docker/login-action@v2'
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_PASSWORD }}

      # customize based on the gcr-cleaner flags
      - uses: 'docker://us-docker.pkg.dev/gcr-cleaner/gcr-cleaner/gcr-cleaner-cli'
        with:
          args: >-
            -repo=us-docker.pkg.dev/my-repo/my-thing
            -repo=ghcr.io/myuser/my-image
            -grace=48h
```


## Authentication

In order to actually delete images in the upstream registry, you will need to
authenticate to the upstream registry. The easiest way to authenticate is to use
the [docker/login-action][docker-auth]. The [documentation][docker-auth] has
detailed configuration instructions for other types of repositories.

You must grant the authenticated principal permission to read and delete
resources in the registry. See [Permissions](../README.md#permissions) for more
information.


[github-actions]: https://github.com/features/actions
[github-actions-cron]: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule
[docker-auth]: https://github.com/docker/login-action
