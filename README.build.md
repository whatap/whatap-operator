# Build Optimization Guide

This document explains the optimizations made to the build process to improve build speed.

## Overview of Changes

The build process has been optimized in several ways:

1. **Default to single architecture builds**: The build script now defaults to building for a single architecture (amd64) instead of all architectures, which significantly speeds up development builds.

2. **Improved caching**: The build process now uses Docker BuildKit's caching capabilities more effectively, including registry-based caching and Go build caching.

3. **Reuse buildx builder**: The buildx builder is now reused across builds instead of being created and removed for each build, reducing overhead.

4. **Optimized Dockerfile**: The Dockerfile has been optimized to improve layer caching and build speed.

5. **Parallel builds**: The Go build process now uses parallel compilation to speed up the build.

6. **Single-pass tagging**: The build process now tags both the version-specific and latest images in a single operation, avoiding duplicate builds.

## Usage

### Basic Usage

To build the operator for a single architecture (amd64):

```bash
./build.sh <VERSION>
```

For example:

```bash
./build.sh 1.7.15
```

### Building for a Specific Architecture

To build for a specific architecture:

```bash
./build.sh <VERSION> <ARCH>
```

Where `<ARCH>` can be:
- `amd64`: Build for AMD64 architecture only
- `arm64`: Build for ARM64 architecture only
- `all`: Build for all supported architectures (arm64, amd64, s390x, ppc64le)

For example:

```bash
./build.sh 1.7.15 arm64
```

### Building Without Cache

To build without using the cache:

```bash
./build.sh <VERSION> <ARCH> --no-cache
```

For example:

```bash
./build.sh 1.7.15 amd64 --no-cache
```

## Technical Details

### Makefile Changes

The `docker-buildx` target in the Makefile has been updated to:

1. Reuse the buildx builder across builds
2. Use registry-based caching
3. Support additional build arguments via the `EXTRA_ARGS` parameter

### Dockerfile Changes

The Dockerfile has been optimized to:

1. Order COPY commands from least frequently changed to most frequently changed
2. Use BuildKit's cache mounts for Go build caching
3. Enable parallel compilation with the `-parallel=4` flag
4. Use trimpath to reduce binary size and improve reproducibility

### Build Script Changes

The build.sh script has been updated to:

1. Default to building for amd64 only
2. Support a --no-cache option
3. Build and tag both version-specific and latest images in a single operation