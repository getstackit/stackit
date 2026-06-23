#!/bin/sh
set -eu

if [ -n "${STACKIT_REPOS_ROOT:-}" ]; then
  case "$STACKIT_REPOS_ROOT" in
    /data/*) ;;
    *)
      echo "STACKIT_REPOS_ROOT must be under /data: $STACKIT_REPOS_ROOT" >&2
      exit 1
      ;;
  esac
  if [ "$STACKIT_REPOS_ROOT" = "/data/" ]; then
    echo "STACKIT_REPOS_ROOT must be a child of /data, not /data itself" >&2
    exit 1
  fi

  if [ -L "$STACKIT_REPOS_ROOT" ]; then
    echo "STACKIT_REPOS_ROOT must not be a symlink: $STACKIT_REPOS_ROOT" >&2
    exit 1
  fi

  mkdir -p "$STACKIT_REPOS_ROOT"
  if [ -L "$STACKIT_REPOS_ROOT" ]; then
    echo "STACKIT_REPOS_ROOT must not be a symlink: $STACKIT_REPOS_ROOT" >&2
    exit 1
  fi

  chown stackit:stackit "$STACKIT_REPOS_ROOT"
fi

exec su-exec stackit:stackit /usr/local/bin/stackit-server "$@"
