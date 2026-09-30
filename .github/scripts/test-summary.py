import sys
import xml.etree.ElementTree as ElementTree

report, shellcheck_status = sys.argv[1], sys.argv[2]
cases = ElementTree.parse(report).getroot().iter("testcase")
passed, skipped, failed, seconds = 0, 0, [], 0.0
for case in cases:
    seconds += float(case.get("time") or 0)
    failure = case.find("failure")
    if failure is None:
        failure = case.find("error")
    if failure is not None:
        message = (failure.text or failure.get("message") or "").strip().splitlines()
        failed.append((case.get("classname", ""), case.get("name", ""), message[-6:]))
    elif case.find("skipped") is not None:
        skipped += 1
    else:
        passed += 1

total = passed + skipped + len(failed)
verdict = "passed" if not failed and shellcheck_status == "0" else "failed"
print(f"## Tests {verdict}")
print()
print("| Tests | Passed | Failed | Skipped | Time | shellcheck |")
print("|---|---|---|---|---|---|")
print(f"| {total} | {passed} | {len(failed)} | {skipped} | {seconds:.0f}s | {'clean' if shellcheck_status == '0' else 'findings'} |")
if failed:
    print()
    print("### Failures")
    for suite, name, message in failed:
        print()
        print(f"**{suite}**: {name}")
        print()
        print("```")
        print("\n".join(message))
        print("```")
