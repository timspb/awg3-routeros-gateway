#!/usr/bin/env bash
set -euo pipefail

docker buildx bake smoke
