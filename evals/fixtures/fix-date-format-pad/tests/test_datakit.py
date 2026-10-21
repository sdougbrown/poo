import unittest

from datakit import format_date


class FormatDateTest(unittest.TestCase):
    def test_pads(self):
        self.assertEqual(format_date(2026, 1, 2), "2026-01-02")

    def test_two_digits(self):
        self.assertEqual(format_date(2026, 12, 31), "2026-12-31")

    def test_year_is_padded(self):
        self.assertEqual(format_date(99, 1, 1), "0099-01-01")


if __name__ == "__main__":
    unittest.main()
