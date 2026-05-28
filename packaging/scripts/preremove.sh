#!/bin/sh
set -e

case "$1" in
	remove|0)
		if command -v systemctl >/dev/null 2>&1; then
			systemctl stop zjunet-go.service >/dev/null 2>&1 || true
			systemctl disable zjunet-go.service >/dev/null 2>&1 || true
		fi
		;;
esac

exit 0
