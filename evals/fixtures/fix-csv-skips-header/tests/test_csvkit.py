import unittest

from csvkit import parse_csv


class ParseCSVTest(unittest.TestCase):
    def test_skips_the_header(self):
        rows = parse_csv("name,age\nalice,30\nbob,41\n")
        self.assertEqual(rows, [["alice", "30"], ["bob", "41"]])

    def test_strips_blank_lines(self):
        rows = parse_csv("name\nalice\n\n")
        self.assertEqual(rows, [["alice"]])

    def test_empty_text(self):
        self.assertEqual(parse_csv(""), [])


if __name__ == "__main__":
    unittest.main()
