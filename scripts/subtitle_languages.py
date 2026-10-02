import re
import sys

import yaml


def flow_list(codes):
    return "[" + ", ".join(codes) + "]"


def section_end(lines, start):
    return next((i for i in range(start + 1, len(lines)) if lines[i].strip() and not lines[i][0].isspace()), len(lines))


def is_list_item_of(line, key_indent):
    indent = len(line) - len(line.lstrip())
    return bool(line.strip()) and (indent > len(key_indent) or (indent == len(key_indent) and line.lstrip().startswith("- ")))


def with_languages(text, codes):
    lines = text.split("\n")
    if "bazarr:" not in [line.split("#")[0].rstrip() for line in lines]:
        body = text.rstrip("\n")
        return (body + "\n" if body else "") + f"bazarr:\n  languages: {flow_list(codes)}\n"
    start = [line.split("#")[0].rstrip() for line in lines].index("bazarr:")
    end = section_end(lines, start)
    for i in range(start + 1, end):
        found = re.match(r"^(\s+)languages:(\s*)([^#]*?)(\s*#.*)?$", lines[i])
        if not found:
            continue
        indent, comment = found.group(1), found.group(4) or ""
        block_end = i + 1
        if not found.group(3):
            while block_end < end and is_list_item_of(lines[block_end], indent):
                block_end += 1
        lines[i:block_end] = [f"{indent}languages: {flow_list(codes)}{comment}"]
        return "\n".join(lines)
    child = next((line[: len(line) - len(line.lstrip())] for line in lines[start + 1:end] if line.strip()), "  ")
    lines.insert(start + 1, f"{child}languages: {flow_list(codes)}")
    return "\n".join(lines)


def main(path, wanted):
    codes = [code.strip() for code in wanted.split(",") if code.strip()]
    text = open(path).read()
    current = ((yaml.safe_load(text) or {}).get("bazarr") or {}).get("languages")
    if current == codes:
        return
    updated = with_languages(text, codes)
    if ((yaml.safe_load(updated) or {}).get("bazarr") or {}).get("languages") != codes:
        sys.exit(f"could not set the subtitle languages in {path}; set bazarr.languages to {flow_list(codes)} by hand")
    with open(path, "w") as out:
        out.write(updated)


if __name__ == "__main__":
    main(*sys.argv[1:3])
