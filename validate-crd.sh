#!/usr/bin/env bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🔍 Validating CRD changes...${NC}"

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed or not in PATH.${NC}"
    echo -e "${YELLOW}Please install kubectl to validate CRDs: https://kubernetes.io/docs/tasks/tools/${NC}"
    exit 1
fi

# Generate CRD manifests
echo -e "${YELLOW}📝 Generating CRD manifests...${NC}"
if ! make manifests; then
    echo -e "${RED}❌ Failed to generate CRD manifests.${NC}"
    echo -e "${YELLOW}Please check the error messages above and fix any issues in your Go types.${NC}"
    exit 1
fi

# Find all CRD files
CRD_FILES=$(find config/crd/bases -name "*.yaml")
if [ -z "$CRD_FILES" ]; then
    echo -e "${RED}❌ No CRD files found in config/crd/bases directory.${NC}"
    exit 1
fi

# Validate each CRD file
echo -e "${YELLOW}✅ Validating CRDs using kubectl...${NC}"
for crd_file in $CRD_FILES; do
    echo -e "${YELLOW}Validating ${crd_file}...${NC}"
    if ! kubectl apply --dry-run=client -f "$crd_file"; then
        echo -e "${RED}❌ Validation failed for ${crd_file}.${NC}"
        echo -e "${YELLOW}Please check the error messages above and fix any issues in your Go types.${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ ${crd_file} is valid.${NC}"
done

echo -e "${GREEN}✅ All CRD validations completed successfully!${NC}"
echo -e "${GREEN}👉 Your CRD changes are valid and can be used with Kubernetes.${NC}"
echo -e "${YELLOW}Note: This validation only checks the syntax and schema of your CRDs.${NC}"
echo -e "${YELLOW}It does not guarantee that the CRDs will work correctly in a real cluster.${NC}"
