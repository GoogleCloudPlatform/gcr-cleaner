FROM --platform=$BUILDPLATFORM alpine AS builder

RUN apk --no-cache --update add ca-certificates && \
  update-ca-certificates


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY gcr-cleaner-cli /bin/gcr-cleaner-cli

ENTRYPOINT ["/bin/gcr-cleaner-cli"]
