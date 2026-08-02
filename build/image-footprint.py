#!/usr/bin/env python3
import argparse
import json
import os
import tarfile
import tempfile
from pathlib import Path


def infer_platform(path: Path) -> str:
    name = path.name
    if "amd64" in name:
        return "linux/amd64"
    if "arm64" in name:
        return "linux/arm64"
    if "armv5" in name:
        return "linux/arm/v5"
    return "multiarch"


def read_member_bytes(archive: tarfile.TarFile, member_name: str) -> bytes:
    member = archive.getmember(member_name)
    extracted = archive.extractfile(member)
    if extracted is None:
        raise RuntimeError(f"missing member content: {member_name}")
    return extracted.read()


def extract_single_platform_rootfs(archive: tarfile.TarFile) -> tuple[int, dict[str, int], list[dict[str, int | str]]]:
    index = json.loads(read_member_bytes(archive, "index.json"))
    manifests = index.get("manifests", [])
    if len(manifests) != 1:
        raise RuntimeError(f"expected single-platform OCI index, got {len(manifests)} manifests")
    manifest_digest = manifests[0]["digest"].split(":", 1)[1]
    manifest = json.loads(read_member_bytes(archive, f"blobs/sha256/{manifest_digest}"))
    layers = manifest.get("layers", [])
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        for layer in layers:
            layer_digest = layer["digest"].split(":", 1)[1]
            layer_member = archive.getmember(f"blobs/sha256/{layer_digest}")
            extracted = archive.extractfile(layer_member)
            if extracted is None:
                raise RuntimeError(f"missing layer blob: {layer_digest}")
            with tarfile.open(fileobj=extracted, mode="r:*") as layer_tar:
                layer_tar.extractall(root)
        files: list[dict[str, int | str]] = []
        binary_sizes: dict[str, int] = {}
        total = 0
        for current, _, filenames in os.walk(root):
            for filename in filenames:
                path = Path(current) / filename
                if path.is_symlink():
                    continue
                try:
                    size = path.stat().st_size
                except FileNotFoundError:
                    continue
                rel = str(path.relative_to(root))
                files.append({"path": rel, "bytes": size})
                total += size
                if rel in {
                    "gateway",
                    "usr/local/bin/amneziawg-go",
                    "usr/local/bin/awg",
                    "usr/local/bin/awg3-parser-validate",
                    "usr/sbin/ip",
                    "usr/sbin/sysctl",
                }:
                    binary_sizes[rel] = size
        files.sort(key=lambda item: (-int(item["bytes"]), str(item["path"])))
        return total, binary_sizes, files[:10]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("archives", nargs="+")
    args = parser.parse_args()

    report = {"images": []}
    for archive_path_str in args.archives:
        archive_path = Path(archive_path_str)
        image = {
            "target": infer_platform(archive_path),
            "archive": str(archive_path),
            "compressed_oci_bytes": archive_path.stat().st_size,
            "unpacked_rootfs_bytes": None,
            "binaries": {},
            "largest_contributors": [],
        }
        with tarfile.open(archive_path, "r:*") as archive:
            if image["target"] == "multiarch":
                image["largest_contributors"] = []
            else:
                total, binaries, largest = extract_single_platform_rootfs(archive)
                image["unpacked_rootfs_bytes"] = total
                image["binaries"] = binaries
                image["largest_contributors"] = largest
        report["images"].append(image)

    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
