# Build the manager binary
FROM golang:1.16 as builder

WORKDIR /workspace

COPY ./ .

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o grakola-controller cmd/grakola-controller/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/grakola-controller .
USER 65532:65532

ENTRYPOINT ["/grakola-controller"]
