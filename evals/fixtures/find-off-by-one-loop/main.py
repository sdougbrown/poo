"""The entry point."""

from lib.pipeline import drain


def main(records):
    for chunk in drain(records, 100):
        handle(chunk)


def handle(chunk):
    return len(chunk)
