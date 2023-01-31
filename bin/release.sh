#!/bin/sh

bin/build-dist.py || exit 1
gh release create --repo diogo464/go-ipfs $(git describe --tags) dist/*

