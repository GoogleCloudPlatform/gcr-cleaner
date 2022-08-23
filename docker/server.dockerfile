FROM --platform=$BUILDPLATFORM alpine AS builder

RUN apk --no-cache --update add ca-certificates && \
  update-ca-certificates


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY gcr-cleaner /bin/gcr-cleaner

ENV PORT 8080

ENTRYPOINT ["/bin/gcr-cleaner"]
