import unittest

from pathkit import join


class JoinTest(unittest.TestCase):
    def test_two_parts(self):
        self.assertEqual(join("a", "b"), "a/b")

    def test_skips_empty_parts(self):
        self.assertEqual(join("a", "", "b"), "a/b")

    def test_strips_trailing_slash(self):
        self.assertEqual(join("a/", "b"), "a/b")

    def test_strips_leading_slash(self):
        self.assertEqual(join("/a", "b"), "/a/b")


if __name__ == "__main__":
    unittest.main()
