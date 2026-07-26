#!/usr/bin/env bash
# Dev loop, as independently selectable steps.
#
#   ./dev.sh                    stop + build + start + ios  (the full loop)
#   ./dev.sh ios                only rebuild/reinstall the app; server untouched
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
#   ios     build the app, install + launch it on a simulator
#
# Options:
#   -y, --yes      don't ask before killing live sessions
#   --sim NAME     simulator to target        (default: iPhone 17)
#   --tailnet      publish over the tailnet   (default: --lan, see below)
#   --xcodegen     regenerate Agenton.xcodeproj from project.yml first
#
# --lan is the default because it is the only deterministic simulator target: in
# tailnet mode the web server binds the machine's tailnet IP and does NOT listen
# on localhost, so the sim has nothing to reach unless Tailscale is up.
#
# `ios` deliberately does NOT run xcodegen on its own. Regenerating rewrites the
# generated .pbxproj (gitignored — never committed) and pulls every file under
# ios/Agenton into the target — including work-in-progress that may not compile.
# Pass --xcodegen when you mean it (i.e. after editing project.yml).
set -euo pipefail
cd "$(dirname "$0")"

ASSUME_YES=0 MODE=lan SIM="iPhone 17" RUN_XCODEGEN=0
DO_STOP=0 DO_BUILD=0 DO_START=0 DO_IOS=0 ANY_STEP=0

while [ $# -gt 0 ]; do
  case "$1" in
    stop)       DO_STOP=1;  ANY_STEP=1 ;;
    build)      DO_BUILD=1; ANY_STEP=1 ;;
    start)      DO_START=1; ANY_STEP=1 ;;
    ios)        DO_IOS=1;   ANY_STEP=1 ;;
    -y|--yes)   ASSUME_YES=1 ;;
    --sim)      SIM="${2:?--sim needs a device name}"; shift ;;
    --tailnet)  MODE=tailnet ;;
    --lan)      MODE=lan ;;
    --xcodegen) RUN_XCODEGEN=1 ;;
    -h|--help)  sed -n '2,29p' "$0" | sed 's/^# \{0,1\}//;s/^#$//'; exit 0 ;;
    *)          echo "dev.sh: unknown argument '$1' (try -h)" >&2; exit 2 ;;
  esac
  shift
done
if [ "$ANY_STEP" -eq 0 ]; then DO_STOP=1 DO_BUILD=1 DO_START=1 DO_IOS=1; fi

# A daemon launched from inside an agenton session would steal the socket, and
# `agenton up` refuses anyway — fail early with a clearer message.
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
    ./agenton up -no-tui --lan
  else
    ./agenton up -no-tui
  fi
fi

if [ "$DO_START" -eq 0 ] && [ "$DO_IOS" -eq 0 ]; then say "Done"; exit 0; fi

# Where the phone/sim should point. lan always serves localhost; tailnet records
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

# Reachability is fatal only if we were asked to start the server; when running
# `ios` alone the server may legitimately be down or owned by someone else.
if curl -fsS -m 3 "http://$HOST:$PORT/healthz" >/dev/null 2>&1; then
  echo "web healthy at http://$HOST:$PORT"
elif [ "$DO_START" -eq 1 ]; then
  echo "dev.sh: /healthz not answering at $HOST:$PORT after start" >&2; exit 1
else
  echo "note: nothing answering at http://$HOST:$PORT — the app will retry once a server is up"
fi

[ "$DO_IOS" -eq 1 ] || { say "Done"; exit 0; }

# --- ios ---------------------------------------------------------------------
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
PROJ=ios/Agenton.xcodeproj

if [ "$RUN_XCODEGEN" -eq 1 ]; then
  say "Regenerating Agenton.xcodeproj from project.yml"
  command -v xcodegen >/dev/null || { echo "dev.sh: xcodegen not installed" >&2; exit 1; }
  (cd ios && xcodegen generate)
elif [ ios/project.yml -nt "$PROJ/project.pbxproj" ]; then
  echo "note: project.yml is newer than the .xcodeproj — building the committed"
  echo "      project as-is. Pass --xcodegen to regenerate (rewrites the tracked"
  echo "      .pbxproj and adds every file under ios/Agenton to the target)."
fi

say "Building the iOS app"
# xcodebuild dumps the whole swift-frontend invocation on failure; keep the
# diagnostics and drop the noise, then fail on xcodebuild's status (not awk's).
set +e
ios/Tools/build-sim.sh 2>&1 | grep -aE "error:|warning:|BUILD (SUCCEEDED|FAILED)|simulator build OK"
IOS_RC=${PIPESTATUS[0]}
set -e
[ "$IOS_RC" -eq 0 ] || { echo "dev.sh: iOS build failed (see errors above)" >&2; exit 1; }

APP_DIR="$(xcodebuild -project "$PROJ" -scheme Agenton \
  -destination 'generic/platform=iOS Simulator' -showBuildSettings CODE_SIGNING_ALLOWED=NO \
  2>/dev/null | awk -F' = ' '/ BUILT_PRODUCTS_DIR /{print $2; exit}')"
APP="$APP_DIR/Agenton.app"
[ -d "$APP" ] || { echo "dev.sh: no app at $APP" >&2; exit 1; }
BID="$(plutil -extract CFBundleIdentifier raw -o - "$APP/Info.plist")"

say "Booting $SIM"
xcrun simctl bootstatus "$SIM" -b >/dev/null   # boots if shut down, waits for ready
open -a Simulator                              # bring the window up to look at

say "Installing $BID"
xcrun simctl terminate "$SIM" "$BID" 2>/dev/null || true
xcrun simctl install "$SIM" "$APP"

# Point the app at this server. MUST go through `defaults` on the device: the app
# container's plist is cached by cfprefsd, so writing that file directly is
# silently ignored on the next launch.
xcrun simctl spawn "$SIM" defaults write "$BID" host -string "$HOST"
xcrun simctl spawn "$SIM" defaults write "$BID" port -int "$PORT"

say "Launching"
xcrun simctl launch "$SIM" "$BID"

cat <<EOF

Ready.
  server   http://$HOST:$PORT   (mode: $MODE)
  sim      $SIM — $BID, pointed at $HOST:$PORT
  logs     ~/.local/state/agenton/{daemon,web}.log

The daemon outlives this script. Next time: ./dev.sh ios for an app-only pass.
EOF
