#!/bin/sh
export HTTP_PROXY="${HTTP_PROXY}"
export HTTPS_PROXY="${HTTPS_PROXY}"
export http_proxy="${http_proxy}"
export https_proxy="${https_proxy}"
export NO_PROXY="${NO_PROXY}"
exec python client.py server.py "$@"
