load helpers

setup() {
  setup_stubs
  ROOT=$(mktemp -d)
  mkdir -p "$ROOT/engine/installation" "$ROOT/config"
  cp "$BATS_TEST_DIRNAME/../installation/Makefile" "$ROOT/Makefile"
  cat > "$ROOT/engine/Makefile" <<'MAKE'
help:
	@echo "engine help"
update:
	@echo "engine update from $(CURDIR)"
configure:
	@echo "engine configure ROTATE=$(ROTATE)"
MAKE
}

teardown() {
  rm -rf "$ROOT"
  teardown_stubs
}

@test "a target run at the installation root runs in the engine" {
  run make -s -C "$ROOT" update
  [ "$status" -eq 0 ]
  [ "$output" = "engine update from $(cd "$ROOT/engine" && pwd -P)" ] || [ "$output" = "engine update from $ROOT/engine" ]
}

@test "variables given at the installation root reach the engine" {
  run make -s -C "$ROOT" configure ROTATE=sonarr
  [ "$output" = "engine configure ROTATE=sonarr" ]
}

@test "make alone at the installation root shows the engine's help" {
  run make -s -C "$ROOT"
  [ "$output" = "engine help" ]
}

@test "an unknown target fails as it does in the engine" {
  run make -s -C "$ROOT" no-such-target
  [ "$status" -ne 0 ]
}

lib_in() {
  ENGINE_DIR="$1" CONFIG_DIR="$2" bash -c "source '$BATS_TEST_DIRNAME/../scripts/lib.sh' && write_installation_makefile"
}

@test "the installation's makefile is written next to an engine inside an installation" {
  rm "$ROOT/Makefile"
  cp -R "$BATS_TEST_DIRNAME/../installation/." "$ROOT/engine/installation/"
  lib_in "$ROOT/engine" "$ROOT/config"
  cmp -s "$ROOT/Makefile" "$BATS_TEST_DIRNAME/../installation/Makefile"
}

@test "an outdated installation makefile is replaced" {
  cp -R "$BATS_TEST_DIRNAME/../installation/." "$ROOT/engine/installation/"
  echo "old" > "$ROOT/Makefile"
  lib_in "$ROOT/engine" "$ROOT/config"
  cmp -s "$ROOT/Makefile" "$BATS_TEST_DIRNAME/../installation/Makefile"
}

@test "an engine that is not inside an installation writes no makefile beside it" {
  checkout=$(mktemp -d)
  mkdir -p "$checkout/media-server-engine/installation"
  cp -R "$BATS_TEST_DIRNAME/../installation/." "$checkout/media-server-engine/installation/"
  lib_in "$checkout/media-server-engine" "$ROOT/config"
  [ ! -e "$checkout/Makefile" ]
  rm -rf "$checkout"
}

@test "the engine's help lists the everyday targets first" {
  run make -s -C "$BATS_TEST_DIRNAME/.." help
  first=$(echo "$output" | sed -n '2,10p' | awk '{print $1}' | sed 's/\x1b\[[0-9;]*m//g' | tr '\n' ' ')
  [ "$first" = "update configure urls logins backup-now verify-backup-now media-start media-stop media-status " ]
}
