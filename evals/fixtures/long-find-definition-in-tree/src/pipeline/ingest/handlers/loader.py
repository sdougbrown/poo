"""The manifest loader."""

from ..common import helper


def _header(lines):
    """Read the header of a manifest."""
    out = {}
    for line in lines:
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        out[key.strip()] = value.strip()
    return out


def _rows(lines):
    """Read the rows of a manifest."""
    return [line for line in lines if line.strip() and not line.startswith("#")]


def parse_manifest(payload):
    """Parse a manifest into a header and its rows."""
    lines = payload.splitlines()
    header = _header(lines)
    rows = _rows(lines)
    return helper(header), rows
