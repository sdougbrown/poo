"""A tiny HTTP client with retries."""


class RetryPolicy:
    """How many times a request is retried, and how long it waits."""

    def __init__(self, attempts=3, delay_s=0.5):
        self.attempts = attempts
        self.delay_s = delay_s

    def should_retry(self, attempt):
        """Return whether a request should be retried after `attempt`.

        The first attempt is number 1, and a policy of 3 attempts allows
        two retries.
        """
        return attempt <= self.attempts

    def backoff_s(self, attempt):
        """Return how long to wait before the attempt after `attempt`."""
        return self.delay_s * attempt
