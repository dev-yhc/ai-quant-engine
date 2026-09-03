#!/usr/bin/env python3
"""Check that adjacent top-level Go functions have a blank line between them."""

from __future__ import annotations

import argparse
from pathlib import Path
import sys


SKIPPED_DIRECTORIES = {".git", "node_modules", "testdata", "vendor"}


def is_identifier_start(character: str) -> bool:
    return character == "_" or character.isalpha()


def skip_whitespace_and_comments(source: str, index: int) -> int:
    while index < len(source):
        if source[index].isspace():
            index += 1
            continue
        if source.startswith("//", index):
            newline = source.find("\n", index + 2)
            index = len(source) if newline == -1 else newline + 1
            continue
        if source.startswith("/*", index):
            end = source.find("*/", index + 2)
            index = len(source) if end == -1 else end + 2
            continue
        return index
    return index


def is_function_declaration(source: str, index: int) -> bool:
    """Return whether ``func`` at index begins a top-level declaration."""
    index = skip_whitespace_and_comments(source, index + len("func"))
    if index >= len(source):
        return False
    if source[index] == "(":
        depth = 1
        index += 1
        while index < len(source) and depth:
            if source[index] == "(":
                depth += 1
            elif source[index] == ")":
                depth -= 1
            index += 1
        index = skip_whitespace_and_comments(source, index)
    return index < len(source) and is_identifier_start(source[index])


def preceding_identifier(source: str, index: int) -> str:
    index -= 1
    while index >= 0 and source[index].isspace():
        index -= 1
    end = index + 1
    while index >= 0 and (source[index].isalnum() or source[index] == "_"):
        index -= 1
    return source[index + 1 : end]


def documentation_start_line(lines: list[str], function_line: int) -> int:
    """Return the first adjacent doc-comment line, or the function line itself."""
    index = function_line - 2
    if index < 0:
        return function_line

    if lines[index].lstrip().startswith("//"):
        while index >= 0 and lines[index].lstrip().startswith("//"):
            index -= 1
        return index + 2

    if lines[index].rstrip().endswith("*/"):
        comment_end = index
        while index >= 0:
            if "/*" in lines[index]:
                return index + 1
            index -= 1
        return comment_end + 1

    return function_line


def check_file(path: Path) -> list[int]:
    """Return the lines where a required blank line is missing."""
    source = path.read_text(encoding="utf-8")
    lines = source.splitlines()
    violations: list[int] = []
    brace_depth = 0
    parenthesis_depth = 0
    bracket_depth = 0
    current_function_start: int | None = None
    pending_function_start: int | None = None
    previous_function_end: int | None = None
    index = 0
    line = 1

    while index < len(source):
        character = source[index]

        if source.startswith("//", index):
            newline = source.find("\n", index + 2)
            index = len(source) if newline == -1 else newline
            continue
        if source.startswith("/*", index):
            end = source.find("*/", index + 2)
            end = len(source) if end == -1 else end + 2
            line += source.count("\n", index, end)
            index = end
            continue
        if character == "`":
            end = source.find("`", index + 1)
            end = len(source) if end == -1 else end + 1
            line += source.count("\n", index, end)
            index = end
            continue
        if character in {'"', "'"}:
            quote = character
            index += 1
            while index < len(source):
                if source[index] == "\\":
                    index += 2
                    continue
                if source[index] == quote:
                    index += 1
                    break
                if source[index] == "\n":
                    line += 1
                index += 1
            continue
        if character == "\n":
            line += 1
            index += 1
            continue
        if source.startswith("func", index) and (
            index == 0 or not (source[index - 1].isalnum() or source[index - 1] == "_")
        ) and (index + 4 == len(source) or not (source[index + 4].isalnum() or source[index + 4] == "_")):
            if brace_depth == 0 and is_function_declaration(source, index):
                pending_function_start = documentation_start_line(lines, line)
            index += len("func")
            continue

        if character == "(":
            parenthesis_depth += 1
        elif character == ")":
            parenthesis_depth -= 1
        elif character == "[":
            bracket_depth += 1
        elif character == "]":
            bracket_depth -= 1
        elif character == "{":
            if (
                pending_function_start is not None
                and parenthesis_depth == 0
                and bracket_depth == 0
                and preceding_identifier(source, index) not in {"interface", "struct"}
            ):
                current_function_start = pending_function_start
                pending_function_start = None
            brace_depth += 1
        elif character == "}":
            brace_depth -= 1
            if current_function_start is not None and brace_depth == 0:
                if previous_function_end is not None and current_function_start - previous_function_end < 2:
                    violations.append(current_function_start)
                previous_function_end = line
                current_function_start = None

        index += 1

    return violations


def go_files(root: Path) -> list[Path]:
    return [
        path
        for path in root.rglob("*.go")
        if not any(part in SKIPPED_DIRECTORIES for part in path.relative_to(root).parts)
    ]


def add_blank_lines(path: Path, line_numbers: list[int]) -> None:
    lines = path.read_text(encoding="utf-8").splitlines(keepends=True)
    for line_number in reversed(line_numbers):
        lines.insert(line_number - 1, "\n")
    path.write_text("".join(lines), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "root",
        nargs="?",
        type=Path,
        default=Path(__file__).resolve().parents[2],
        help="repository directory to scan (default: repository root)",
    )
    parser.add_argument("--fix", action="store_true", help="insert the required blank lines")
    args = parser.parse_args()
    root = args.root.resolve()

    violations = 0
    for path in go_files(root):
        line_numbers = check_file(path)
        if args.fix and line_numbers:
            add_blank_lines(path, line_numbers)
        for line in line_numbers:
            if not args.fix:
                print(f"{path.relative_to(root)}:{line}: add a blank line between functions", file=sys.stderr)
            violations += 1
    if violations:
        if args.fix:
            print(f"function spacing: fixed {violations} violation(s)")
            return 0
        print(f"function spacing: found {violations} violation(s)", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
