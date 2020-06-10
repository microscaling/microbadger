FROM alpine:3.11

RUN apk add --no-cache ca-certificates

# Add binaries and Dockerfile
COPY microbadger notifier Dockerfile /

# Add OSI license list
COPY inspector/licenses.json /inspector/

# Create non privileged user and set permissions
RUN addgroup app && adduser -D -G app app && \
  chown -R app:app /microbadger  && \
  chown -R app:app /notifier && \
  chown -R app:app /inspector/licenses.json && \
  chmod +x /microbadger && chmod +x /notifier
USER app

# Metadata params
ARG VERSION
ARG VCS_URL
ARG VCS_REF
ARG BUILD_DATE

# Metadata
LABEL org.label-schema.vendor="Microscaling Systems" \
      org.label-schema.url="https://microbadger.com" \
      org.label-schema.vcs-type="git" \
      org.label-schema.vcs-url=$VCS_URL \
      org.label-schema.vcs-ref=$VCS_REF \
      org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.docker.dockerfile="/Dockerfile"

ENTRYPOINT ["/microbadger"]
