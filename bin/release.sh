#!/bin/sh

bin/build-dist.py || exit 1
gh create release --repo diogo464/go-ipfs $(git describe --tags) dist/*

