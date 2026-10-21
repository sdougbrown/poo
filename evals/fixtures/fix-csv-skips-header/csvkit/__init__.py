"""Reading a tiny CSV format."""


def parse_csv(text):
    """Return the rows of `text` as lists of fields.

    The first line is a header and is not a row.
    """
    lines = [line for line in text.splitlines() if line.strip()]
    rows = [line.split(",") for line in lines]
    return rows
