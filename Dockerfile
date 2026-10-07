FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o terraform-provider-nodeping .

# Pin the runtime base image instead of tracking :latest, so a rebuild cannot
# silently pick up a different distro release (Trivy DS-0001).
FROM alpine:3.24 AS runtime
RUN apk add --no-cache ca-certificates

# Run as an unprivileged user (Trivy DS-0002).
RUN addgroup -S -g 10001 provider \
 && adduser -S -u 10001 -G provider -h /app provider

WORKDIR /app
COPY --from=builder /app/terraform-provider-nodeping .

USER 10001:10001

# Terraform plugins speak gRPC over stdio and are started by Terraform core, so
# there is no endpoint to poll. Verifying that the binary is executable is the
# only meaningful liveness signal (Trivy DS-0026).
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/app/terraform-provider-nodeping", "--help"]

ENTRYPOINT ["./terraform-provider-nodeping"]

# The acceptance tests drive a real terraform binary. Taking it from the
# official image keeps the version pinned and avoids terraform-plugin-testing
# downloading a different one at run time. It is a named stage rather than
# `COPY --from=<image>` because Dependabot only reads `FROM` lines. Pin a full
# patch version: ci.yml reads it from here for setup-terraform.
FROM hashicorp/terraform:1.16.5 AS terraform

FROM golang:1.27-alpine AS test
# build-base supplies the C toolchain the race detector needs.
RUN apk add --no-cache git build-base
COPY --from=terraform /bin/terraform /usr/local/bin/terraform
ENV TF_ACC_TERRAFORM_PATH=/usr/local/bin/terraform
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-v", "./..."]
