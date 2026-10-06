load helpers

REPO="$BATS_TEST_DIRNAME/.."

renovate() {
  python3 -c '
import json, re, sys
config = json.load(open(sys.argv[1]))
def pattern(js):
    return re.compile(js.strip("/").replace("\\/", "/"))
def capture(match_string):
    return re.compile(re.sub(r"\(\?<", "(?P<", match_string))
exec(sys.argv[2])
' "$REPO/renovate.json" "$1"
}

@test "the engine's renovate config is valid json and keeps action digests pinned" {
  renovate 'assert "helpers:pinGitHubActionDigests" in config["extends"]'
}

@test "renovate updates the template's image pins" {
  renovate '
patterns = [pattern(p) for p in config["docker-compose"]["managerFilePatterns"]]
for name in ("config-template/images.yml", "config-template/images.monitoring.yml"):
    assert any(p.search(name) for p in patterns), name
assert not any(p.search("docker-compose.yml") for p in patterns)'
}

@test "the template's image versioning rules are the same in the engine's renovate config" {
  python3 -c '
import json, sys
engine = json.load(open(sys.argv[1]))["packageRules"]
template = json.load(open(sys.argv[2]))["packageRules"]
assert all(rule in engine for rule in template)' "$REPO/renovate.json" "$REPO/config-template/renovate.json"
}

@test "every version renovate tracks by regex is found in its file" {
  renovate '
import glob
expected = {
    "restic/restic": "internal/restic/release.env",
    "koalaman/shellcheck": ".github/workflows/test.yml",
}
found = {}
for manager in config["customManagers"]:
    files = [f for f in set(expected.values()) if any(pattern(p).search(f) for p in manager["managerFilePatterns"])]
    for file in files:
        text = open(sys.argv[1].rsplit("/", 1)[0] + "/" + file).read()
        for match_string in manager["matchStrings"]:
            match = capture(match_string).search(text)
            if match:
                found[manager["depNameTemplate"]] = (file, match.group("currentValue"))
for dep, file in expected.items():
    assert dep in found and found[dep][0] == file, (dep, found)
assert found["restic/restic"][1][0].isdigit()
'
}

@test "renovate tidies the go modules it updates" {
  renovate 'assert "gomodTidy" in config.get("postUpdateOptions", []), config.get("postUpdateOptions")'
}

@test "renovate bumps the restic mse pins too" {
  renovate '
found = [m for m in config["customManagers"] if m["depNameTemplate"] == "restic/restic"]
assert len(found) == 1
patterns = found[0]["managerFilePatterns"]
assert any(pattern(p).search("internal/restic/release.env") for p in patterns), patterns
assert not any(pattern(p).search("scripts/tool-versions.env") for p in patterns), patterns
'
}

@test "renovate no longer tracks sops, age or the Python test tools" {
  renovate '
names = [m["depNameTemplate"] for m in config["customManagers"]]
assert "getsops/sops" not in names and "FiloSottile/age" not in names, names
assert not any("tool-versions" in f for r in config["packageRules"] for f in r.get("matchFileNames", [])), config["packageRules"]
'
}
