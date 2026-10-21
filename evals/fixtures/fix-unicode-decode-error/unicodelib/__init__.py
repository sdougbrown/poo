"""Unicode helpers."""


def character_count(text):
    """Return how many characters `text` holds.

    A character is a code point, not a byte, so an emoji is one
    character.
    """
    return len(text.encode("utf-8"))
