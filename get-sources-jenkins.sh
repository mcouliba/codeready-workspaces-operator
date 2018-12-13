#!/bin/bash -xe
# script to trigger rhpkg - no sources needed here


echo "[INFO] Trigger container-build in current branch: rhpkg --verbose container-build $1"
rhpkg --verbose container-build $1
wait
