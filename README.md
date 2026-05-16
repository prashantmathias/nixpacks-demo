# Kaniko + Nixpacks Deno Builder Prototype

This project is a Go-based prototype demonstrating how to programmatically build Docker images from inside a Kubernetes cluster **without relying on a Docker daemon**, and subsequently deploying that newly built image.

It utilizes the Kubernetes Go SDK (`client-go`) to orchestrate a hybrid build pipeline: it uses **Nixpacks** to analyze a Deno TypeScript application and automatically generate a `Dockerfile`, and then uses **Kaniko** to daemonlessly build that generated Dockerfile.

## Architecture & Execution Flow

When you run the orchestrator, the following steps occur entirely within your local Kubernetes cluster:

1.  **Source Code Provisioning**: The orchestrator (`main.go`) embeds `target-app/main.ts` and `target-app/deno.json`. It creates a Kubernetes `ConfigMap` containing these raw source files.
2.  **ConfigMap Dereferencing**: Because Kubernetes ConfigMap mounts are presented as symbolic links (which context executors often have trouble navigating), the orchestrator injects an `initContainer` (alpine). This container copies the files out of the ConfigMap mount using `cp -L` (to dereference the symlinks) into an `EmptyDir` volume (`/workspace`).
3.  **Nixpacks Plan Generation**: A second `initContainer` (ubuntu) installs the `nixpacks` CLI and runs `nixpacks build . -o .` directly against the codebase in `/workspace`. Nixpacks intelligently analyzes the TypeScript code, detects Deno, and generates a `.nixpacks/Dockerfile`.
4.  **Kaniko Daemonless Build**: A Pod running the `gcr.io/kaniko-project/executor` image is spawned. It mounts the `EmptyDir` containing our generated Dockerfile and executes a daemonless build using `--dockerfile=/workspace/.nixpacks/Dockerfile`.
5.  **Ephemeral Registry Push**: To make testing seamless locally without needing Docker Hub credentials, Kaniko automatically pushes the freshly compiled image to `ttl.sh`. This is an anonymous, ephemeral container registry (images expire after 1 hour).
6.  **Watch & Deploy**: The orchestrator monitors Kubernetes pod events to wait for the Kaniko build to succeed. Once successful, it programmatically generates a `Deployment` and a `Service` (NodePort) pointing to the newly pushed `ttl.sh` image.

## Project Structure

- `main.go` - The orchestrator CLI. Uses `client-go` to create the ConfigMap, the hybrid Kaniko/Nixpacks Pod, Deployment, and Service.
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
Starting Kaniko prototype run cf02455a
Image destination will be: ttl.sh/kaniko-proto-cf02455a:1h
-> Creating ConfigMap with source files...
-> Launching Kaniko Pod...
-> Waiting for Kaniko build to complete (this may take a minute)...
-> Kaniko build SUCCEEDED!
-> Creating Deployment...
-> Creating Service...
=====================================================
Prototype Execution Complete!
Deployment created: kaniko-app-cf02455a
You can access the app at: http://localhost:32413
=====================================================
```

Once complete, you can hit the dynamically built application:

```bash
curl http://localhost:32413
# Output: Hello from dynamically built Deno image via Nixpacks + Kaniko!
```

## Cleanup

The orchestrator cleans up the `ConfigMap` and Kaniko build `Pod` automatically (unless it crashes). To clean up the final deployment, run the commands printed at the end of the execution, which will look like:

```bash
kubectl delete deployment kaniko-app-<run-id>
kubectl delete service kaniko-app-<run-id>-svc
```
