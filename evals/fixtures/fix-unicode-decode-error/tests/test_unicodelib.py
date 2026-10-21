import unittest

from unicodelib import character_count


class CharacterCountTest(unittest.TestCase):
    def test_ascii(self):
        self.assertEqual(character_count("abc"), 3)

    def test_emoji_is_one_character(self):
        self.assertEqual(character_count("\U0001F600"), 1)

    def test_mixed(self):
        self.assertEqual(character_count("a\U0001F600b"), 3)

    def test_empty(self):
        self.assertEqual(character_count(""), 0)


if __name__ == "__main__":
    unittest.main()
