import unittest

from textkit import count_words


class CountWordsTest(unittest.TestCase):
    def test_plain(self):
        self.assertEqual(count_words("one two three"), 3)

    def test_empty(self):
        self.assertEqual(count_words(""), 0)

    def test_whitespace_runs_count_once(self):
        self.assertEqual(count_words("one   two\tthree\n"), 3)

    def test_capitalised_word_is_one_word(self):
        self.assertEqual(count_words("Hello world"), 2)

    def test_all_capitalised(self):
        self.assertEqual(count_words("HELLO"), 1)


if __name__ == "__main__":
    unittest.main()
