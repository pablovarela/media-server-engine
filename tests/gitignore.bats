setup() {
  cd "$BATS_TEST_DIRNAME/.."
}

@test "go build output and coverage are ignored by git" {
  git check-ignore -q dist/mse
  git check-ignore -q coverage.out
}

@test "the engine's sources, compose files and installer are not ignored" {
  ! git check-ignore -q main.go || false
  ! git check-ignore -q cmd/root.go || false
  ! git check-ignore -q tools/go.mod || false
  ! git check-ignore -q docker-compose.yml || false
  ! git check-ignore -q install.sh || false
}

@test "decrypted secrets in a checkout of this repository are ignored by git" {
  git check-ignore -q .secrets/vpn.env
  git check-ignore -q .env
}
