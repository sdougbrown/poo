"""Path helpers."""


def join(*parts):
    """Join path parts with a single slash.

    A part that is empty does not add a slash of its own.
    """
    cleaned = [p for p in parts if p]
    return "/".join(cleaned)
