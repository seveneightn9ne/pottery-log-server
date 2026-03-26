set -euxo pipefail

ROOT=/tmp/pottery-log-exports
[ -d "$ROOT" ] || exit 0

echo "Size before cleanup:"
du -sh "$ROOT/"

find "$ROOT/*" -type f -mtime +1 -exec rm -f {} \;

echo "Size after cleanup:"
du -sh "$ROOT/"
