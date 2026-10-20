"""The record store."""


class Store:
    def __init__(self, records):
        self.records = records

    def get(self, key):
        return find_all(self.records, key)
