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

@test "the engine's help lists the everyday targets first" {
  run make -s -C "$BATS_TEST_DIRNAME/.." help
  first=$(echo "$output" | sed -n '2,11p' | awk '{print $1}' | sed 's/\x1b\[[0-9;]*m//g' | tr '\n' ' ')
  [ "$first" = "update configure version urls logins backup-now verify-backup-now media-start media-stop media-status " ]
}
