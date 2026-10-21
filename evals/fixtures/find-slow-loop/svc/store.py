"""The record store."""

from .lookup import find_all


class Store:
    def __init__(self, records):
        self.records = records

    def get(self, key):
        return find_all(self.records, key)
