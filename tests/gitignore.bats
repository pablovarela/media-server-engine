setup() {
  cd "$BATS_TEST_DIRNAME/.."
}

@test "decrypted secrets written next to the engine are ignored by git" {
  git check-ignore -q .secrets/vpn.env
  git check-ignore -q .env
}

@test "engine sources are not ignored" {
  ! git check-ignore -q scripts/update.sh || false
  ! git check-ignore -q docker-compose.yml || false
}

@test "python bytecode from the wiring is ignored by git" {
  git check-ignore -q scripts/wire/__pycache__/wirelib.cpython-314.pyc
}
