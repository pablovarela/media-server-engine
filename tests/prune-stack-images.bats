load helpers

setup() {
  setup_stubs
  cat > "$CONFIG_DIR/images.yml" <<'YML'
services:
  sonarr:
    image: lscr.io/linuxserver/sonarr:4.0.21-ls327@sha256:newsonarr
YML
  cat > "$CONFIG_DIR/images.monitoring.yml" <<'YML'
services:
  grafana:
    image: grafana/grafana:13.2.3@sha256:newgrafana
YML
  make_stub docker '
if [ "$1 $2" = "image ls" ]; then
  printf "%s\n" "grafana/grafana latest sha256:oldgrafana" "postgres 16 sha256:unrelated"
  case " $* " in
    *" -a "*) printf "%s\n" \
      "lscr.io/linuxserver/sonarr <none> sha256:newsonarr" \
      "lscr.io/linuxserver/sonarr <none> sha256:oldsonarr" \
      "grafana/grafana <none> sha256:newgrafana" ;;
  esac
fi'
}

teardown() {
  teardown_stubs
}

@test "prune removes outdated media stack images" {
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  [ "$status" -eq 0 ]
  grep -q "docker image rm lscr.io/linuxserver/sonarr@sha256:oldsonarr" "$STUB_LOG"
}

@test "prune removes outdated monitoring stack images" {
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  grep -q "docker image rm grafana/grafana:latest" "$STUB_LOG"
}

@test "prune keeps every pinned image, running or not" {
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  ! grep -q "newsonarr" <(grep "image rm" "$STUB_LOG") || false
  ! grep -q "newgrafana" <(grep "image rm" "$STUB_LOG") || false
}

@test "prune leaves images from other projects alone" {
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  ! grep -q "postgres" <(grep "image rm" "$STUB_LOG") || false
}

@test "prune carries on when an image is still in use" {
  make_stub docker '
if [ "$1 $2" = "image ls" ]; then
  printf "%s\n" "lscr.io/linuxserver/sonarr <none> sha256:oldsonarr" "grafana/grafana latest sha256:oldgrafana"
fi
if [ "$1 $2" = "image rm" ] && [ "$3" = "lscr.io/linuxserver/sonarr@sha256:oldsonarr" ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  [ "$status" -eq 0 ]
  grep -q "docker image rm grafana/grafana:latest" "$STUB_LOG"
}

@test "prune keeps images pinned only in the override" {
  printf 'services:\n  extra:\n    image: example/extra:1@sha256:extra1\n' > "$CONFIG_DIR/compose.override.yml"
  make_stub docker '
if [ "$1 $2" = "image ls" ]; then
  printf "%s\\n" "example/extra <none> sha256:extra1" "example/extra <none> sha256:extra0"
fi'
  run "$BATS_TEST_DIRNAME/../scripts/prune-stack-images.sh"
  grep -q "docker image rm example/extra@sha256:extra0" "$STUB_LOG"
  ! grep -q "extra1" <(grep "image rm" "$STUB_LOG") || false
}
