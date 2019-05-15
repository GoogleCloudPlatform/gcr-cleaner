docker-push:
	@gcloud builds submit \
	  --project=gcr-cleaner \
		--tag=gcr.io/gcr-cleaner/gcr-cleaner
.PHONY: docker-push
