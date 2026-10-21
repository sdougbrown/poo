import unittest

from collection import sorted_copy


class SortedCopyTest(unittest.TestCase):
    def test_sorts(self):
        self.assertEqual(sorted_copy([3, 1, 2]), [1, 2, 3])

    def test_does_not_change_the_input(self):
        items = [3, 1, 2]
        sorted_copy(items)
        self.assertEqual(items, [3, 1, 2])

    def test_sorts_by_key(self):
        items = ["bbb", "a", "cc"]
        self.assertEqual(sorted_copy(items, key=len), ["a", "cc", "bbb"])


if __name__ == "__main__":
    unittest.main()
