load helpers

setup() {
  setup_stubs
  LIB="$BATS_TEST_DIRNAME/../scripts/lib.sh"
}

teardown() {
  teardown_stubs
}

@test "ask keeps the default on an empty answer" {
  run bash -c "source '$LIB'; ask NAME 'Installation name' testinst <<< ''; echo \"=\$NAME\""
  [ "${lines[-1]}" = "=testinst" ]
}

@test "ask replaces the default with a typed answer" {
  run bash -c "source '$LIB'; ask NAME 'Installation name' testinst <<< 'otherinst'; echo \"=\$NAME\""
  [ "${lines[-1]}" = "=otherinst" ]
}

@test "ask shows the label and the default" {
  run bash -c "source '$LIB'; ask NAME 'Installation name' testinst <<< '' 2>&1"
  echo "$output" | grep -q "Installation name \[testinst\]"
}

@test "ask_secret never prints the current value" {
  run bash -c "source '$LIB'; ask_secret KEY 'B2 application key' 'K005supersecretvalueXYZ' <<< '' 2>&1; echo \"=\$KEY\""
  [ "${lines[-1]}" = "=K005supersecretvalueXYZ" ]
  ! echo "${lines[@]:0:${#lines[@]}-1}" | grep -q "supersecret"
  echo "$output" | grep -q "set, ends …XYZ"
}

@test "ask_secret says when nothing is set" {
  run bash -c "source '$LIB'; ask_secret KEY 'B2 application key' '' <<< 'new' 2>&1; echo \"=\$KEY\""
  echo "$output" | grep -q "not set"
  [ "${lines[-1]}" = "=new" ]
}

@test "ask_password generates a password on an empty answer when none is set" {
  run bash -c "source '$LIB'; ask_password PW 'Jellyfin admin password' '' <<< ''; echo \"=\$PW\""
  pw=${lines[-1]#=}
  [ "${#pw}" -ge 24 ]
}

@test "ask_password keeps the current password on an empty answer" {
  run bash -c "source '$LIB'; ask_password PW 'Jellyfin admin password' 'current-one' <<< ''; echo \"=\$PW\""
  [ "${lines[-1]}" = "=current-one" ]
}

@test "generate_secret makes different url-safe values" {
  run bash -c "source '$LIB'; a=\$(generate_secret 24); b=\$(generate_secret 24); echo \"\$a \$b\""
  set -- $output
  [ "$1" != "$2" ]
  [[ "$1" =~ ^[A-Za-z0-9_-]{32}$ ]]
}

@test "mask shows only the last three characters" {
  run bash -c "source '$LIB'; mask 'abcdefgh'"
  [ "$output" = "set, ends …fgh" ]
}

@test "the prompt helpers can set variables whose names they use internally" {
  run bash -c "source '$LIB'; for v in answer var label default current typed; do ask \$v 'Q' d <<< 'x'; ask_secret \$v 'Q' '' <<< 'y'; ask_password \$v 'Q' '' <<< 'z'; done; echo \"=\$answer\$var\$label\$default\$current\$typed\""
  [ "${lines[-1]}" = "=zzzzzz" ]
}
