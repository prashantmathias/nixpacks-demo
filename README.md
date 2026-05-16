# BuildKit + Nixpacks Deno Builder Prototype

This project is a Go-based prototype demonstrating how to programmatically build Docker images from inside a Kubernetes cluster **without relying on a Docker daemon**, and subsequently deploying that newly built image.

It utilizes the Kubernetes Go SDK (`client-go`) to orchestrate a hybrid build pipeline: it uses **Nixpacks** to analyze a Deno TypeScript application and automatically generate a `Dockerfile`, and then uses **BuildKit** to daemonlessly build that generated Dockerfile.

## Architecture & Execution Flow

When you run the orchestrator, the following steps occur entirely within your local Kubernetes cluster:

1.  **Source Code Provisioning**: The orchestrator (`main.go`) embeds `target-app/main.ts` and `target-app/deno.json`. It creates a Kubernetes `ConfigMap` containing these raw source files.
2.  **BuildKit Sidecar**: An isolated `buildkitd` daemon runs as a privileged sidecar container in the Pod to process build requests.
3.  **Collapsed Client Layer**: The `builder` container runs the client-side steps without multi-container hops. It copies files to dereference ConfigMap symlinks, installs Nixpacks, generates the build plan via `nixpacks build . -o .`, and finally issues the `buildctl` command against the sidecar socket.
4.  **Ephemeral Registry Push**: To make testing seamless locally without needing Docker Hub credentials, BuildKit automatically pushes the freshly compiled image to `ttl.sh`. This is an anonymous, ephemeral container registry (images expire after 1 hour).
5.  **Watch & Deploy**: The orchestrator monitors Kubernetes pod events to wait for the BuildKit build to succeed. Once successful, it programmatically generates a `Deployment` and a `Service` (NodePort) pointing to the newly pushed `ttl.sh` image.

## Project Structure

- `main.go` - The orchestrator CLI. Uses `client-go` to create the ConfigMap, the hybrid BuildKit/Nixpacks Pod, Deployment, and Service.
- `target-app/`
  - `main.ts` - A simple Deno TypeScript HTTP server to be containerized.
  - `deno.json` - Configuration ensuring Nixpacks detects the application as a Deno runtime project.

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
Starting BuildKit prototype run cf02455a
Image destination will be: ttl.sh/buildkit-proto-cf02455a:1h
-> Creating ConfigMap with source files...
-> Launching BuildKit Pod...
-> Waiting for BuildKit build to complete (this may take a minute)...
-> BuildKit build SUCCEEDED!
-> Creating Deployment...
-> Creating Service...
=====================================================
Prototype Execution Complete!
Deployment created: buildkit-app-cf02455a
You can access the app at: http://localhost:32413
=====================================================
```

Once complete, you can hit the dynamically built application:

```bash
curl http://localhost:32413
# Output: Hello from dynamically built Deno image via Nixpacks + BuildKit!
```

## Cleanup

The orchestrator cleans up the `ConfigMap` and BuildKit build `Pod` automatically (unless it crashes). To clean up the final deployment, run the commands printed at the end of the execution, which will look like:

```bash
kubectl delete deployment buildkit-app-<run-id>
kubectl delete service buildkit-app-<run-id>-svc
```

## Production Readiness

While this prototype demonstrates a highly efficient, single-pod architecture with isolated daemons and native OCI metadata caching, **it is not completely ready for a strict production environment**. To adapt this setup for production use, consider the following required improvements:

1.  **Security (Rootless BuildKit)**: Currently, the `buildkitd` daemon runs in `Privileged: true` mode. Strict Kubernetes environments (using Pod Security Standards or OPA) block privileged containers. You must transition to rootless BuildKit (`moby/buildkit:master-rootless`) with appropriate AppArmor/Seccomp profiles.
2.  **Registry Authentication**: The orchestrator pushes to `ttl.sh` (a public, ephemeral registry). Production requires pushing to private registries (e.g., ECR, GCR, Docker Hub). You'll need to mount a Kubernetes Docker Registry Secret to the `builder` container at `~/.docker/config.json`.
3.  **BuildKit Caching**: The current design loses its cache when the Pod terminates. Implement caching by having `buildctl` export the cache to a registry (`--export-cache` and `--import-cache`) or by mounting a `PersistentVolumeClaim` (PVC) to `/var/lib/buildkit`.
4.  **Supply Chain Resiliency**: The `builder` dynamically installs `curl`, `nixpacks`, and `buildctl` at runtime. In production, use a custom, pre-baked Docker image that already contains these tools to improve reliability and reduce build times.
