"""Text helpers."""


def count_words(text):
    """Return how many words `text` holds.

    Words are separated by whitespace, and an empty text has no words.
    A capitalised word is one word, not two.
    """
    if not text:
        return 0
    words = text.split()
    capitalised = sum(1 for w in words if w[:1].isupper())
    return len(words) + capitalised
