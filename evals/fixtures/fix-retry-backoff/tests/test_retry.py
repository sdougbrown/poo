import unittest

from netclient import RetryPolicy


class RetryPolicyTest(unittest.TestCase):
    def test_first_attempt_is_retried(self):
        self.assertTrue(RetryPolicy(attempts=3).should_retry(1))

    def test_second_attempt_is_retried(self):
        self.assertTrue(RetryPolicy(attempts=3).should_retry(2))

    def test_third_attempt_is_not_retried(self):
        self.assertFalse(RetryPolicy(attempts=3).should_retry(3))

    def test_backoff_grows(self):
        policy = RetryPolicy(attempts=3, delay_s=1.0)
        self.assertEqual(policy.backoff_s(1), 1.0)
        self.assertEqual(policy.backoff_s(2), 2.0)


if __name__ == "__main__":
    unittest.main()
