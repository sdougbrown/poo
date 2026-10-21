"""A tiny slicing helper."""


def clamp_slice(items, start, stop):
    """Return the items of `items` between `start` and `stop`.

    The bounds are inclusive on both ends, and the result is clamped to
    the length of the list.
    """
    if stop < start:
        return []
    stop = min(stop, len(items))
    return items[start:stop]
