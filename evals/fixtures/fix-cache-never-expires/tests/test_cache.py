import unittest

from cachekit import Cache


class CacheTest(unittest.TestCase):
    def test_put_and_get(self):
        cache = Cache(capacity=2)
        cache.put("a", 1)
        self.assertEqual(cache.get("a"), 1)

    def test_missing_key_is_none(self):
        self.assertIsNone(Cache().get("nope"))

    def test_drops_the_least_recently_used(self):
        cache = Cache(capacity=2)
        cache.put("a", 1)
        cache.put("b", 2)
        cache.get("a")
        cache.put("c", 3)
        self.assertIsNone(cache.get("b"))
        self.assertEqual(cache.get("a"), 1)
        self.assertEqual(len(cache), 2)


if __name__ == "__main__":
    unittest.main()
