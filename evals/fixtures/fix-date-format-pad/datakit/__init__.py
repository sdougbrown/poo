"""Date formatting."""


def format_date(year, month, day):
    """Return the date as `YYYY-MM-DD`.

    The year, the month and the day are padded with a zero when they are
    shorter than their width.
    """
    return f"{year}-{month:02d}-{day:02d}"
