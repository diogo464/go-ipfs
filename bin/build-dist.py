#!/usr/bin/python
import os
import subprocess
import itertools

# go tool dist list
GOOS = ["darwin", "freebsd", "linux", "openbsd", "windows"]
GOARCH = ["386", "amd64", "arm", "arm64"]
IGNORED = [("darwin", "386"), ("darwin", "arm")]

os.makedirs("dist", exist_ok=True)

tag = subprocess.run(
    ["git", "describe", "--tags"], capture_output=True, text=True, check=True
).stdout.strip()

for goos, goarch in itertools.product(GOOS, GOARCH):
    if (goos, goarch) in IGNORED:
        continue
    print(f"building {goos}/{goarch}")
    env = os.environ | {"GOOS": goos, "GOARCH": goarch}
    subprocess.run(["make", "build"], env=env).check_returncode()
    os.rename("cmd/ipfs/ipfs", f"dist/ipfs_{goos}_{goarch}-{tag}")
