"""A small collection helper."""


def sorted_copy(items, key=None):
    """Return a sorted copy of `items`.

    The caller's list is never changed.
    """
    items.sort(key=key)
    return items
