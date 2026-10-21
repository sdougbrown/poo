import unittest

from shop.api.api_0 import API_TIMEOUT_SECONDS
from shop.jobs.jobs_0 import JOBS_TIMEOUT_SECONDS


class TimeoutTest(unittest.TestCase):
    def test_api_timeout_is_thirty(self):
        self.assertEqual(API_TIMEOUT_SECONDS, 30)

    def test_jobs_timeout_is_thirty(self):
        self.assertEqual(JOBS_TIMEOUT_SECONDS, 30)


if __name__ == "__main__":
    unittest.main()
