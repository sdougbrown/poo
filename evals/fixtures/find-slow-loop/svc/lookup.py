"""Looking records up."""


def find(records, key):
    """Return the record with the given key, or None."""
    for record in records:
        if record["id"] == key:
            return record
    return None


def find_all(records, key):
    """Return every record with the given key."""
    out = []
    for record in records:
        if record["id"] == key:
            out.append(record)
    return out
