import unittest

from confload import load_config, require


class LoadConfigTest(unittest.TestCase):
    def test_parses_pairs(self):
        config = load_config("a=1\nb=2\n")
        self.assertEqual(config, {"a": "1", "b": "2"})

    def test_ignores_comments(self):
        config = load_config("# note\na=1\n\n")
        self.assertEqual(config, {"a": "1"})

    def test_keeps_the_last_line_without_a_newline(self):
        config = load_config("a=1\nb=2")
        self.assertEqual(config, {"a": "1", "b": "2"})

    def test_require_raises_for_missing(self):
        config = load_config("a=1\n")
        with self.assertRaises(KeyError) as caught:
            require(config, "b")
        self.assertIn("b", str(caught.exception))

    def test_require_returns_value(self):
        config = load_config("a=1\n")
        self.assertEqual(require(config, "a"), "1")


if __name__ == "__main__":
    unittest.main()
