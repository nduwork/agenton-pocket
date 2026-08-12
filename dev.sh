#!/usr/bin/env bash
# Dev loop, as independently selectable steps.
#
#   ./dev.sh                    stop + build + start  (the full loop)
#   ./dev.sh build              only rebuild ./agenton; nothing restarted
#   ./dev.sh stop start         restart the server, no rebuild
#   ./dev.sh build start        rebuild, then start if not already up
#                               (`start` is idempotent — it reuses a live server,
#                               so pair it with `stop` to actually cycle one)
#
# Steps (run in this order no matter how you list them):
#   stop    kill the running daemon + web. NOT implied by anything else.
#   build   go build -o agenton ./cmd/agenton
#   start   launch daemon + web detached
#
# Options:
#   -y, --yes      don't ask before killing live sessions
#   --tailnet      publish over the tailnet   (default: --lan)
#   --lan          serve localhost only       (default)
set -euo pipefail
cd "$(dirname "$0")"

ASSUME_YES=0 MODE=lan
DO_STOP=0 DO_BUILD=0 DO_START=0 ANY_STEP=0

while [ $# -gt 0 ]; do
  case "$1" in
    stop)       DO_STOP=1;  ANY_STEP=1 ;;
    build)      DO_BUILD=1; ANY_STEP=1 ;;
    start)      DO_START=1; ANY_STEP=1 ;;
    -y|--yes)   ASSUME_YES=1 ;;
    --tailnet)  MODE=tailnet ;;
    --lan)      MODE=lan ;;
    -h|--help)  sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//;s/^#$//'; exit 0 ;;
    *)          echo "dev.sh: unknown argument '$1' (try -h)" >&2; exit 2 ;;
  esac
  shift
done
if [ "$ANY_STEP" -eq 0 ]; then DO_STOP=1 DO_BUILD=1 DO_START=1; fi

# A daemon launched from inside an agenton session would steal the socket, and
# `agenton vpn`/`lan` refuse anyway — fail early with a clearer message.
if [ "$DO_START" -eq 1 ] && [ -n "${AGENTON_SESSION:-}" ]; then
  echo "dev.sh: you're inside an agenton session; run this from a normal shell." >&2
  exit 1
fi

say() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

# --- stop --------------------------------------------------------------------
if [ "$DO_STOP" -eq 1 ]; then
  say "Stopping daemon + web"
  DPID="$(pgrep -f 'agenton daemon' | head -1 || true)"
  if [ -n "$DPID" ]; then
    # Sessions are children of the daemon; they die with it, scrollback included.
    LIVE="$(ps -axo pid=,ppid=,command= | awk -v d="$DPID" \
      '$2==d && $0 !~ /defunct/ {print "  " $1 "  " substr($0, index($0,$3))}' || true)"
    if [ -n "$LIVE" ]; then
      echo "These live sessions will be killed (scrollback is lost):"
      echo "$LIVE"
      if [ "$ASSUME_YES" -eq 0 ]; then
        read -r -p "Continue? [y/N] " ans
        case "$ans" in [yY]*) ;; *) echo "aborted."; exit 1 ;; esac
      fi
    fi
  fi
  pkill -f 'agenton daemon' 2>/dev/null || true
  pkill -f 'agenton web' 2>/dev/null || true
  sleep 1
  if pgrep -f 'agenton (daemon|web)' >/dev/null 2>&1; then
    echo "dev.sh: something survived SIGTERM" >&2; exit 1
  fi
  echo "stopped."
fi

# --- build -------------------------------------------------------------------
if [ "$DO_BUILD" -eq 1 ]; then
  say "Building agenton"
  go build -o agenton ./cmd/agenton
  echo "$(wc -c < agenton | tr -d ' ') bytes"
fi

# --- start -------------------------------------------------------------------
if [ "$DO_START" -eq 1 ]; then
  say "Starting daemon + web (mode: $MODE)"
  if [ "$MODE" = lan ]; then
    ./agenton lan -no-tui
  else
    ./agenton vpn -no-tui
  fi

  # Confirm the web endpoint answers. lan always serves localhost; tailnet records
  # the published endpoint in tailnet.json (absent = it fell back to localhost
  # because no Tailscale app was reachable).
  HOST=127.0.0.1 PORT=9787
  if [ "$MODE" = tailnet ]; then
    INFO="$HOME/.local/state/agenton/tailnet.json"
    if [ -f "$INFO" ]; then
      HOST="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["host"])' "$INFO")"
      PORT="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["port"])' "$INFO")"
    else
      echo "no tailnet.json — assuming the localhost fallback at $HOST:$PORT"
    fi
  fi
  if curl -fsS -m 3 "http://$HOST:$PORT/healthz" >/dev/null 2>&1; then
    echo "web healthy at http://$HOST:$PORT"
  else
    echo "dev.sh: /healthz not answering at $HOST:$PORT after start" >&2; exit 1
  fi
fi

say "Done"
