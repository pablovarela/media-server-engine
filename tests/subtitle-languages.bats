load helpers

setup() {
  setup_stubs
  APPS="$CONFIG_DIR/apps.yml"
}

teardown() {
  teardown_stubs
}

set_languages() {
  python3 "$BATS_TEST_DIRNAME/../scripts/subtitle_languages.py" "$APPS" "$1"
}

languages_read_back() {
  python3 -c 'import sys, yaml; print(yaml.safe_load(open(sys.argv[1]))["bazarr"]["languages"])' "$APPS"
}

@test "a code yaml would read as something other than text is quoted, so it stays a language" {
  printf 'bazarr:\n  languages: [en]\n' > "$APPS"
  run set_languages "en, no"
  [ "$status" -eq 0 ]
  [ "$(languages_read_back)" = "['en', 'no']" ]
  grep -qx "  languages: \[en, 'no'\]" "$APPS"
}

@test "a bazarr section written on one line is left alone, with a hint" {
  printf 'bazarr: {languages: [en], providers: [x]}\n' > "$APPS"
  cp "$APPS" "$BATS_TEST_TMPDIR/before.yml"
  run set_languages "en, es"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "set bazarr.languages to \[en, es\] by hand"
  cmp -s "$BATS_TEST_TMPDIR/before.yml" "$APPS"
}

@test "a languages list spread over several lines is left alone, with a hint" {
  printf 'bazarr:\n  languages: [en,\n    fr]\n' > "$APPS"
  cp "$APPS" "$BATS_TEST_TMPDIR/before.yml"
  run set_languages "es"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "set bazarr.languages to \[es\] by hand"
  ! echo "$output" | grep -q "Traceback" || false
  cmp -s "$BATS_TEST_TMPDIR/before.yml" "$APPS"
}
