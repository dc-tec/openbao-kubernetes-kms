#!/bin/sh
# Inspect the executable independently of binfmt/QEMU, which can mask a wrong binary.
set -eu
case "$1" in
  amd64) machine='Advanced Micro Devices X86-64' ;;
  arm64) machine='AArch64' ;;
  *) echo 'expected amd64 or arm64' >&2; exit 2 ;;
esac
actual=$(LC_ALL=C readelf -h "$2" | sed -n 's/^ *Machine: *//p')
test "$actual" = "$machine" || { echo "wrong executable architecture: expected $machine; got $actual" >&2; exit 1; }
