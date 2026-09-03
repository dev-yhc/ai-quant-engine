from __future__ import annotations

from pathlib import Path
import tempfile
import unittest

from check_function_spacing import add_blank_lines, check_file


class CheckFunctionSpacingTests(unittest.TestCase):
    def check(self, source: str) -> list[int]:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "sample.go"
            path.write_text(source, encoding="utf-8")
            return check_file(path)

    def test_accepts_functions_separated_by_a_blank_line(self) -> None:
        self.assertEqual(
            self.check("package sample\n\nfunc first() {}\n\nfunc second() {}\n"),
            [],
        )

    def test_rejects_adjacent_functions(self) -> None:
        self.assertEqual(
            self.check("package sample\n\nfunc first() {}\nfunc second() {}\n"),
            [4],
        )

    def test_requires_a_blank_line_before_a_function_comment(self) -> None:
        self.assertEqual(
            self.check("package sample\n\nfunc first() {}\n// second does work.\nfunc second() {}\n"),
            [4],
        )

    def test_ignores_func_in_comments_and_strings(self) -> None:
        self.assertEqual(
            self.check(
                "package sample\n\nfunc first() {\n\t_ = \"func fake() {}\"\n\t// func fake() {}\n}\n\nfunc second() {}\n"
            ),
            [],
        )

    def test_handles_struct_type_in_a_function_signature(self) -> None:
        self.assertEqual(
            self.check(
                "package sample\n\nfunc first() map[string]struct{} {\n\treturn nil\n}\nfunc second() {}\n"
            ),
            [6],
        )

    def test_fix_inserts_a_blank_line(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "sample.go"
            path.write_text("package sample\n\nfunc first() {}\nfunc second() {}\n", encoding="utf-8")

            add_blank_lines(path, check_file(path))

            self.assertEqual(check_file(path), [])


if __name__ == "__main__":
    unittest.main()
