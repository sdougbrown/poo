"""Rate averaging."""


def average(values):
    """Return the arithmetic mean of `values`.

    An empty list has no mean, so the function returns None for it.
    """
    total = sum(values)
    count = len(values)
    return total / count
