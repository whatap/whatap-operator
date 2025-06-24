# Docker Manifest List Creation Guide

This guide explains how to combine existing architecture-specific Docker images into a single multi-architecture image using Docker manifest lists.

> **Note:** If you're using the `build.sh` script to build your images, you don't need to run `create-manifest.sh` separately. The `build.sh` script already includes the manifest creation functionality.

## Problem

You have separate Docker images for different architectures:
- `public.ecr.aws/whatap/whatap-operator:1.9.78-amd64` (for AMD64 architecture)
- `public.ecr.aws/whatap/whatap-operator:1.9.78-arm64` (for ARM64 architecture)

And you want to combine them into a single multi-architecture image:
- `public.ecr.aws/whatap/whatap-operator:1.9.78`

## Solution

The `create-manifest.sh` script creates a Docker manifest list that references both architecture-specific images, allowing Docker to automatically pull the correct image for the client's architecture. This script is useful when you have existing architecture-specific images that were built separately and you want to combine them into a multi-architecture image.

### Prerequisites

1. Docker CLI with manifest command support (Docker 18.03 or later)
2. Access to the registry where the images are stored

### Usage

```bash
./create-manifest.sh <VERSION> [<REGISTRY>]
```

#### Arguments
- `<VERSION>`: The version of the images to combine (e.g., 1.9.78)
- `<REGISTRY>`: (Optional) The registry where the images are stored (default: public.ecr.aws/whatap)

#### Example

To combine the amd64 and arm64 images for version 1.9.78:

```bash
./create-manifest.sh 1.9.78
```

This will:
1. Create a manifest list referencing both architecture-specific images
2. Push the manifest list to the registry

### How It Works

The script uses the Docker manifest command to create a manifest list that references both architecture-specific images:

```bash
docker manifest create ${IMG} ${IMG}-amd64 ${IMG}-arm64
docker manifest push ${IMG}
```

When a client pulls the combined image, Docker automatically selects the appropriate architecture-specific image based on the client's platform.

### Verification

To verify that the manifest list was created correctly, you can use the Docker manifest inspect command:

```bash
docker manifest inspect public.ecr.aws/whatap/whatap-operator:1.9.78
```

This should show information about both architectures included in the manifest list.
