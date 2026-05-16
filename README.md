# Kaniko Kubernetes Builder Prototype

This project is a Go-based prototype demonstrating how to programmatically build Docker images from inside a Kubernetes cluster without relying on a Docker daemon (Docker-in-Docker), and subsequently deploying that newly built image.

It utilizes the Kubernetes Go SDK (`client-go`) to orchestrate a [Kaniko](https://github.com/GoogleContainerTools/kaniko) build pod, wait for its completion, and dynamically roll out the application.

## Architecture & Execution Flow

When you run the orchestrator, the following steps occur entirely within your local Kubernetes cluster:

1.  **Source Code Provisioning**: The orchestrator (`main.go`) embeds the `target-app/main.go` and `target-app/Dockerfile`. It creates a Kubernetes `ConfigMap` containing these raw source files.
2.  **Kaniko Init Container**: Because Kubernetes ConfigMap mounts are presented as symbolic links (which the Kaniko `dir://` context executor has trouble navigating by default), the orchestrator injects a lightweight `initContainer`. This container copies the files out of the ConfigMap mount using `cp -L` (to dereference the symlinks) into an `EmptyDir` volume.
3.  **Kaniko Build**: A Pod running the `gcr.io/kaniko-project/executor` image is spawned. It mounts the `EmptyDir` containing our dereferenced source code to `/workspace` and builds the Go application natively inside the cluster.
4.  **Ephemeral Registry Push**: To make testing seamless locally without needing Docker Hub credentials or local registry configurations, Kaniko automatically pushes the freshly compiled image to `ttl.sh`. This is an anonymous, ephemeral container registry (images expire after 1 hour).
5.  **Watch & Deploy**: The orchestrator monitors Kubernetes pod events to wait for the Kaniko build to succeed. Once successful, it programmatically generates a `Deployment` and a `Service` (NodePort 30080) pointing to the newly pushed `ttl.sh` image.

## Project Structure

- `main.go` - The orchestrator CLI. Uses `client-go` to create the ConfigMap, Kaniko Pod, Deployment, and Service.
- `target-app/`
  - `main.go` - A simple "Hello World" Go HTTP server to be containerized.
  - `Dockerfile` - A multi-stage Dockerfile that compiles the target app into an Alpine container.

## Prerequisites

- Go 1.22+ installed
- A local Kubernetes cluster running (e.g., Docker Desktop, Minikube, or Kind)
- Your active Kubernetes context (`~/.kube/config`) must point to your local cluster.

## Running the Prototype

To test the prototype locally, simply run the orchestrator:

```bash
go run main.go
```

### Expected Output

```
Starting Kaniko prototype run 816470e0
Image destination will be: ttl.sh/kaniko-proto-816470e0:1h
-> Creating ConfigMap with source files...
-> Launching Kaniko Pod...
-> Waiting for Kaniko build to complete (this may take a minute)...
-> Kaniko build SUCCEEDED!
-> Creating Deployment...
-> Creating Service...
=====================================================
Prototype Execution Complete!
Deployment created: kaniko-app-816470e0
You can access the app at: http://localhost:30080
=====================================================
```

Once complete, you can hit the dynamically built application:

```bash
curl http://localhost:30080
# Output: Hello from dynamically built Kaniko image!
```

## Cleanup

The orchestrator cleans up the `ConfigMap` and Kaniko build `Pod` automatically (unless it crashes). To clean up the final deployment, run the commands printed at the end of the execution, which will look like:

```bash
kubectl delete deployment kaniko-app-<run-id>
kubectl delete service kaniko-app-<run-id>-svc
```
