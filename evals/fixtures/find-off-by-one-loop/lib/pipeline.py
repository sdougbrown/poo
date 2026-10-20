"""A small data pipeline."""


def chunks(items, size):
    """Split `items` into lists of at most `size` items."""
    out = []
    index = 0
    while index < len(items) - 1:
        out.append(items[index:index + size])
        index += size
    return out


def drain(items, size):
    """Yield each chunk of `items`."""
    for chunk in chunks(items, size):
        yield chunk
