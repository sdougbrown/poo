import unittest

from slacelib import clamp_slice


class ClampSliceTest(unittest.TestCase):
    def test_middle(self):
        self.assertEqual(clamp_slice([0, 1, 2, 3, 4], 1, 3), [1, 2, 3])

    def test_clamps_stop(self):
        self.assertEqual(clamp_slice([0, 1, 2], 1, 99), [1, 2])

    def test_empty_when_reversed(self):
        self.assertEqual(clamp_slice([0, 1, 2], 3, 1), [])

    def test_single(self):
        self.assertEqual(clamp_slice([0, 1, 2], 2, 2), [2])


if __name__ == "__main__":
    unittest.main()
