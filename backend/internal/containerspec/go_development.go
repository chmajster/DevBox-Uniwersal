package containerspec

import "encoding/json"

const goWatchScript = `#!/bin/sh
set -eu
child=''
stop() { if [ -n "$child" ]; then kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true; child=''; fi; }
trap 'stop; exit 0' TERM INT
trap stop EXIT
fingerprint() { find . -type d \( -name .git -o -name vendor \) -prune -o -type f \( -name '*.go' -o -name go.mod -o -name go.sum \) -exec sha256sum '{}' \; | sort | sha256sum; }
build() {
  if [ -f go.mod ]; then go mod download; else export GO111MODULE=off; fi
  go build -o /tmp/devbox-app "$@"
}
previous=''
while :; do
  current=$(fingerprint)
  if [ "$current" != "$previous" ]; then
    if build "$@"; then
      stop
      /tmp/devbox-app &
      child=$!
    else
      echo 'Go rebuild failed; retaining the previous process until sources are corrected' >&2
    fi
    previous=$current
  fi
  sleep 1 &
  wait $! || true
done
`

func goDevelopmentWatcher() string {
	// Use a JSON RUN with the script as an argv value to preserve quotes safely.
	command, _ := json.Marshal([]string{"sh", "-c", "printf '%s' \"$1\" > /usr/local/bin/devbox-go-watch && chmod 0755 /usr/local/bin/devbox-go-watch", "devbox", goWatchScript})
	return "RUN " + string(command) + "\n"
}
