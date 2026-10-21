import unittest

from rates import average


class AverageTest(unittest.TestCase):
    def test_two_values(self):
        self.assertEqual(average([10, 20]), 15)

    def test_empty_is_none(self):
        self.assertIsNone(average([]))

    def test_one_value(self):
        self.assertEqual(average([7]), 7)


if __name__ == "__main__":
    unittest.main()
